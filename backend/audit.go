package main

import (
	"context"
	"log"
)

// auditQueueCapacity bounds the number of pending audit-log writes. Sized
// generously above normal request bursts; only sustained DB unavailability
// (e.g. a locked SQLite file) would fill it.
const auditQueueCapacity = 256

type auditEntry struct {
	userID      int64
	action      string
	entityType  string
	entityID    int64
	entityLabel string
}

// AuditQueue is a bounded, best-effort background queue for audit log writes.
// Handlers call Enqueue and return immediately; a single worker goroutine
// drains the queue and writes to the DB. This replaces the previous pattern
// of spawning one goroutine per write (`go db.InsertAuditLog(...)`), which
// could accumulate unboundedly if the DB is locked or slow — Enqueue instead
// drops (and logs) an entry when the queue is full, trading a lost audit
// entry for a bounded worst case.
type AuditQueue struct {
	db      *DB
	entries chan auditEntry
}

// newAuditQueue starts the background worker; it runs until ctx is cancelled.
func newAuditQueue(ctx context.Context, db *DB) *AuditQueue {
	q := &AuditQueue{db: db, entries: make(chan auditEntry, auditQueueCapacity)}
	go q.run(ctx)
	return q
}

func (q *AuditQueue) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-q.entries:
			q.db.InsertAuditLog(ctx, e.userID, e.action, e.entityType, e.entityID, e.entityLabel)
		}
	}
}

// Enqueue schedules an audit log write without blocking the caller. If the
// queue is full the entry is dropped and logged rather than blocking the HTTP
// handler or spawning another goroutine.
func (q *AuditQueue) Enqueue(userID int64, action, entityType string, entityID int64, entityLabel string) {
	select {
	case q.entries <- auditEntry{userID, action, entityType, entityID, entityLabel}:
	default:
		log.Printf("audit queue full — dropping entry: user=%d action=%s entity=%s/%d", userID, action, entityType, entityID)
	}
}
