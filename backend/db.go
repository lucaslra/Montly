package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// ErrAlreadyCompleted is returned when trying to skip a task that is already completed.
var ErrAlreadyCompleted = errors.New("task is already completed")

type DB struct {
	*sql.DB
	driver string // "sqlite" or "postgres"
}

// q rewrites ? placeholders to $1, $2, … for Postgres.
func (db *DB) q(query string) string {
	if db.driver != "postgres" {
		return query
	}
	var b strings.Builder
	n := 0
	for _, c := range query {
		if c == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// ymExpr returns a SQL expression that formats a datetime column as YYYY-MM.
func (db *DB) ymExpr(col string) string {
	if db.driver == "postgres" {
		return "to_char(" + col + "::timestamp, 'YYYY-MM')"
	}
	return "strftime('%Y-%m', " + col + ")"
}

// ======== Types ========

type SharedUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Task struct {
	ID          int64           `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Amount      string          `json:"amount"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   string          `json:"created_at"`
	StartDate   string          `json:"start_date"`
	EndDate     string          `json:"end_date"`
	UserID      int64           `json:"user_id"`
	Interval    int             `json:"interval"`
	ArchivedAt  *string         `json:"archived_at,omitempty"`
	SharedWith  []SharedUser    `json:"shared_with,omitempty"`
	IsShared    bool            `json:"is_shared,omitempty"`
	OwnerName   string          `json:"owner_name,omitempty"`
}

type Completion struct {
	TaskID      int64  `json:"task_id"`
	Month       string `json:"month"`
	CompletedAt string `json:"completed_at"`
	ReceiptFile string `json:"receipt_file"`
	Amount      string `json:"amount"` // overrides task's default amount when non-empty
	Note        string `json:"note"`
	Skipped     bool   `json:"skipped"`
}

type ReportMonth struct {
	Month       string       `json:"month"`
	IsForecast  bool         `json:"is_forecast"`
	Tasks       []Task       `json:"tasks"`
	Completions []Completion `json:"completions"`
}

type AuditLog struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Action      string `json:"action"`
	EntityType  string `json:"entity_type"`
	EntityID    int64  `json:"entity_id"`
	EntityLabel string `json:"entity_label"`
	CreatedAt   string `json:"created_at"`
}

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"` // never serialized
	IsAdmin      bool   `json:"is_admin"`
	CreatedAt    string `json:"created_at"`
	Email        string `json:"email,omitempty"`
	OIDCIssuer   string `json:"-"` // set for SSO-linked accounts
	OIDCSubject  string `json:"-"` // stable per-user id from the IdP
}

type APIToken struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

type Webhook struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	URL       string `json:"url"`
	Events    string `json:"events"` // comma-separated: "task.completed,task.uncompleted"
	Secret    string `json:"-"`      // never serialized
	CreatedAt string `json:"created_at"`
}

// ======== Init & Migrations ========

func initDB(dsn, driver string) (*DB, error) {
	var driverName string
	switch driver {
	case "postgres":
		driverName = "pgx"
	default:
		driverName = "sqlite"
		dsn = fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", dsn)
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "postgres" {
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)
	} else {
		// SQLite: serialize all access through a single connection.
		// Multiple connections to the same :memory: database each get their own isolated DB.
		// For file-based SQLite, serialization avoids "database is locked" errors under
		// concurrent goroutines (e.g. audit log writes, token updates).
		db.SetMaxOpenConns(1)
	}

	if driver != "postgres" {
		// WAL mode allows concurrent readers alongside the writer; synchronous=NORMAL
		// is safe with WAL and gives a meaningful durability/throughput improvement.
		db.Exec("PRAGMA journal_mode=WAL")   //nolint:errcheck
		db.Exec("PRAGMA synchronous=NORMAL") //nolint:errcheck
	}

	var migrateErr error
	if driver == "postgres" {
		migrateErr = migratePostgres(db)
	} else {
		migrateErr = migrate(db)
	}
	if migrateErr != nil {
		db.Close()
		return nil, migrateErr
	}
	return &DB{db, driver}, nil
}

// migratePostgres creates tables and indexes idempotently for PostgreSQL.
func migratePostgres(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            BIGSERIAL PRIMARY KEY,
			username      TEXT    NOT NULL UNIQUE,
			password_hash TEXT    NOT NULL,
			is_admin      INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT    NOT NULL DEFAULT to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id          BIGSERIAL PRIMARY KEY,
			title       TEXT    NOT NULL,
			description TEXT    NOT NULL DEFAULT '',
			type        TEXT    NOT NULL DEFAULT '',
			metadata    TEXT    NOT NULL DEFAULT '{}',
			created_at  TEXT    NOT NULL DEFAULT to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			start_date  TEXT,
			end_date    TEXT,
			user_id     BIGINT  REFERENCES users(id),
			interval    INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS completions (
			task_id      BIGINT  NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			month        TEXT    NOT NULL,
			completed_at TEXT    NOT NULL DEFAULT to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			receipt_file TEXT    NOT NULL DEFAULT '',
			amount       TEXT    NOT NULL DEFAULT '',
			note         TEXT    NOT NULL DEFAULT '',
			PRIMARY KEY (task_id, month)
		)`,
		`ALTER TABLE completions ADD COLUMN IF NOT EXISTS note    TEXT    NOT NULL DEFAULT ''`,
		`ALTER TABLE completions ADD COLUMN IF NOT EXISTS skipped INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id           BIGSERIAL PRIMARY KEY,
			user_id      BIGINT  NOT NULL,
			action       TEXT    NOT NULL,
			entity_type  TEXT    NOT NULL DEFAULT '',
			entity_id    BIGINT  NOT NULL DEFAULT 0,
			entity_label TEXT    NOT NULL DEFAULT '',
			created_at   TEXT    NOT NULL DEFAULT to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at)`,
		`CREATE TABLE IF NOT EXISTS settings (
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT   NOT NULL,
			value   TEXT   NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		)`,
		`CREATE TABLE IF NOT EXISTS api_tokens (
			id           BIGSERIAL PRIMARY KEY,
			user_id      BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name         TEXT    NOT NULL DEFAULT '',
			token_hash   TEXT    NOT NULL UNIQUE,
			created_at   TEXT    NOT NULL DEFAULT to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			last_used_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS webhooks (
			id         BIGSERIAL PRIMARY KEY,
			user_id    BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			url        TEXT    NOT NULL,
			events     TEXT    NOT NULL DEFAULT '',
			secret     TEXT    NOT NULL DEFAULT '',
			created_at TEXT    NOT NULL DEFAULT to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		)`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS user_id     BIGINT   REFERENCES users(id)`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS interval    INTEGER  NOT NULL DEFAULT 1`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS archived_at TEXT`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS amount      TEXT     NOT NULL DEFAULT ''`,
		`UPDATE tasks SET amount = COALESCE(metadata::json->>'amount', '') WHERE amount = ''`,
		`CREATE INDEX IF NOT EXISTS idx_completions_task_month ON completions(task_id, month)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_webhooks_user_id ON webhooks(user_id)`,
		`CREATE TABLE IF NOT EXISTS task_shares (
			task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			PRIMARY KEY (task_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_shares_user_id ON task_shares(user_id)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS email        TEXT`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_issuer  TEXT`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_subject TEXT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc ON users(oidc_issuer, oidc_subject) WHERE oidc_subject IS NOT NULL`,
	}
	for _, s := range statements {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func migrate(db *sql.DB) error {
	applied := 0

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			username      TEXT    NOT NULL UNIQUE,
			password_hash TEXT    NOT NULL,
			is_admin      INTEGER NOT NULL DEFAULT 0,
			created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		);
		CREATE TABLE IF NOT EXISTS tasks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			title       TEXT    NOT NULL,
			description TEXT    NOT NULL DEFAULT '',
			type        TEXT    NOT NULL DEFAULT '',
			metadata    TEXT    NOT NULL DEFAULT '{}',
			amount      TEXT    NOT NULL DEFAULT '',
			created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		);
		CREATE TABLE IF NOT EXISTS completions (
			task_id      INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			month        TEXT    NOT NULL,
			completed_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			receipt_file TEXT    NOT NULL DEFAULT '',
			amount       TEXT    NOT NULL DEFAULT '',
			note         TEXT    NOT NULL DEFAULT '',
			skipped      INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (task_id, month)
		);
		CREATE TABLE IF NOT EXISTS settings (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT    NOT NULL,
			value   TEXT    NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		);
		CREATE TABLE IF NOT EXISTS api_tokens (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name         TEXT    NOT NULL DEFAULT '',
			token_hash   TEXT    NOT NULL UNIQUE,
			created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			last_used_at TEXT
		);
		CREATE TABLE IF NOT EXISTS webhooks (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			url        TEXT    NOT NULL,
			events     TEXT    NOT NULL DEFAULT '',
			secret     TEXT    NOT NULL DEFAULT '',
			created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		);
	`); err != nil {
		return err
	}

	// Idempotent column additions for existing databases.
	// "duplicate column" (SQLite) is the expected error when the column already exists.
	type colStep struct{ desc, stmt string }
	for _, s := range []colStep{
		{"tasks.type", `ALTER TABLE tasks ADD COLUMN type        TEXT NOT NULL DEFAULT ''`},
		{"tasks.metadata", `ALTER TABLE tasks ADD COLUMN metadata    TEXT NOT NULL DEFAULT '{}'`},
		{"tasks.start_date", `ALTER TABLE tasks ADD COLUMN start_date  TEXT NOT NULL DEFAULT ''`},
		{"tasks.end_date", `ALTER TABLE tasks ADD COLUMN end_date    TEXT NOT NULL DEFAULT ''`},
		{"tasks.user_id", `ALTER TABLE tasks ADD COLUMN user_id     INTEGER REFERENCES users(id)`},
		{"tasks.interval", `ALTER TABLE tasks ADD COLUMN interval    INTEGER NOT NULL DEFAULT 1`},
		{"tasks.archived_at", `ALTER TABLE tasks ADD COLUMN archived_at TEXT`},
		{"tasks.amount", `ALTER TABLE tasks ADD COLUMN amount      TEXT NOT NULL DEFAULT ''`},
		{"completions.receipt_file", `ALTER TABLE completions ADD COLUMN receipt_file TEXT    NOT NULL DEFAULT ''`},
		{"completions.amount", `ALTER TABLE completions ADD COLUMN amount       TEXT    NOT NULL DEFAULT ''`},
		{"completions.note", `ALTER TABLE completions ADD COLUMN note         TEXT    NOT NULL DEFAULT ''`},
		{"completions.skipped", `ALTER TABLE completions ADD COLUMN skipped      INTEGER NOT NULL DEFAULT 0`},
		{"users.email", `ALTER TABLE users ADD COLUMN email        TEXT`},
		{"users.oidc_issuer", `ALTER TABLE users ADD COLUMN oidc_issuer  TEXT`},
		{"users.oidc_subject", `ALTER TABLE users ADD COLUMN oidc_subject TEXT`},
	} {
		if _, err := db.Exec(s.stmt); err != nil {
			msg := err.Error()
			if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
				return fmt.Errorf("migration: %w", err)
			}
		} else {
			log.Printf("migration: added column %s", s.desc)
			applied++
		}
	}

	// Migrate date columns to nullable TEXT (SQLite table rebuild pattern).
	// PRAGMA foreign_keys must be OFF during rename: SQLite 3.26+ otherwise rewrites
	// FK references in child tables.
	var notNull int
	db.QueryRow(`SELECT "notnull" FROM pragma_table_info('tasks') WHERE name='start_date'`).Scan(&notNull)
	if notNull == 1 {
		log.Printf("migration: rebuilding tasks table (converting date columns to nullable)")
		db.Exec(`PRAGMA foreign_keys = OFF`)      //nolint:errcheck
		defer db.Exec(`PRAGMA foreign_keys = ON`) //nolint:errcheck
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tasks rebuild tx: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := tx.Exec(`ALTER TABLE tasks RENAME TO tasks_old`); err != nil {
			return fmt.Errorf("rename tasks: %w", err)
		}
		if _, err := tx.Exec(`CREATE TABLE tasks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			title       TEXT    NOT NULL,
			description TEXT    NOT NULL DEFAULT '',
			type        TEXT    NOT NULL DEFAULT '',
			metadata    TEXT    NOT NULL DEFAULT '{}',
			amount      TEXT    NOT NULL DEFAULT '',
			created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			start_date  TEXT,
			end_date    TEXT,
			user_id     INTEGER REFERENCES users(id),
			interval    INTEGER NOT NULL DEFAULT 1,
			archived_at TEXT
		)`); err != nil {
			return fmt.Errorf("create tasks: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO tasks SELECT id,title,description,type,metadata,COALESCE(json_extract(metadata,'$.amount'),''),created_at,NULLIF(start_date,''),NULLIF(end_date,''),user_id,COALESCE(interval,1),NULL FROM tasks_old`); err != nil {
			return fmt.Errorf("migrate tasks data: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE tasks_old`); err != nil {
			return fmt.Errorf("drop tasks_old: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit tasks rebuild: %w", err)
		}
		applied++
	}

	// One-time repair: fix completions FK broken by a previous migration run that
	// renamed tasks without disabling foreign keys.
	var fkTable string
	db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('completions') LIMIT 1`).Scan(&fkTable)
	if fkTable == "tasks_old" {
		log.Printf("migration: repairing completions foreign key (was pointing to tasks_old)")
		db.Exec(`PRAGMA foreign_keys = OFF`)      //nolint:errcheck
		defer db.Exec(`PRAGMA foreign_keys = ON`) //nolint:errcheck
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin completions repair tx: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := tx.Exec(`ALTER TABLE completions RENAME TO completions_old`); err != nil {
			return fmt.Errorf("rename completions: %w", err)
		}
		if _, err := tx.Exec(`CREATE TABLE completions (
			task_id      INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			month        TEXT    NOT NULL,
			completed_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			receipt_file TEXT    NOT NULL DEFAULT '',
			amount       TEXT    NOT NULL DEFAULT '',
			note         TEXT    NOT NULL DEFAULT '',
			skipped      INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (task_id, month)
		)`); err != nil {
			return fmt.Errorf("create completions: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO completions SELECT task_id, month, completed_at, receipt_file, amount, '', 0 FROM completions_old`); err != nil {
			return fmt.Errorf("migrate completions data: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE completions_old`); err != nil {
			return fmt.Errorf("drop completions_old: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit completions repair: %w", err)
		}
		applied++
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_completions_task_month ON completions(task_id, month)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_completions_month ON completions(month)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_webhooks_user_id ON webhooks(user_id)`)
	// Unique per-provider identity for SSO-linked accounts (partial: only when linked).
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc ON users(oidc_issuer, oidc_subject) WHERE oidc_subject IS NOT NULL`)

	// Backfill amount from metadata JSON for existing rows.
	if res, err := db.Exec(`UPDATE tasks SET amount = COALESCE(json_extract(metadata, '$.amount'), '') WHERE amount = ''`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("migration: backfilled amount from metadata for %d task(s)", n)
			applied++
		}
	}

	var hasTaskShares int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='task_shares'`).Scan(&hasTaskShares)
	if hasTaskShares == 0 {
		log.Printf("migration: creating task_shares table")
		applied++
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS task_shares (
		task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		PRIMARY KEY (task_id, user_id)
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_task_shares_user_id ON task_shares(user_id)`)

	var hasAuditLogs int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='audit_logs'`).Scan(&hasAuditLogs)
	if hasAuditLogs == 0 {
		log.Printf("migration: creating audit_logs table")
		applied++
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS audit_logs (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id      INTEGER NOT NULL,
		action       TEXT    NOT NULL,
		entity_type  TEXT    NOT NULL DEFAULT '',
		entity_id    INTEGER NOT NULL DEFAULT 0,
		entity_label TEXT    NOT NULL DEFAULT '',
		created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at)`)

	if applied == 0 {
		log.Printf("migration: schema up to date, no migrations needed")
	} else {
		log.Printf("migration: complete (%d step(s) applied)", applied)
	}
	return nil
}

// MigrateSettingsToUserScoped migrates the settings table from the old
// (key PRIMARY KEY) schema to the new (user_id, key PRIMARY KEY) schema.
// It is idempotent: if the migration has already run, it returns nil immediately.
// Must be called after the first admin user exists.
func (db *DB) MigrateSettingsToUserScoped(ctx context.Context, adminID int64) error {
	// Check if user_id column already exists.
	var hasUserID int
	var row *sql.Row
	if db.driver == "postgres" {
		row = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name='settings' AND column_name='user_id'`)
	} else {
		row = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('settings') WHERE name='user_id'`)
	}
	if err := row.Scan(&hasUserID); err != nil {
		return fmt.Errorf("check settings schema: %w", err)
	}
	if hasUserID > 0 {
		log.Printf("migration: settings table already user-scoped, skipping")
		return nil
	}
	log.Printf("migration: migrating settings table to user-scoped schema")

	insertStmt := db.q(`INSERT INTO settings SELECT ?, key, value FROM settings_old`)

	if db.driver == "postgres" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := tx.ExecContext(ctx, `ALTER TABLE settings RENAME TO settings_old`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE settings (
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT   NOT NULL,
			value   TEXT   NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, insertStmt, adminID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE settings_old`); err != nil {
			return err
		}
		return tx.Commit()
	}

	// SQLite: must disable FK enforcement during table rename.
	db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`)
	defer db.ExecContext(ctx, `PRAGMA foreign_keys = ON`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE settings RENAME TO settings_old`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE settings (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     TEXT    NOT NULL,
		value   TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, key)
	)`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, insertStmt, adminID); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE settings_old`); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ======== Settings ========

var defaultSettings = map[string]string{
	"currency":          "€",
	"date_format":       "long",
	"color_mode":        "system",
	"task_sort":         "type",
	"completed_last":    "false",
	"fiscal_year_start": "1",
	"number_format":     "en",
}

func (db *DB) GetSettings(ctx context.Context, userID int64) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, db.q(`SELECT key, value FROM settings WHERE user_id = ?`), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for k, v := range defaultSettings {
		result[k] = v
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if v != "" {
			result[k] = v
		}
	}
	return result, rows.Err()
}

func (db *DB) SaveSettings(ctx context.Context, userID int64, settings map[string]string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.Prepare(db.q(`INSERT INTO settings (user_id, key, value) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`))
	if err != nil {
		return err
	}
	defer stmt.Close()
	for k, v := range settings {
		if _, err := stmt.Exec(userID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ======== Tasks ========

const taskColumns = `id, title, description, type, metadata, created_at, start_date, end_date, user_id, interval, archived_at, amount`

func scanTask(row *sql.Row) (Task, error) {
	var t Task
	var meta string
	var startDate, endDate *string
	var userID *int64
	if err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Type, &meta, &t.CreatedAt, &startDate, &endDate, &userID, &t.Interval, &t.ArchivedAt, &t.Amount); err != nil {
		return Task{}, err
	}
	if meta == "" {
		meta = "{}"
	}
	t.Metadata = json.RawMessage(meta)
	if startDate != nil {
		t.StartDate = *startDate
	}
	if endDate != nil {
		t.EndDate = *endDate
	}
	if userID != nil {
		t.UserID = *userID
	}
	if t.Interval == 0 {
		t.Interval = 1
	}
	return t, nil
}

// monthIndex converts a YYYY-MM string to a monotonic integer (year*12 + month - 1).
// The caller must ensure month is a valid YYYY-MM string (validated by isValidYearMonth).
func monthIndex(month string) int {
	if len(month) < 7 {
		return 0
	}
	year, _ := strconv.Atoi(month[:4])
	mon, _ := strconv.Atoi(month[5:])
	return year*12 + mon - 1
}

// taskActiveInMonth reports whether t is scheduled for the given YYYY-MM month,
// replicating the same logic used in the GetTasks SQL WHERE clause.
func taskActiveInMonth(t Task, month string) bool {
	es := t.StartDate
	if es == "" && len(t.CreatedAt) >= 7 {
		es = t.CreatedAt[:7]
	}
	if es > month {
		return false
	}
	if t.EndDate != "" && t.EndDate < month {
		return false
	}
	interval := t.Interval
	if interval <= 0 {
		interval = 1
	}
	return (monthIndex(month)-monthIndex(es))%interval == 0
}

// intervalCheckExpr returns a SQL expression that is true when the query month
// falls on a recurring interval anchored at the task's effective start date.
func (db *DB) intervalCheckExpr() string {
	return db.intervalCheckExprForAlias("")
}

func (db *DB) intervalCheckExprForAlias(alias string) string {
	p := ""
	if alias != "" {
		p = alias + "."
	}
	es := "COALESCE(NULLIF(" + p + "start_date,''), " + db.ymExpr(p+"created_at") + ")"
	return "(? - (CAST(SUBSTR(" + es + ", 1, 4) AS INTEGER) * 12 + CAST(SUBSTR(" + es + ", 6, 2) AS INTEGER) - 1)) % " + p + "interval = 0"
}

func (db *DB) GetTasks(ctx context.Context, month string, userID int64) ([]Task, error) {
	mi := monthIndex(month)
	es := "COALESCE(NULLIF(start_date,''), " + db.ymExpr("created_at") + ")"
	esT := "COALESCE(NULLIF(t.start_date,''), " + db.ymExpr("t.created_at") + ")"
	query := `SELECT id, title, description, type, metadata, created_at, start_date, end_date, user_id, interval, archived_at, amount, 0 AS is_shared, '' AS owner_name
		FROM tasks
		WHERE ` + es + ` <= ?
		  AND (end_date IS NULL OR end_date = '' OR end_date >= ?)
		  AND user_id = ?
		  AND archived_at IS NULL
		  AND ` + db.intervalCheckExpr() + `
		UNION
		SELECT t.id, t.title, t.description, t.type, t.metadata, t.created_at, t.start_date, t.end_date, t.user_id, t.interval, t.archived_at, t.amount, 1 AS is_shared, u.username AS owner_name
		FROM tasks t
		JOIN task_shares ts ON ts.task_id = t.id
		JOIN users u ON u.id = t.user_id
		WHERE ` + esT + ` <= ?
		  AND (t.end_date IS NULL OR t.end_date = '' OR t.end_date >= ?)
		  AND ts.user_id = ?
		  AND t.archived_at IS NULL
		  AND ` + db.intervalCheckExprForAlias("t") + `
		ORDER BY created_at ASC`
	rows, err := db.QueryContext(ctx, db.q(query), month, month, userID, mi, month, month, userID, mi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		var t Task
		var meta string
		var startDate, endDate *string
		var uid *int64
		var isSharedInt int
		var ownerName string
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Type, &meta, &t.CreatedAt, &startDate, &endDate, &uid, &t.Interval, &t.ArchivedAt, &t.Amount, &isSharedInt, &ownerName); err != nil {
			return nil, err
		}
		if meta == "" {
			meta = "{}"
		}
		t.Metadata = json.RawMessage(meta)
		if startDate != nil {
			t.StartDate = *startDate
		}
		if endDate != nil {
			t.EndDate = *endDate
		}
		if uid != nil {
			t.UserID = *uid
		}
		if t.Interval == 0 {
			t.Interval = 1
		}
		t.IsShared = isSharedInt != 0
		t.OwnerName = ownerName
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (db *DB) GetTaskByID(ctx context.Context, id int64) (Task, error) {
	return scanTask(db.QueryRowContext(ctx, db.q(`SELECT `+taskColumns+` FROM tasks WHERE id = ?`), id))
}

// GetReportData fetches tasks and completions for the given history and forecast months
// using two database queries, then groups the results per month in memory.
// historyMonths and forecastMonths must be sorted ascending YYYY-MM strings.
func (db *DB) GetReportData(ctx context.Context, userID int64, historyMonths, forecastMonths []string) ([]ReportMonth, error) {
	allMonths := make([]string, 0, len(historyMonths)+len(forecastMonths))
	allMonths = append(allMonths, historyMonths...)
	allMonths = append(allMonths, forecastMonths...)
	if len(allMonths) == 0 {
		return []ReportMonth{}, nil
	}
	minMonth := allMonths[0]
	maxMonth := allMonths[len(allMonths)-1]

	// 1. All tasks potentially active in [minMonth, maxMonth] — owned or shared.
	es := "COALESCE(NULLIF(start_date,''), " + db.ymExpr("created_at") + ")"
	taskRows, err := db.QueryContext(ctx,
		db.q(`SELECT `+taskColumns+` FROM tasks
		 WHERE `+es+` <= ?
		   AND (end_date IS NULL OR end_date = '' OR end_date >= ?)
		   AND (user_id = ? OR EXISTS (SELECT 1 FROM task_shares WHERE task_id = tasks.id AND user_id = ?))
		 ORDER BY created_at ASC`),
		maxMonth, minMonth, userID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("report tasks query: %w", err)
	}
	var allTasks []Task
	for taskRows.Next() {
		var t Task
		var meta string
		var startDate, endDate *string
		var uid *int64
		if err := taskRows.Scan(&t.ID, &t.Title, &t.Description, &t.Type, &meta, &t.CreatedAt, &startDate, &endDate, &uid, &t.Interval, &t.ArchivedAt, &t.Amount); err != nil {
			taskRows.Close()
			return nil, fmt.Errorf("report task scan: %w", err)
		}
		if meta == "" {
			meta = "{}"
		}
		t.Metadata = json.RawMessage(meta)
		if startDate != nil {
			t.StartDate = *startDate
		}
		if endDate != nil {
			t.EndDate = *endDate
		}
		if uid != nil {
			t.UserID = *uid
		}
		if t.Interval == 0 {
			t.Interval = 1
		}
		allTasks = append(allTasks, t)
	}
	if err := taskRows.Err(); err != nil {
		taskRows.Close()
		return nil, fmt.Errorf("report task rows: %w", err)
	}
	taskRows.Close()

	// 2. Completions for the history range (forecast months have none yet).
	compsByMonth := make(map[string][]Completion)
	if len(historyMonths) > 0 {
		compRows, err := db.QueryContext(ctx,
			db.q(`SELECT c.task_id, c.month, c.completed_at, c.receipt_file, c.amount, c.note, c.skipped
			 FROM completions c
			 JOIN tasks t ON t.id = c.task_id
			 WHERE c.month >= ? AND c.month <= ?
			   AND (t.user_id = ? OR EXISTS (SELECT 1 FROM task_shares WHERE task_id = t.id AND user_id = ?))`),
			historyMonths[0], historyMonths[len(historyMonths)-1], userID, userID,
		)
		if err != nil {
			return nil, fmt.Errorf("report completions query: %w", err)
		}
		defer compRows.Close()
		for compRows.Next() {
			var c Completion
			var skipped int
			if err := compRows.Scan(&c.TaskID, &c.Month, &c.CompletedAt, &c.ReceiptFile, &c.Amount, &c.Note, &skipped); err != nil {
				return nil, fmt.Errorf("report completion scan: %w", err)
			}
			c.Skipped = skipped != 0
			compsByMonth[c.Month] = append(compsByMonth[c.Month], c)
		}
		if err := compRows.Err(); err != nil {
			return nil, fmt.Errorf("report completion rows: %w", err)
		}
	}

	// 3. Build per-month result, filtering tasks in Go using interval logic.
	isForecastSet := make(map[string]bool, len(forecastMonths))
	for _, m := range forecastMonths {
		isForecastSet[m] = true
	}
	result := make([]ReportMonth, 0, len(allMonths))
	for _, m := range allMonths {
		isForecast := isForecastSet[m]
		monthTasks := []Task{}
		for _, t := range allTasks {
			if isForecast && t.ArchivedAt != nil {
				continue
			}
			if taskActiveInMonth(t, m) {
				monthTasks = append(monthTasks, t)
			}
		}
		comps := compsByMonth[m]
		if comps == nil {
			comps = []Completion{}
		}
		result = append(result, ReportMonth{
			Month:       m,
			IsForecast:  isForecast,
			Tasks:       monthTasks,
			Completions: comps,
		})
	}
	return result, nil
}

func (db *DB) GetReceiptsForTask(ctx context.Context, taskID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT receipt_file FROM completions WHERE task_id = ? AND receipt_file != ''`),
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (db *DB) CreateTask(ctx context.Context, title, description, taskType, startDate, endDate, amount string, metadata json.RawMessage, userID int64, interval int) (Task, error) {
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	if interval <= 0 {
		interval = 1
	}
	var sd, ed *string
	if startDate != "" {
		sd = &startDate
	}
	if endDate != "" {
		ed = &endDate
	}

	var id int64
	if db.driver == "postgres" {
		err := db.QueryRowContext(ctx,
			db.q(`INSERT INTO tasks (title, description, type, metadata, start_date, end_date, user_id, interval, amount) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`),
			title, description, taskType, string(metadata), sd, ed, userID, interval, amount,
		).Scan(&id)
		if err != nil {
			return Task{}, err
		}
	} else {
		res, err := db.ExecContext(ctx,
			db.q(`INSERT INTO tasks (title, description, type, metadata, start_date, end_date, user_id, interval, amount) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			title, description, taskType, string(metadata), sd, ed, userID, interval, amount,
		)
		if err != nil {
			return Task{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return Task{}, err
		}
	}
	return db.GetTaskByID(ctx, id)
}

// UpdateTaskWithAmountBackfill updates a task and, when the amount changes,
// stamps the previous amount onto past completions that held no per-completion override
// (amount = ”). This preserves historical accuracy.
func (db *DB) UpdateTaskWithAmountBackfill(ctx context.Context, id int64, title, description, taskType, startDate, endDate, amount string, metadata json.RawMessage, interval int) (Task, error) {
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	if interval <= 0 {
		interval = 1
	}
	var sd, ed *string
	if startDate != "" {
		sd = &startDate
	}
	if endDate != "" {
		ed = &endDate
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()

	var oldAmount string
	if err := tx.QueryRowContext(ctx, db.q(`SELECT amount FROM tasks WHERE id = ?`), id).Scan(&oldAmount); err != nil {
		return Task{}, err
	}

	if _, err := tx.ExecContext(ctx,
		db.q(`UPDATE tasks SET title = ?, description = ?, type = ?, metadata = ?, start_date = ?, end_date = ?, interval = ?, amount = ? WHERE id = ?`),
		title, description, taskType, string(metadata), sd, ed, interval, amount, id,
	); err != nil {
		return Task{}, err
	}

	if oldAmount != "" && amount != oldAmount {
		if _, err := tx.ExecContext(ctx,
			db.q(`UPDATE completions SET amount = ? WHERE task_id = ? AND (amount = '' OR amount IS NULL)`),
			oldAmount, id,
		); err != nil {
			return Task{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return db.GetTaskByID(ctx, id)
}

func (db *DB) DeleteTask(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, db.q(`DELETE FROM tasks WHERE id = ?`), id)
	return err
}

func (db *DB) ArchiveTask(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, db.q(`UPDATE tasks SET archived_at = ? WHERE id = ?`),
		time.Now().UTC().Format("2006-01-02T15:04:05Z"), id)
	return err
}

func (db *DB) UnarchiveTask(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, db.q(`UPDATE tasks SET archived_at = NULL WHERE id = ?`), id)
	return err
}

func (db *DB) GetArchivedTasks(ctx context.Context, userID int64) ([]Task, error) {
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT `+taskColumns+` FROM tasks WHERE user_id = ? AND archived_at IS NOT NULL ORDER BY archived_at DESC`),
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		var t Task
		var meta string
		var startDate, endDate *string
		var uid *int64
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Type, &meta, &t.CreatedAt, &startDate, &endDate, &uid, &t.Interval, &t.ArchivedAt, &t.Amount); err != nil {
			return nil, err
		}
		if meta == "" {
			meta = "{}"
		}
		t.Metadata = json.RawMessage(meta)
		if startDate != nil {
			t.StartDate = *startDate
		}
		if endDate != nil {
			t.EndDate = *endDate
		}
		if uid != nil {
			t.UserID = *uid
		}
		if t.Interval == 0 {
			t.Interval = 1
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ======== Completions ========

const completionColumns = `task_id, month, completed_at, receipt_file, amount, note, skipped`

func scanCompletion(row *sql.Row) (Completion, bool, error) {
	var c Completion
	var skipped int
	err := row.Scan(&c.TaskID, &c.Month, &c.CompletedAt, &c.ReceiptFile, &c.Amount, &c.Note, &skipped)
	if err == sql.ErrNoRows {
		return Completion{}, false, nil
	}
	c.Skipped = skipped != 0
	return c, err == nil, err
}

// GetCompletions returns completions for a given month that the user can access (owned or shared tasks).
func (db *DB) GetCompletions(ctx context.Context, month string, userID int64) ([]Completion, error) {
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT c.task_id, c.month, c.completed_at, c.receipt_file, c.amount, c.note, c.skipped
		 FROM completions c
		 JOIN tasks t ON t.id = c.task_id
		 WHERE c.month = ? AND (t.user_id = ? OR EXISTS (SELECT 1 FROM task_shares WHERE task_id = t.id AND user_id = ?))`),
		month, userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	completions := []Completion{}
	for rows.Next() {
		var c Completion
		var skipped int
		if err := rows.Scan(&c.TaskID, &c.Month, &c.CompletedAt, &c.ReceiptFile, &c.Amount, &c.Note, &skipped); err != nil {
			return nil, err
		}
		c.Skipped = skipped != 0
		completions = append(completions, c)
	}
	return completions, rows.Err()
}

func (db *DB) GetCompletion(ctx context.Context, taskID int64, month string) (Completion, bool, error) {
	return scanCompletion(db.QueryRowContext(ctx,
		db.q(`SELECT `+completionColumns+` FROM completions WHERE task_id = ? AND month = ?`),
		taskID, month,
	))
}

func (db *DB) AddCompletion(ctx context.Context, taskID int64, month string) (Completion, error) {
	if _, err := db.ExecContext(ctx, db.q(`INSERT INTO completions (task_id, month) VALUES (?, ?)`), taskID, month); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(ctx, taskID, month)
	return c, err
}

func (db *DB) RemoveCompletion(ctx context.Context, taskID int64, month string) error {
	_, err := db.ExecContext(ctx, db.q(`DELETE FROM completions WHERE task_id = ? AND month = ?`), taskID, month)
	return err
}

// SkipCompletion toggles the skip state for the given task+month.
// - No row → inserts a skipped row; returns (completion, true, nil).
// - Row with skipped=1 → removes the row (back to pending); returns (Completion{}, false, nil).
// - Row with skipped=0 → returns an error (task is completed, not pending).
func (db *DB) SkipCompletion(ctx context.Context, taskID int64, month string) (Completion, bool, error) {
	existing, found, err := db.GetCompletion(ctx, taskID, month)
	if err != nil {
		return Completion{}, false, err
	}
	if found && !existing.Skipped {
		return Completion{}, false, ErrAlreadyCompleted
	}
	if found && existing.Skipped {
		if _, err := db.ExecContext(ctx, db.q(`DELETE FROM completions WHERE task_id = ? AND month = ?`), taskID, month); err != nil {
			return Completion{}, false, err
		}
		return Completion{}, false, nil
	}
	if _, err := db.ExecContext(ctx, db.q(`INSERT INTO completions (task_id, month, skipped) VALUES (?, ?, 1)`), taskID, month); err != nil {
		return Completion{}, false, err
	}
	c, _, err := db.GetCompletion(ctx, taskID, month)
	return c, true, err
}

// CompleteSkipped updates a skipped completion to mark it as completed (skipped=0).
func (db *DB) CompleteSkipped(ctx context.Context, taskID int64, month string) (Completion, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		db.q(`UPDATE completions SET skipped = 0, completed_at = ? WHERE task_id = ? AND month = ?`),
		now, taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(ctx, taskID, month)
	return c, err
}

func (db *DB) SetCompletionReceipt(ctx context.Context, taskID int64, month, filename string) (Completion, error) {
	if _, err := db.ExecContext(ctx,
		db.q(`UPDATE completions SET receipt_file = ? WHERE task_id = ? AND month = ?`),
		filename, taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(ctx, taskID, month)
	return c, err
}

func (db *DB) ClearCompletionReceipt(ctx context.Context, taskID int64, month string) (Completion, error) {
	if _, err := db.ExecContext(ctx,
		db.q(`UPDATE completions SET receipt_file = '' WHERE task_id = ? AND month = ?`),
		taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(ctx, taskID, month)
	return c, err
}

func (db *DB) SetCompletionAmount(ctx context.Context, taskID int64, month, amount string) (Completion, error) {
	if _, err := db.ExecContext(ctx,
		db.q(`UPDATE completions SET amount = ? WHERE task_id = ? AND month = ?`),
		amount, taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(ctx, taskID, month)
	return c, err
}

func (db *DB) SetCompletionNote(ctx context.Context, taskID int64, month, note string) (Completion, error) {
	if _, err := db.ExecContext(ctx,
		db.q(`UPDATE completions SET note = ? WHERE task_id = ? AND month = ?`),
		note, taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(ctx, taskID, month)
	return c, err
}

// ExportRow is a flat row used for CSV export.
type ExportRow struct {
	Title      string
	Type       string
	Month      string
	Amount     string
	HasReceipt bool
	Status     string // "completed" or "skipped"
}

// GetCompletionsForExport returns all completions in the [from, to] month range for the user.
func (db *DB) GetCompletionsForExport(ctx context.Context, userID int64, from, to string) ([]ExportRow, error) {
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT t.title, t.type, c.month, c.amount, c.receipt_file, c.skipped
		 FROM completions c
		 JOIN tasks t ON t.id = c.task_id
		 WHERE t.user_id = ? AND c.month >= ? AND c.month <= ?
		 ORDER BY c.month ASC, t.title ASC`),
		userID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ExportRow
	for rows.Next() {
		var row ExportRow
		var receiptFile string
		var skipped int
		if err := rows.Scan(&row.Title, &row.Type, &row.Month, &row.Amount, &receiptFile, &skipped); err != nil {
			return nil, err
		}
		row.HasReceipt = receiptFile != ""
		if skipped != 0 {
			row.Status = "skipped"
		} else {
			row.Status = "completed"
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// ======== CSV Import ========

// ImportRow is one data row from the CSV import.
type ImportRow struct {
	Title  string
	Type   string
	Month  string
	Status string // "completed" or "skipped"
	Amount string
}

// ImportResult summarises what the import did.
type ImportResult struct {
	TasksCreated       int `json:"tasks_created"`
	CompletionsCreated int `json:"completions_created"`
	CompletionsUpdated int `json:"completions_updated"`
}

// ImportCompletionsCSV processes parsed import rows inside a single transaction.
// Tasks are matched by (title, type, user_id); a minimal task is created when no
// match is found. Completions are inserted or updated (amount + skipped status);
// existing receipt_file and note fields are never touched.
func (db *DB) ImportCompletionsCSV(ctx context.Context, userID int64, rows []ImportRow) (ImportResult, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var result ImportResult
	taskCache := map[string]int64{} // "title\x00type" → task_id

	for _, row := range rows {
		cacheKey := row.Title + "\x00" + row.Type
		taskID, cached := taskCache[cacheKey]
		if !cached {
			var id int64
			err := tx.QueryRowContext(ctx,
				db.q(`SELECT id FROM tasks WHERE user_id = ? AND title = ? AND type = ?`),
				userID, row.Title, row.Type,
			).Scan(&id)
			if err == sql.ErrNoRows {
				// Create a minimal placeholder task.
				if db.driver == "postgres" {
					err = tx.QueryRowContext(ctx,
						db.q(`INSERT INTO tasks (title, description, type, metadata, user_id, interval, start_date) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`),
						row.Title, "", row.Type, "{}", userID, 1, row.Month,
					).Scan(&id)
				} else {
					var res sql.Result
					res, err = tx.ExecContext(ctx,
						db.q(`INSERT INTO tasks (title, description, type, metadata, user_id, interval, start_date) VALUES (?, ?, ?, ?, ?, ?, ?)`),
						row.Title, "", row.Type, "{}", userID, 1, row.Month,
					)
					if err == nil {
						id, err = res.LastInsertId()
					}
				}
				if err != nil {
					return ImportResult{}, fmt.Errorf("create task %q: %w", row.Title, err)
				}
				result.TasksCreated++
			} else if err != nil {
				return ImportResult{}, fmt.Errorf("lookup task %q: %w", row.Title, err)
			}
			taskCache[cacheKey] = id
			taskID = id
		}

		skipped := 0
		if row.Status == "skipped" {
			skipped = 1
		}

		var existingCount int
		if err := tx.QueryRowContext(ctx,
			db.q(`SELECT COUNT(*) FROM completions WHERE task_id = ? AND month = ?`),
			taskID, row.Month,
		).Scan(&existingCount); err != nil {
			return ImportResult{}, fmt.Errorf("check completion %q %s: %w", row.Title, row.Month, err)
		}

		if existingCount == 0 {
			completedAt := ""
			if row.Status == "completed" {
				completedAt = time.Now().UTC().Format(time.RFC3339)
			}
			if _, err := tx.ExecContext(ctx,
				db.q(`INSERT INTO completions (task_id, month, amount, skipped, completed_at) VALUES (?, ?, ?, ?, ?)`),
				taskID, row.Month, row.Amount, skipped, completedAt,
			); err != nil {
				return ImportResult{}, fmt.Errorf("insert completion %q %s: %w", row.Title, row.Month, err)
			}
			result.CompletionsCreated++
		} else {
			if _, err := tx.ExecContext(ctx,
				db.q(`UPDATE completions SET amount = ?, skipped = ? WHERE task_id = ? AND month = ?`),
				row.Amount, skipped, taskID, row.Month,
			); err != nil {
				return ImportResult{}, fmt.Errorf("update completion %q %s: %w", row.Title, row.Month, err)
			}
			result.CompletionsUpdated++
		}
	}

	if err := tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

// ======== Users ========

const userColumns = `id, username, password_hash, is_admin, created_at`

func scanUser(row *sql.Row) (User, error) {
	var u User
	var isAdminInt int64
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdminInt, &u.CreatedAt); err != nil {
		return User{}, err
	}
	u.IsAdmin = isAdminInt != 0
	return u, nil
}

func (db *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (db *DB) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&n)
	return n, err
}

func (db *DB) CountTasksForUser(ctx context.Context, userID int64) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, db.q(`SELECT COUNT(*) FROM tasks WHERE user_id = ?`), userID).Scan(&n)
	return n, err
}

func (db *DB) CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (User, error) {
	isAdminInt := 0
	if isAdmin {
		isAdminInt = 1
	}
	var id int64
	if db.driver == "postgres" {
		err := db.QueryRowContext(ctx,
			db.q(`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?) RETURNING id`),
			username, passwordHash, isAdminInt,
		).Scan(&id)
		if err != nil {
			return User{}, err
		}
	} else {
		res, err := db.ExecContext(ctx,
			db.q(`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)`),
			username, passwordHash, isAdminInt,
		)
		if err != nil {
			return User{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return User{}, err
		}
	}
	return db.GetUserByID(ctx, id)
}

func (db *DB) GetUserByID(ctx context.Context, id int64) (User, error) {
	return scanUser(db.QueryRowContext(ctx, db.q(`SELECT `+userColumns+` FROM users WHERE id = ?`), id))
}

func (db *DB) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(db.QueryRowContext(ctx, db.q(`SELECT `+userColumns+` FROM users WHERE username = ?`), username))
}

func (db *DB) GetFirstAdmin(ctx context.Context) (User, error) {
	return scanUser(db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE is_admin = 1 ORDER BY id ASC LIMIT 1`))
}

func (db *DB) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		var isAdminInt int64
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdminInt, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.IsAdmin = isAdminInt != 0
		users = append(users, u)
	}
	return users, rows.Err()
}

func (db *DB) DeleteUser(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, db.q(`DELETE FROM users WHERE id = ?`), id)
	return err
}

func (db *DB) UpdateUserPassword(ctx context.Context, userID int64, newHash string) error {
	_, err := db.ExecContext(ctx, db.q(`UPDATE users SET password_hash = ? WHERE id = ?`), newHash, userID)
	return err
}

// AssignOrphanedTasks assigns all tasks with no user_id to the given user.
// Called once after the first admin is created.
func (db *DB) AssignOrphanedTasks(ctx context.Context, adminID int64) error {
	_, err := db.ExecContext(ctx, db.q(`UPDATE tasks SET user_id = ? WHERE user_id IS NULL`), adminID)
	return err
}

// ======== OIDC identity ========

const oidcUserColumns = `id, username, password_hash, is_admin, created_at, email, oidc_issuer, oidc_subject`

func scanOIDCUser(row *sql.Row) (User, bool, error) {
	var u User
	var isAdminInt int64
	var email, issuer, subject *string
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdminInt, &u.CreatedAt, &email, &issuer, &subject)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	u.IsAdmin = isAdminInt != 0
	if email != nil {
		u.Email = *email
	}
	if issuer != nil {
		u.OIDCIssuer = *issuer
	}
	if subject != nil {
		u.OIDCSubject = *subject
	}
	return u, true, nil
}

// GetUserByOIDC returns the user linked to the given (issuer, subject) pair, if any.
func (db *DB) GetUserByOIDC(ctx context.Context, issuer, subject string) (User, bool, error) {
	return scanOIDCUser(db.QueryRowContext(ctx,
		db.q(`SELECT `+oidcUserColumns+` FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`),
		issuer, subject,
	))
}

// GetUserByEmail returns the (lowest-id) user with the given non-empty email, if any.
// Used for account linking; callers must ensure email is non-empty.
func (db *DB) GetUserByEmail(ctx context.Context, email string) (User, bool, error) {
	return scanOIDCUser(db.QueryRowContext(ctx,
		db.q(`SELECT `+oidcUserColumns+` FROM users WHERE email = ? ORDER BY id ASC LIMIT 1`),
		email,
	))
}

// GetUserByUsernameFull returns the user with the given username plus OIDC/email fields.
func (db *DB) GetUserByUsernameFull(ctx context.Context, username string) (User, bool, error) {
	return scanOIDCUser(db.QueryRowContext(ctx,
		db.q(`SELECT `+oidcUserColumns+` FROM users WHERE username = ?`),
		username,
	))
}

// LinkOIDCIdentity stamps an (issuer, subject) pair onto an existing user, and
// records the email when provided (never wipes an existing email).
func (db *DB) LinkOIDCIdentity(ctx context.Context, userID int64, issuer, subject, email string) error {
	if email != "" {
		_, err := db.ExecContext(ctx, db.q(`UPDATE users SET oidc_issuer = ?, oidc_subject = ?, email = ? WHERE id = ?`), issuer, subject, email, userID)
		return err
	}
	_, err := db.ExecContext(ctx, db.q(`UPDATE users SET oidc_issuer = ?, oidc_subject = ? WHERE id = ?`), issuer, subject, userID)
	return err
}

// CreateOIDCUser provisions a new SSO-only user (empty password hash — password
// login is impossible until one is set).
func (db *DB) CreateOIDCUser(ctx context.Context, username, email, issuer, subject string, isAdmin bool) (User, error) {
	isAdminInt := 0
	if isAdmin {
		isAdminInt = 1
	}
	var emailPtr *string
	if email != "" {
		emailPtr = &email
	}
	var id int64
	if db.driver == "postgres" {
		err := db.QueryRowContext(ctx,
			db.q(`INSERT INTO users (username, password_hash, is_admin, email, oidc_issuer, oidc_subject) VALUES (?, '', ?, ?, ?, ?) RETURNING id`),
			username, isAdminInt, emailPtr, issuer, subject,
		).Scan(&id)
		if err != nil {
			return User{}, err
		}
	} else {
		res, err := db.ExecContext(ctx,
			db.q(`INSERT INTO users (username, password_hash, is_admin, email, oidc_issuer, oidc_subject) VALUES (?, '', ?, ?, ?, ?)`),
			username, isAdminInt, emailPtr, issuer, subject,
		)
		if err != nil {
			return User{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return User{}, err
		}
	}
	return db.GetUserByID(ctx, id)
}

// SetUserAdmin updates a user's admin flag (used to sync from an IdP group claim).
func (db *DB) SetUserAdmin(ctx context.Context, userID int64, isAdmin bool) error {
	v := 0
	if isAdmin {
		v = 1
	}
	_, err := db.ExecContext(ctx, db.q(`UPDATE users SET is_admin = ? WHERE id = ?`), v, userID)
	return err
}

// ======== API Tokens ========

func (db *DB) CreateToken(ctx context.Context, userID int64, name, tokenHash string) (APIToken, error) {
	var id int64
	if db.driver == "postgres" {
		err := db.QueryRowContext(ctx,
			db.q(`INSERT INTO api_tokens (user_id, name, token_hash) VALUES (?, ?, ?) RETURNING id`),
			userID, name, tokenHash,
		).Scan(&id)
		if err != nil {
			return APIToken{}, err
		}
	} else {
		res, err := db.ExecContext(ctx,
			db.q(`INSERT INTO api_tokens (user_id, name, token_hash) VALUES (?, ?, ?)`),
			userID, name, tokenHash,
		)
		if err != nil {
			return APIToken{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return APIToken{}, err
		}
	}
	return db.getTokenByID(ctx, id)
}

func (db *DB) getTokenByID(ctx context.Context, id int64) (APIToken, error) {
	var t APIToken
	var lastUsed *string
	err := db.QueryRowContext(ctx,
		db.q(`SELECT id, user_id, name, created_at, last_used_at FROM api_tokens WHERE id = ?`), id,
	).Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &lastUsed)
	if err != nil {
		return APIToken{}, err
	}
	if lastUsed != nil {
		t.LastUsedAt = *lastUsed
	}
	return t, nil
}

func (db *DB) GetTokenByHash(ctx context.Context, hash string) (APIToken, error) {
	var t APIToken
	var lastUsed *string
	err := db.QueryRowContext(ctx,
		db.q(`SELECT id, user_id, name, created_at, last_used_at FROM api_tokens WHERE token_hash = ?`), hash,
	).Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &lastUsed)
	if err != nil {
		return APIToken{}, err
	}
	if lastUsed != nil {
		t.LastUsedAt = *lastUsed
	}
	return t, nil
}

func (db *DB) ListTokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT id, user_id, name, created_at, last_used_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`),
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		var lastUsed *string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed != nil {
			t.LastUsedAt = *lastUsed
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (db *DB) RevokeToken(ctx context.Context, id, userID int64) error {
	res, err := db.ExecContext(ctx, db.q(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`), id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateTokenLastUsed updates last_used_at; called asynchronously.
func (db *DB) UpdateTokenLastUsed(ctx context.Context, id int64) {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, db.q(`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`), now, id); err != nil {
		log.Printf("UpdateTokenLastUsed(%d): %v", id, err)
	}
}

// ======== Task Shares ========

func (db *DB) AddShare(ctx context.Context, taskID, sharedWithUserID int64) error {
	var q string
	if db.driver == "postgres" {
		q = db.q(`INSERT INTO task_shares (task_id, user_id) VALUES (?, ?) ON CONFLICT DO NOTHING`)
	} else {
		q = `INSERT OR IGNORE INTO task_shares (task_id, user_id) VALUES (?, ?)`
	}
	_, err := db.ExecContext(ctx, q, taskID, sharedWithUserID)
	return err
}

func (db *DB) RemoveShare(ctx context.Context, taskID, sharedWithUserID int64) error {
	_, err := db.ExecContext(ctx, db.q(`DELETE FROM task_shares WHERE task_id = ? AND user_id = ?`), taskID, sharedWithUserID)
	return err
}

func (db *DB) IsSharedWith(ctx context.Context, taskID, userID int64) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, db.q(`SELECT COUNT(*) FROM task_shares WHERE task_id = ? AND user_id = ?`), taskID, userID).Scan(&n)
	return n > 0, err
}

func (db *DB) GetSharesForTask(ctx context.Context, taskID int64) ([]SharedUser, error) {
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT u.id, u.username FROM task_shares ts JOIN users u ON u.id = ts.user_id WHERE ts.task_id = ? ORDER BY u.username ASC`),
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var shares []SharedUser
	for rows.Next() {
		var s SharedUser
		if err := rows.Scan(&s.ID, &s.Username); err != nil {
			return nil, err
		}
		shares = append(shares, s)
	}
	return shares, rows.Err()
}

// LookupUsers returns users whose username contains the search string (case-insensitive),
// excluding the given user. Used for the share-task autocomplete.
func (db *DB) LookupUsers(ctx context.Context, query string, excludeUserID int64) ([]SharedUser, error) {
	pattern := "%" + strings.ToLower(query) + "%"
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT id, username FROM users WHERE lower(username) LIKE ? AND id != ? ORDER BY username ASC LIMIT 20`),
		pattern, excludeUserID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []SharedUser
	for rows.Next() {
		var u SharedUser
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// ReceiptBelongsToUser checks that the given receipt file is attached to a completion
// whose task is accessible by userID (owned or shared).
func (db *DB) ReceiptBelongsToUser(ctx context.Context, filename string, userID int64) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx,
		db.q(`SELECT COUNT(*) FROM completions c
		      JOIN tasks t ON t.id = c.task_id
		      WHERE c.receipt_file = ?
		        AND (t.user_id = ? OR EXISTS (SELECT 1 FROM task_shares WHERE task_id = t.id AND user_id = ?))`),
		filename, userID, userID,
	).Scan(&n)
	return n > 0, err
}

// ======== Webhooks ========

func (db *DB) CreateWebhook(ctx context.Context, userID int64, url, events, secret string) (Webhook, error) {
	var id int64
	if db.driver == "postgres" {
		err := db.QueryRowContext(ctx,
			db.q(`INSERT INTO webhooks (user_id, url, events, secret) VALUES (?, ?, ?, ?) RETURNING id`),
			userID, url, events, secret,
		).Scan(&id)
		if err != nil {
			return Webhook{}, err
		}
	} else {
		res, err := db.ExecContext(ctx,
			db.q(`INSERT INTO webhooks (user_id, url, events, secret) VALUES (?, ?, ?, ?)`),
			userID, url, events, secret,
		)
		if err != nil {
			return Webhook{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return Webhook{}, err
		}
	}
	return db.getWebhookByID(ctx, id)
}

func (db *DB) getWebhookByID(ctx context.Context, id int64) (Webhook, error) {
	var wh Webhook
	err := db.QueryRowContext(ctx,
		db.q(`SELECT id, user_id, url, events, secret, created_at FROM webhooks WHERE id = ?`), id,
	).Scan(&wh.ID, &wh.UserID, &wh.URL, &wh.Events, &wh.Secret, &wh.CreatedAt)
	return wh, err
}

func (db *DB) ListWebhooks(ctx context.Context, userID int64) ([]Webhook, error) {
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT id, user_id, url, events, secret, created_at FROM webhooks WHERE user_id = ? ORDER BY created_at ASC`),
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hooks []Webhook
	for rows.Next() {
		var wh Webhook
		if err := rows.Scan(&wh.ID, &wh.UserID, &wh.URL, &wh.Events, &wh.Secret, &wh.CreatedAt); err != nil {
			return nil, err
		}
		hooks = append(hooks, wh)
	}
	return hooks, rows.Err()
}

func (db *DB) DeleteWebhook(ctx context.Context, id, userID int64) error {
	res, err := db.ExecContext(ctx, db.q(`DELETE FROM webhooks WHERE id = ? AND user_id = ?`), id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetWebhooksForUser returns all webhooks for firing — includes secret.
func (db *DB) GetWebhooksForUser(ctx context.Context, userID int64) ([]Webhook, error) {
	return db.ListWebhooks(ctx, userID)
}

// GetMonthDigestWebhooks returns every webhook subscribed to "month.digest",
// across all users, in a single query — used by FireMonthDigest to avoid
// querying webhooks per-user for a fan-out that usually has few subscribers.
func (db *DB) GetMonthDigestWebhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT id, user_id, url, events, secret, created_at FROM webhooks WHERE ',' || events || ',' LIKE '%,month.digest,%'`),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hooks []Webhook
	for rows.Next() {
		var wh Webhook
		if err := rows.Scan(&wh.ID, &wh.UserID, &wh.URL, &wh.Events, &wh.Secret, &wh.CreatedAt); err != nil {
			return nil, err
		}
		hooks = append(hooks, wh)
	}
	return hooks, rows.Err()
}

// ======== Audit Log ========

// InsertAuditLog records an action. Intended to be called in a goroutine (best-effort).
func (db *DB) InsertAuditLog(ctx context.Context, userID int64, action, entityType string, entityID int64, entityLabel string) {
	_, err := db.ExecContext(ctx,
		db.q(`INSERT INTO audit_logs (user_id, action, entity_type, entity_id, entity_label) VALUES (?, ?, ?, ?, ?)`),
		userID, action, entityType, entityID, entityLabel,
	)
	if err != nil {
		log.Printf("InsertAuditLog: %v", err)
	}
}

// GetAuditLogs returns audit log entries, newest first.
func (db *DB) GetAuditLogs(ctx context.Context, limit, offset int) ([]AuditLog, int, error) {
	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.QueryContext(ctx,
		db.q(`SELECT al.id, al.user_id, COALESCE(u.username, 'deleted'), al.action, al.entity_type, al.entity_id, al.entity_label, al.created_at
		 FROM audit_logs al
		 LEFT JOIN users u ON u.id = al.user_id
		 ORDER BY al.created_at DESC
		 LIMIT ? OFFSET ?`),
		limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var logs []AuditLog
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Action, &l.EntityType, &l.EntityID, &l.EntityLabel, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []AuditLog{}
	}
	return logs, total, rows.Err()
}
