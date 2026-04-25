package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

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

type Task struct {
	ID          int64           `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   string          `json:"created_at"`
	StartDate   string          `json:"start_date"`
	EndDate     string          `json:"end_date"`
	UserID      int64           `json:"user_id"`
	Interval    int             `json:"interval"`
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
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS user_id   BIGINT   REFERENCES users(id)`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS interval  INTEGER  NOT NULL DEFAULT 1`,
		`CREATE INDEX IF NOT EXISTS idx_completions_task_month ON completions(task_id, month)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_webhooks_user_id ON webhooks(user_id)`,
	}
	for _, s := range statements {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func migrate(db *sql.DB) error {
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
	for _, stmt := range []string{
		`ALTER TABLE tasks ADD COLUMN type        TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN metadata    TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE tasks ADD COLUMN start_date  TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN end_date    TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN user_id     INTEGER REFERENCES users(id)`,
		`ALTER TABLE tasks ADD COLUMN interval    INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE completions ADD COLUMN receipt_file TEXT    NOT NULL DEFAULT ''`,
		`ALTER TABLE completions ADD COLUMN amount       TEXT    NOT NULL DEFAULT ''`,
		`ALTER TABLE completions ADD COLUMN note         TEXT    NOT NULL DEFAULT ''`,
		`ALTER TABLE completions ADD COLUMN skipped      INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			msg := err.Error()
			if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
				return fmt.Errorf("migration: %w", err)
			}
		}
	}

	// Migrate date columns to nullable TEXT (SQLite table rebuild pattern).
	// PRAGMA foreign_keys must be OFF during rename: SQLite 3.26+ otherwise rewrites
	// FK references in child tables.
	var notNull int
	db.QueryRow(`SELECT "notnull" FROM pragma_table_info('tasks') WHERE name='start_date'`).Scan(&notNull)
	if notNull == 1 {
		db.Exec(`PRAGMA foreign_keys = OFF`)           //nolint:errcheck
		defer db.Exec(`PRAGMA foreign_keys = ON`)       //nolint:errcheck
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
			created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			start_date  TEXT,
			end_date    TEXT,
			user_id     INTEGER REFERENCES users(id),
			interval    INTEGER NOT NULL DEFAULT 1
		)`); err != nil {
			return fmt.Errorf("create tasks: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO tasks SELECT id,title,description,type,metadata,created_at,NULLIF(start_date,''),NULLIF(end_date,''),user_id,COALESCE(interval,1) FROM tasks_old`); err != nil {
			return fmt.Errorf("migrate tasks data: %w", err)
		}
		if _, err := tx.Exec(`DROP TABLE tasks_old`); err != nil {
			return fmt.Errorf("drop tasks_old: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit tasks rebuild: %w", err)
		}
	}

	// One-time repair: fix completions FK broken by a previous migration run that
	// renamed tasks without disabling foreign keys.
	var fkTable string
	db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('completions') LIMIT 1`).Scan(&fkTable)
	if fkTable == "tasks_old" {
		db.Exec(`PRAGMA foreign_keys = OFF`)           //nolint:errcheck
		defer db.Exec(`PRAGMA foreign_keys = ON`)       //nolint:errcheck
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
	}

	db.Exec(`CREATE INDEX IF NOT EXISTS idx_completions_task_month ON completions(task_id, month)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_completions_month ON completions(month)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_webhooks_user_id ON webhooks(user_id)`)
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

	return nil
}

// MigrateSettingsToUserScoped migrates the settings table from the old
// (key PRIMARY KEY) schema to the new (user_id, key PRIMARY KEY) schema.
// It is idempotent: if the migration has already run, it returns nil immediately.
// Must be called after the first admin user exists.
func (db *DB) MigrateSettingsToUserScoped(adminID int64) error {
	// Check if user_id column already exists.
	var hasUserID int
	var row *sql.Row
	if db.driver == "postgres" {
		row = db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name='settings' AND column_name='user_id'`)
	} else {
		row = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('settings') WHERE name='user_id'`)
	}
	if err := row.Scan(&hasUserID); err != nil {
		return fmt.Errorf("check settings schema: %w", err)
	}
	if hasUserID > 0 {
		return nil // already migrated
	}

	insertStmt := db.q(`INSERT INTO settings SELECT ?, key, value FROM settings_old`)

	if db.driver == "postgres" {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := tx.Exec(`ALTER TABLE settings RENAME TO settings_old`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE TABLE settings (
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			key     TEXT   NOT NULL,
			value   TEXT   NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, key)
		)`); err != nil {
			return err
		}
		if _, err := tx.Exec(insertStmt, adminID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE settings_old`); err != nil {
			return err
		}
		return tx.Commit()
	}

	// SQLite: must disable FK enforcement during table rename.
	db.Exec(`PRAGMA foreign_keys = OFF`)
	defer db.Exec(`PRAGMA foreign_keys = ON`)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE settings RENAME TO settings_old`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE settings (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     TEXT    NOT NULL,
		value   TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, key)
	)`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(insertStmt, adminID); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DROP TABLE settings_old`); err != nil {
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

func (db *DB) GetSettings(userID int64) (map[string]string, error) {
	rows, err := db.Query(db.q(`SELECT key, value FROM settings WHERE user_id = ?`), userID)
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

func (db *DB) SaveSettings(userID int64, settings map[string]string) error {
	tx, err := db.Begin()
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

const taskColumns = `id, title, description, type, metadata, created_at, start_date, end_date, user_id, interval`

func scanTask(row *sql.Row) (Task, error) {
	var t Task
	var meta string
	var startDate, endDate *string
	var userID *int64
	if err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Type, &meta, &t.CreatedAt, &startDate, &endDate, &userID, &t.Interval); err != nil {
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
	es := "COALESCE(NULLIF(start_date,''), " + db.ymExpr("created_at") + ")"
	return "(? - (CAST(SUBSTR(" + es + ", 1, 4) AS INTEGER) * 12 + CAST(SUBSTR(" + es + ", 6, 2) AS INTEGER) - 1)) % interval = 0"
}

func (db *DB) GetTasks(month string, userID int64) ([]Task, error) {
	rows, err := db.Query(
		db.q(`SELECT `+taskColumns+` FROM tasks
		 WHERE COALESCE(NULLIF(start_date,''), `+db.ymExpr("created_at")+`) <= ?
		   AND (end_date IS NULL OR end_date = '' OR end_date >= ?)
		   AND user_id = ?
		   AND `+db.intervalCheckExpr()+`
		 ORDER BY created_at ASC`),
		month, month, userID, monthIndex(month),
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
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Type, &meta, &t.CreatedAt, &startDate, &endDate, &uid, &t.Interval); err != nil {
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

func (db *DB) GetTaskByID(id int64) (Task, error) {
	return scanTask(db.QueryRow(db.q(`SELECT `+taskColumns+` FROM tasks WHERE id = ?`), id))
}

// GetReportData fetches tasks and completions for the given history and forecast months
// using two database queries, then groups the results per month in memory.
// historyMonths and forecastMonths must be sorted ascending YYYY-MM strings.
func (db *DB) GetReportData(userID int64, historyMonths, forecastMonths []string) ([]ReportMonth, error) {
	allMonths := make([]string, 0, len(historyMonths)+len(forecastMonths))
	allMonths = append(allMonths, historyMonths...)
	allMonths = append(allMonths, forecastMonths...)
	if len(allMonths) == 0 {
		return []ReportMonth{}, nil
	}
	minMonth := allMonths[0]
	maxMonth := allMonths[len(allMonths)-1]

	// 1. All tasks potentially active in [minMonth, maxMonth].
	es := "COALESCE(NULLIF(start_date,''), " + db.ymExpr("created_at") + ")"
	taskRows, err := db.Query(
		db.q(`SELECT `+taskColumns+` FROM tasks
		 WHERE `+es+` <= ?
		   AND (end_date IS NULL OR end_date = '' OR end_date >= ?)
		   AND user_id = ?
		 ORDER BY created_at ASC`),
		maxMonth, minMonth, userID,
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
		if err := taskRows.Scan(&t.ID, &t.Title, &t.Description, &t.Type, &meta, &t.CreatedAt, &startDate, &endDate, &uid, &t.Interval); err != nil {
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
		compRows, err := db.Query(
			db.q(`SELECT c.task_id, c.month, c.completed_at, c.receipt_file, c.amount, c.note, c.skipped
			 FROM completions c
			 JOIN tasks t ON t.id = c.task_id
			 WHERE c.month >= ? AND c.month <= ? AND t.user_id = ?`),
			historyMonths[0], historyMonths[len(historyMonths)-1], userID,
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
		monthTasks := []Task{}
		for _, t := range allTasks {
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
			IsForecast:  isForecastSet[m],
			Tasks:       monthTasks,
			Completions: comps,
		})
	}
	return result, nil
}

func (db *DB) GetReceiptsForTask(taskID int64) ([]string, error) {
	rows, err := db.Query(
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

func (db *DB) CreateTask(title, description, taskType, startDate, endDate string, metadata json.RawMessage, userID int64, interval int) (Task, error) {
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
		err := db.QueryRow(
			db.q(`INSERT INTO tasks (title, description, type, metadata, start_date, end_date, user_id, interval) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`),
			title, description, taskType, string(metadata), sd, ed, userID, interval,
		).Scan(&id)
		if err != nil {
			return Task{}, err
		}
	} else {
		res, err := db.Exec(
			db.q(`INSERT INTO tasks (title, description, type, metadata, start_date, end_date, user_id, interval) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
			title, description, taskType, string(metadata), sd, ed, userID, interval,
		)
		if err != nil {
			return Task{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return Task{}, err
		}
	}
	return db.GetTaskByID(id)
}

func (db *DB) UpdateTask(id int64, title, description, taskType, startDate, endDate string, metadata json.RawMessage, interval int) (Task, error) {
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
	_, err := db.Exec(
		db.q(`UPDATE tasks SET title = ?, description = ?, type = ?, metadata = ?, start_date = ?, end_date = ?, interval = ? WHERE id = ?`),
		title, description, taskType, string(metadata), sd, ed, interval, id,
	)
	if err != nil {
		return Task{}, err
	}
	return db.GetTaskByID(id)
}

// extractAmount pulls the "amount" string from a JSON metadata blob.
// Returns "" on any parse failure or if the key is absent.
func extractAmount(rawMeta string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(rawMeta), &m); err != nil {
		return ""
	}
	a, _ := m["amount"].(string)
	return a
}

// UpdateTaskWithAmountBackfill updates a task and, when the metadata amount changes,
// stamps the previous amount onto past completions that held no per-completion override
// (amount = ''). This preserves historical accuracy without any schema changes.
func (db *DB) UpdateTaskWithAmountBackfill(id int64, title, description, taskType, startDate, endDate string, metadata json.RawMessage, interval int) (Task, error) {
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

	tx, err := db.Begin()
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()

	var rawOld string
	if err := tx.QueryRow(db.q(`SELECT metadata FROM tasks WHERE id = ?`), id).Scan(&rawOld); err != nil {
		return Task{}, err
	}

	oldAmount := extractAmount(rawOld)
	newAmount := extractAmount(string(metadata))

	if _, err := tx.Exec(
		db.q(`UPDATE tasks SET title = ?, description = ?, type = ?, metadata = ?, start_date = ?, end_date = ?, interval = ? WHERE id = ?`),
		title, description, taskType, string(metadata), sd, ed, interval, id,
	); err != nil {
		return Task{}, err
	}

	if oldAmount != "" && newAmount != oldAmount {
		if _, err := tx.Exec(
			db.q(`UPDATE completions SET amount = ? WHERE task_id = ? AND (amount = '' OR amount IS NULL)`),
			oldAmount, id,
		); err != nil {
			return Task{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	return db.GetTaskByID(id)
}

func (db *DB) DeleteTask(id int64) error {
	_, err := db.Exec(db.q(`DELETE FROM tasks WHERE id = ?`), id)
	return err
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

// GetCompletions returns completions for a given month that belong to the user (via task ownership).
func (db *DB) GetCompletions(month string, userID int64) ([]Completion, error) {
	rows, err := db.Query(
		db.q(`SELECT c.task_id, c.month, c.completed_at, c.receipt_file, c.amount, c.note, c.skipped
		 FROM completions c
		 JOIN tasks t ON t.id = c.task_id
		 WHERE c.month = ? AND t.user_id = ?`),
		month, userID,
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

func (db *DB) GetCompletion(taskID int64, month string) (Completion, bool, error) {
	return scanCompletion(db.QueryRow(
		db.q(`SELECT `+completionColumns+` FROM completions WHERE task_id = ? AND month = ?`),
		taskID, month,
	))
}

func (db *DB) AddCompletion(taskID int64, month string) (Completion, error) {
	if _, err := db.Exec(db.q(`INSERT INTO completions (task_id, month) VALUES (?, ?)`), taskID, month); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(taskID, month)
	return c, err
}

func (db *DB) RemoveCompletion(taskID int64, month string) error {
	_, err := db.Exec(db.q(`DELETE FROM completions WHERE task_id = ? AND month = ?`), taskID, month)
	return err
}

// SkipCompletion toggles the skip state for the given task+month.
// - No row → inserts a skipped row; returns (completion, true, nil).
// - Row with skipped=1 → removes the row (back to pending); returns (Completion{}, false, nil).
// - Row with skipped=0 → returns an error (task is completed, not pending).
func (db *DB) SkipCompletion(taskID int64, month string) (Completion, bool, error) {
	existing, found, err := db.GetCompletion(taskID, month)
	if err != nil {
		return Completion{}, false, err
	}
	if found && !existing.Skipped {
		return Completion{}, false, fmt.Errorf("task is already completed")
	}
	if found && existing.Skipped {
		if _, err := db.Exec(db.q(`DELETE FROM completions WHERE task_id = ? AND month = ?`), taskID, month); err != nil {
			return Completion{}, false, err
		}
		return Completion{}, false, nil
	}
	if _, err := db.Exec(db.q(`INSERT INTO completions (task_id, month, skipped) VALUES (?, ?, 1)`), taskID, month); err != nil {
		return Completion{}, false, err
	}
	c, _, err := db.GetCompletion(taskID, month)
	return c, true, err
}

// CompleteSkipped updates a skipped completion to mark it as completed (skipped=0).
func (db *DB) CompleteSkipped(taskID int64, month string) (Completion, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		db.q(`UPDATE completions SET skipped = 0, completed_at = ? WHERE task_id = ? AND month = ?`),
		now, taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(taskID, month)
	return c, err
}

func (db *DB) SetCompletionReceipt(taskID int64, month, filename string) (Completion, error) {
	if _, err := db.Exec(
		db.q(`UPDATE completions SET receipt_file = ? WHERE task_id = ? AND month = ?`),
		filename, taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(taskID, month)
	return c, err
}

func (db *DB) ClearCompletionReceipt(taskID int64, month string) (Completion, error) {
	if _, err := db.Exec(
		db.q(`UPDATE completions SET receipt_file = '' WHERE task_id = ? AND month = ?`),
		taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(taskID, month)
	return c, err
}

func (db *DB) SetCompletionAmount(taskID int64, month, amount string) (Completion, error) {
	if _, err := db.Exec(
		db.q(`UPDATE completions SET amount = ? WHERE task_id = ? AND month = ?`),
		amount, taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(taskID, month)
	return c, err
}

func (db *DB) SetCompletionNote(taskID int64, month, note string) (Completion, error) {
	if _, err := db.Exec(
		db.q(`UPDATE completions SET note = ? WHERE task_id = ? AND month = ?`),
		note, taskID, month,
	); err != nil {
		return Completion{}, err
	}
	c, _, err := db.GetCompletion(taskID, month)
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
func (db *DB) GetCompletionsForExport(userID int64, from, to string) ([]ExportRow, error) {
	rows, err := db.Query(
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
func (db *DB) ImportCompletionsCSV(userID int64, rows []ImportRow) (ImportResult, error) {
	tx, err := db.Begin()
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
			err := tx.QueryRow(
				db.q(`SELECT id FROM tasks WHERE user_id = ? AND title = ? AND type = ?`),
				userID, row.Title, row.Type,
			).Scan(&id)
			if err == sql.ErrNoRows {
				// Create a minimal placeholder task.
				if db.driver == "postgres" {
					err = tx.QueryRow(
						db.q(`INSERT INTO tasks (title, description, type, metadata, user_id, interval) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`),
						row.Title, "", row.Type, "{}", userID, 1,
					).Scan(&id)
				} else {
					var res sql.Result
					res, err = tx.Exec(
						db.q(`INSERT INTO tasks (title, description, type, metadata, user_id, interval) VALUES (?, ?, ?, ?, ?, ?)`),
						row.Title, "", row.Type, "{}", userID, 1,
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
		if err := tx.QueryRow(
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
			if _, err := tx.Exec(
				db.q(`INSERT INTO completions (task_id, month, amount, skipped, completed_at) VALUES (?, ?, ?, ?, ?)`),
				taskID, row.Month, row.Amount, skipped, completedAt,
			); err != nil {
				return ImportResult{}, fmt.Errorf("insert completion %q %s: %w", row.Title, row.Month, err)
			}
			result.CompletionsCreated++
		} else {
			if _, err := tx.Exec(
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

func (db *DB) CountUsers() (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (db *DB) CountAdmins() (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&n)
	return n, err
}

func (db *DB) CreateUser(username, passwordHash string, isAdmin bool) (User, error) {
	isAdminInt := 0
	if isAdmin {
		isAdminInt = 1
	}
	var id int64
	if db.driver == "postgres" {
		err := db.QueryRow(
			db.q(`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?) RETURNING id`),
			username, passwordHash, isAdminInt,
		).Scan(&id)
		if err != nil {
			return User{}, err
		}
	} else {
		res, err := db.Exec(
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
	return db.GetUserByID(id)
}

func (db *DB) GetUserByID(id int64) (User, error) {
	return scanUser(db.QueryRow(db.q(`SELECT `+userColumns+` FROM users WHERE id = ?`), id))
}

func (db *DB) GetUserByUsername(username string) (User, error) {
	return scanUser(db.QueryRow(db.q(`SELECT `+userColumns+` FROM users WHERE username = ?`), username))
}

func (db *DB) GetFirstAdmin() (User, error) {
	return scanUser(db.QueryRow(`SELECT ` + userColumns + ` FROM users WHERE is_admin = 1 ORDER BY id ASC LIMIT 1`))
}

func (db *DB) ListUsers() ([]User, error) {
	rows, err := db.Query(`SELECT ` + userColumns + ` FROM users ORDER BY created_at ASC`)
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

func (db *DB) DeleteUser(id int64) error {
	_, err := db.Exec(db.q(`DELETE FROM users WHERE id = ?`), id)
	return err
}

func (db *DB) UpdateUserPassword(userID int64, newHash string) error {
	_, err := db.Exec(db.q(`UPDATE users SET password_hash = ? WHERE id = ?`), newHash, userID)
	return err
}

// AssignOrphanedTasks assigns all tasks with no user_id to the given user.
// Called once after the first admin is created.
func (db *DB) AssignOrphanedTasks(adminID int64) error {
	_, err := db.Exec(db.q(`UPDATE tasks SET user_id = ? WHERE user_id IS NULL`), adminID)
	return err
}

// ======== API Tokens ========

func (db *DB) CreateToken(userID int64, name, tokenHash string) (APIToken, error) {
	var id int64
	if db.driver == "postgres" {
		err := db.QueryRow(
			db.q(`INSERT INTO api_tokens (user_id, name, token_hash) VALUES (?, ?, ?) RETURNING id`),
			userID, name, tokenHash,
		).Scan(&id)
		if err != nil {
			return APIToken{}, err
		}
	} else {
		res, err := db.Exec(
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
	return db.getTokenByID(id)
}

func (db *DB) getTokenByID(id int64) (APIToken, error) {
	var t APIToken
	var lastUsed *string
	err := db.QueryRow(
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

func (db *DB) GetTokenByHash(hash string) (APIToken, error) {
	var t APIToken
	var lastUsed *string
	err := db.QueryRow(
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

func (db *DB) ListTokens(userID int64) ([]APIToken, error) {
	rows, err := db.Query(
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

func (db *DB) RevokeToken(id, userID int64) error {
	res, err := db.Exec(db.q(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`), id, userID)
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
func (db *DB) UpdateTokenLastUsed(id int64) {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(db.q(`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`), now, id); err != nil {
		log.Printf("UpdateTokenLastUsed(%d): %v", id, err)
	}
}

// ReceiptBelongsToUser checks that the given receipt file is attached to a completion
// whose task is owned by userID. Used to authorise GET /receipts/{filename}.
func (db *DB) ReceiptBelongsToUser(filename string, userID int64) (bool, error) {
	var n int
	err := db.QueryRow(
		db.q(`SELECT COUNT(*) FROM completions c
		      JOIN tasks t ON t.id = c.task_id
		      WHERE c.receipt_file = ? AND t.user_id = ?`),
		filename, userID,
	).Scan(&n)
	return n > 0, err
}

// ======== Webhooks ========

func (db *DB) CreateWebhook(userID int64, url, events, secret string) (Webhook, error) {
	var id int64
	if db.driver == "postgres" {
		err := db.QueryRow(
			db.q(`INSERT INTO webhooks (user_id, url, events, secret) VALUES (?, ?, ?, ?) RETURNING id`),
			userID, url, events, secret,
		).Scan(&id)
		if err != nil {
			return Webhook{}, err
		}
	} else {
		res, err := db.Exec(
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
	return db.getWebhookByID(id)
}

func (db *DB) getWebhookByID(id int64) (Webhook, error) {
	var wh Webhook
	err := db.QueryRow(
		db.q(`SELECT id, user_id, url, events, secret, created_at FROM webhooks WHERE id = ?`), id,
	).Scan(&wh.ID, &wh.UserID, &wh.URL, &wh.Events, &wh.Secret, &wh.CreatedAt)
	return wh, err
}

func (db *DB) ListWebhooks(userID int64) ([]Webhook, error) {
	rows, err := db.Query(
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

func (db *DB) DeleteWebhook(id, userID int64) error {
	res, err := db.Exec(db.q(`DELETE FROM webhooks WHERE id = ? AND user_id = ?`), id, userID)
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
func (db *DB) GetWebhooksForUser(userID int64) ([]Webhook, error) {
	return db.ListWebhooks(userID)
}

// ======== Audit Log ========

// InsertAuditLog records an action. Intended to be called in a goroutine (best-effort).
func (db *DB) InsertAuditLog(userID int64, action, entityType string, entityID int64, entityLabel string) {
	_, err := db.Exec(
		db.q(`INSERT INTO audit_logs (user_id, action, entity_type, entity_id, entity_label) VALUES (?, ?, ?, ?, ?)`),
		userID, action, entityType, entityID, entityLabel,
	)
	if err != nil {
		log.Printf("InsertAuditLog: %v", err)
	}
}

// GetAuditLogs returns audit log entries, newest first.
func (db *DB) GetAuditLogs(limit, offset int) ([]AuditLog, int, error) {
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.Query(
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
