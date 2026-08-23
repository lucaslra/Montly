package main

import (
	"context"
	"testing"
	"time"
)

func TestAuditQueue_WorkerWritesEntries(t *testing.T) {
	db := setupTestDB(t)
	user, _ := db.CreateUser(t.Context(), "alice", testHash(t), false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newAuditQueue(ctx, db)

	q.Enqueue(user.ID, "test_action", "widget", 42, "label")

	deadline := time.Now().Add(2 * time.Second)
	for {
		logs, _, err := db.GetAuditLogs(t.Context(), 10, 0)
		if err != nil {
			t.Fatalf("GetAuditLogs: %v", err)
		}
		if len(logs) > 0 {
			if logs[0].Action != "test_action" {
				t.Errorf("action: got %q, want test_action", logs[0].Action)
			}
			if logs[0].EntityID != 42 {
				t.Errorf("entity_id: got %d, want 42", logs[0].EntityID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("audit entry did not appear within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAuditQueue_DropsWhenFull constructs the queue directly (no worker
// draining it) so entries accumulate predictably, then verifies Enqueue never
// blocks the caller even once the channel is at capacity — a full queue must
// drop the entry, not stall the HTTP handler that called it.
func TestAuditQueue_DropsWhenFull(t *testing.T) {
	db := setupTestDB(t)
	q := &AuditQueue{db: db, entries: make(chan auditEntry, 2)}
	q.Enqueue(1, "a", "t", 1, "")
	q.Enqueue(1, "a", "t", 1, "")

	done := make(chan struct{})
	go func() {
		q.Enqueue(1, "a", "t", 1, "") // queue is full; must return immediately, not block
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Enqueue blocked on a full queue")
	}
	if got := len(q.entries); got != 2 {
		t.Errorf("queue length: got %d, want 2 (capacity, excess dropped)", got)
	}
}
