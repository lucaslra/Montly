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
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

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
		`ALTER TABLE completions ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT ''`,
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
		`ALTER TABLE completions ADD COLUMN receipt_file TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE completions ADD COLUMN amount       TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE completions ADD COLUMN note         TEXT NOT NULL DEFAULT ''`,
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
			PRIMARY KEY (task_id, month)
		)`); err != nil {
			return fmt.Errorf("create completions: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO completions SELECT task_id, month, completed_at, receipt_file, amount, '' FROM completions_old`); err != nil {
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
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_webhooks_user_id ON webhooks(user_id)`)

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

func (db *DB) DeleteTask(id int64) error {
	_, err := db.Exec(db.q(`DELETE FROM tasks WHERE id = ?`), id)
	return err
}

// ======== Completions ========

const completionColumns = `task_id, month, completed_at, receipt_file, amount, note`

func scanCompletion(row *sql.Row) (Completion, bool, error) {
	var c Completion
	err := row.Scan(&c.TaskID, &c.Month, &c.CompletedAt, &c.ReceiptFile, &c.Amount, &c.Note)
	if err == sql.ErrNoRows {
		return Completion{}, false, nil
	}
	return c, err == nil, err
}

// GetCompletions returns completions for a given month that belong to the user (via task ownership).
func (db *DB) GetCompletions(month string, userID int64) ([]Completion, error) {
	rows, err := db.Query(
		db.q(`SELECT c.task_id, c.month, c.completed_at, c.receipt_file, c.amount, c.note
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
		if err := rows.Scan(&c.TaskID, &c.Month, &c.CompletedAt, &c.ReceiptFile, &c.Amount, &c.Note); err != nil {
			return nil, err
		}
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
