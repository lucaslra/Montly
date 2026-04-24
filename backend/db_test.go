package main

import (
	"database/sql"
	"encoding/json"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := initDB(":memory:", "sqlite")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testHash(t *testing.T) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(h)
}

func TestMigrateIdempotent(t *testing.T) {
	db := setupTestDB(t)
	// Running migrate a second time on the same DB should be a no-op.
	if err := migrate(db.DB); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
}

func TestUserCRUD(t *testing.T) {
	db := setupTestDB(t)

	u, err := db.CreateUser("alice", testHash(t), false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("username: got %q, want alice", u.Username)
	}
	if u.IsAdmin {
		t.Error("IsAdmin: want false")
	}

	got, err := db.GetUserByUsername("alice")
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("id mismatch: got %d, want %d", got.ID, u.ID)
	}

	users, err := db.ListUsers()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("list: got %d users, want 1", len(users))
	}

	if err := db.DeleteUser(u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = db.GetUserByID(u.ID)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestTaskScoping(t *testing.T) {
	db := setupTestDB(t)

	alice, _ := db.CreateUser("alice", testHash(t), false)
	bob, _ := db.CreateUser("bob", testHash(t), false)

	// Use an explicit start_date so the task always appears regardless of when the test runs.
	_, err := db.CreateTask("Alice task", "", "", "2020-01", "", nil, alice.ID, 1)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	aliceTasks, err := db.GetTasks("9999-12", alice.ID)
	if err != nil {
		t.Fatalf("get tasks alice: %v", err)
	}
	bobTasks, err := db.GetTasks("9999-12", bob.ID)
	if err != nil {
		t.Fatalf("get tasks bob: %v", err)
	}

	if len(aliceTasks) != 1 {
		t.Errorf("alice: want 1 task, got %d", len(aliceTasks))
	}
	if len(bobTasks) != 0 {
		t.Errorf("bob: want 0 tasks, got %d", len(bobTasks))
	}
}

func TestCompletionScoping(t *testing.T) {
	db := setupTestDB(t)

	alice, _ := db.CreateUser("alice", testHash(t), false)
	bob, _ := db.CreateUser("bob", testHash(t), false)

	task, _ := db.CreateTask("Pay rent", "", "payment", "2020-01", "", nil, alice.ID, 1)
	_, err := db.AddCompletion(task.ID, "2026-04")
	if err != nil {
		t.Fatalf("add completion: %v", err)
	}

	aliceCompletions, _ := db.GetCompletions("2026-04", alice.ID)
	bobCompletions, _ := db.GetCompletions("2026-04", bob.ID)

	if len(aliceCompletions) != 1 {
		t.Errorf("alice completions: want 1, got %d", len(aliceCompletions))
	}
	if len(bobCompletions) != 0 {
		t.Errorf("bob completions: want 0, got %d", len(bobCompletions))
	}
}

func TestSettingsPerUser(t *testing.T) {
	db := setupTestDB(t)

	alice, _ := db.CreateUser("alice", testHash(t), true)
	bob, _ := db.CreateUser("bob", testHash(t), false)

	// On a fresh in-memory DB, settings already has user_id — MigrateSettingsToUserScoped is a no-op.
	if err := db.MigrateSettingsToUserScoped(alice.ID); err != nil {
		t.Fatalf("migrate settings: %v", err)
	}

	if err := db.SaveSettings(alice.ID, map[string]string{"currency": "€"}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	aliceSettings, err := db.GetSettings(alice.ID)
	if err != nil {
		t.Fatalf("get alice settings: %v", err)
	}
	bobSettings, err := db.GetSettings(bob.ID)
	if err != nil {
		t.Fatalf("get bob settings: %v", err)
	}

	if aliceSettings["currency"] != "€" {
		t.Errorf("alice currency: got %q, want €", aliceSettings["currency"])
	}
	if bobSettings["currency"] != "€" {
		t.Errorf("bob currency: got %q, want € (default)", bobSettings["currency"])
	}
}

func TestTokenCRUD(t *testing.T) {
	db := setupTestDB(t)

	user, _ := db.CreateUser("alice", testHash(t), false)

	tok, err := db.CreateToken(user.ID, "my token", "hashvalue123")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if tok.Name != "my token" {
		t.Errorf("name: got %q, want 'my token'", tok.Name)
	}
	if tok.UserID != user.ID {
		t.Errorf("user_id: got %d, want %d", tok.UserID, user.ID)
	}

	fetched, err := db.GetTokenByHash("hashvalue123")
	if err != nil {
		t.Fatalf("get by hash: %v", err)
	}
	if fetched.ID != tok.ID {
		t.Errorf("id mismatch: got %d, want %d", fetched.ID, tok.ID)
	}

	tokens, err := db.ListTokens(user.ID)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Errorf("list: want 1 token, got %d", len(tokens))
	}

	if err := db.RevokeToken(tok.ID, user.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, err = db.GetTokenByHash("hashvalue123")
	if err == nil {
		t.Error("expected error after revoke, got nil")
	}
}

func TestRevokeTokenOwnership(t *testing.T) {
	db := setupTestDB(t)

	alice, _ := db.CreateUser("alice", testHash(t), false)
	bob, _ := db.CreateUser("bob", testHash(t), false)

	tok, _ := db.CreateToken(alice.ID, "alice's token", "alicehash")

	// Bob should not be able to revoke Alice's token.
	err := db.RevokeToken(tok.ID, bob.ID)
	if err == nil {
		t.Error("bob should not be able to revoke alice's token")
	}
}

func TestAssignOrphanedTasks(t *testing.T) {
	db := setupTestDB(t)

	// Insert a task with NULL user_id directly (simulating pre-auth data).
	_, err := db.Exec(`INSERT INTO tasks (title, user_id) VALUES ('orphan', NULL)`)
	if err != nil {
		t.Fatalf("insert orphan task: %v", err)
	}

	admin, _ := db.CreateUser("admin", testHash(t), true)
	if err := db.AssignOrphanedTasks(admin.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	tasks, _ := db.GetTasks("9999-12", admin.ID)
	if len(tasks) != 1 {
		t.Errorf("want 1 task after assign, got %d", len(tasks))
	}
}

func TestUpdateUserPassword(t *testing.T) {
	db := setupTestDB(t)

	user, _ := db.CreateUser("alice", testHash(t), false)

	newHash, _ := bcrypt.GenerateFromPassword([]byte("newpassword99"), bcrypt.MinCost)
	if err := db.UpdateUserPassword(user.ID, string(newHash)); err != nil {
		t.Fatalf("update password: %v", err)
	}

	updated, _ := db.GetUserByID(user.ID)
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("newpassword99")); err != nil {
		t.Error("updated password hash does not match new password")
	}
}

// ── Phase 2: DB gap-fill ──────────────────────────────────────────────────────

// ── CountUsers / CountAdmins / GetFirstAdmin ──────────────────────────────────

func TestCountUsers(t *testing.T) {
	db := setupTestDB(t)

	n, err := db.CountUsers()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("empty DB: got %d, want 0", n)
	}

	db.CreateUser("alice", testHash(t), false)
	db.CreateUser("bob", testHash(t), false)

	n, err = db.CountUsers()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("after 2 inserts: got %d, want 2", n)
	}
}

func TestCountAdmins(t *testing.T) {
	db := setupTestDB(t)

	db.CreateUser("alice", testHash(t), false)
	db.CreateUser("admin1", testHash(t), true)
	db.CreateUser("admin2", testHash(t), true)

	n, err := db.CountAdmins()
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d admins, want 2", n)
	}
}

func TestGetFirstAdmin(t *testing.T) {
	db := setupTestDB(t)

	// Create a non-admin first, then two admins — expect the first admin by ID.
	db.CreateUser("regular", testHash(t), false)
	admin1, _ := db.CreateUser("admin1", testHash(t), true)
	db.CreateUser("admin2", testHash(t), true)

	got, err := db.GetFirstAdmin()
	if err != nil {
		t.Fatalf("GetFirstAdmin: %v", err)
	}
	if got.ID != admin1.ID {
		t.Errorf("got admin id %d, want %d", got.ID, admin1.ID)
	}
	if got.Username != "admin1" {
		t.Errorf("got username %q, want admin1", got.Username)
	}
}

// ── Task CRUD ─────────────────────────────────────────────────────────────────

func TestTaskCRUD(t *testing.T) {
	db := setupTestDB(t)
	user, _ := db.CreateUser("alice", testHash(t), false)

	// Create
	task, err := db.CreateTask("Pay rent", "monthly rent", "payment", "2026-01", "2026-12", nil, user.ID, 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Title != "Pay rent" {
		t.Errorf("title: got %q, want 'Pay rent'", task.Title)
	}
	if task.Type != "payment" {
		t.Errorf("type: got %q, want payment", task.Type)
	}
	if task.StartDate != "2026-01" {
		t.Errorf("start_date: got %q, want 2026-01", task.StartDate)
	}
	if task.EndDate != "2026-12" {
		t.Errorf("end_date: got %q, want 2026-12", task.EndDate)
	}
	if task.Interval != 1 {
		t.Errorf("interval: got %d, want 1", task.Interval)
	}
	if task.UserID != user.ID {
		t.Errorf("user_id: got %d, want %d", task.UserID, user.ID)
	}

	// Update
	updated, err := db.UpdateTask(task.ID, "New title", "new desc", "bill", "2026-02", "2026-11", nil, 3)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "New title" {
		t.Errorf("updated title: got %q, want 'New title'", updated.Title)
	}
	if updated.Type != "bill" {
		t.Errorf("updated type: got %q, want bill", updated.Type)
	}
	if updated.Interval != 3 {
		t.Errorf("updated interval: got %d, want 3", updated.Interval)
	}

	// GetTaskByID round-trip
	fetched, err := db.GetTaskByID(task.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if fetched.Title != "New title" {
		t.Errorf("fetched title: got %q", fetched.Title)
	}

	// Delete
	if err := db.DeleteTask(task.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = db.GetTaskByID(task.ID)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestUpdateTaskWithAmountBackfill(t *testing.T) {
	t.Run("stamps old amount onto empty-amount completions", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Netflix", "", "subscription", "2026-01", "", json.RawMessage(`{"amount":"10"}`), user.ID, 1)
		db.AddCompletion(task.ID, "2026-01")
		db.AddCompletion(task.ID, "2026-02")

		if _, err := db.UpdateTaskWithAmountBackfill(task.ID, task.Title, task.Description, task.Type, task.StartDate, task.EndDate, json.RawMessage(`{"amount":"11"}`), task.Interval); err != nil {
			t.Fatalf("update: %v", err)
		}

		for _, month := range []string{"2026-01", "2026-02"} {
			c, _, err := db.GetCompletion(task.ID, month)
			if err != nil {
				t.Fatalf("get completion %s: %v", month, err)
			}
			if c.Amount != "10" {
				t.Errorf("completion %s: amount = %q, want '10'", month, c.Amount)
			}
		}
	})

	t.Run("manual per-completion overrides are not touched", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Netflix", "", "subscription", "2026-01", "", json.RawMessage(`{"amount":"10"}`), user.ID, 1)
		db.AddCompletion(task.ID, "2026-01")
		db.SetCompletionAmount(task.ID, "2026-01", "9") // manual override

		if _, err := db.UpdateTaskWithAmountBackfill(task.ID, task.Title, task.Description, task.Type, task.StartDate, task.EndDate, json.RawMessage(`{"amount":"11"}`), task.Interval); err != nil {
			t.Fatalf("update: %v", err)
		}

		c, _, _ := db.GetCompletion(task.ID, "2026-01")
		if c.Amount != "9" {
			t.Errorf("amount: got %q, want '9' (manual override should be preserved)", c.Amount)
		}
	})

	t.Run("non-amount changes do not touch completions", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Netflix", "", "subscription", "2026-01", "", json.RawMessage(`{"amount":"10"}`), user.ID, 1)
		db.AddCompletion(task.ID, "2026-01")

		if _, err := db.UpdateTaskWithAmountBackfill(task.ID, "New Title", task.Description, task.Type, task.StartDate, task.EndDate, json.RawMessage(`{"amount":"10"}`), task.Interval); err != nil {
			t.Fatalf("update: %v", err)
		}

		c, _, _ := db.GetCompletion(task.ID, "2026-01")
		if c.Amount != "" {
			t.Errorf("amount: got %q, want '' (unchanged)", c.Amount)
		}
	})

	t.Run("no previous amount set means no backfill", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Netflix", "", "subscription", "2026-01", "", nil, user.ID, 1)
		db.AddCompletion(task.ID, "2026-01")

		if _, err := db.UpdateTaskWithAmountBackfill(task.ID, task.Title, task.Description, task.Type, task.StartDate, task.EndDate, json.RawMessage(`{"amount":"11"}`), task.Interval); err != nil {
			t.Fatalf("update: %v", err)
		}

		c, _, _ := db.GetCompletion(task.ID, "2026-01")
		if c.Amount != "" {
			t.Errorf("amount: got %q, want '' (no old amount to stamp)", c.Amount)
		}
	})
}

func TestGetTasks_StartDateFilter(t *testing.T) {
	db := setupTestDB(t)
	user, _ := db.CreateUser("alice", testHash(t), false)

	// Task with start_date "2026-03" should not appear in earlier months.
	db.CreateTask("March task", "", "", "2026-03", "", nil, user.ID, 1)

	before, _ := db.GetTasks("2026-02", user.ID)
	if len(before) != 0 {
		t.Errorf("2026-02: expected 0 tasks (before start), got %d", len(before))
	}

	onStart, _ := db.GetTasks("2026-03", user.ID)
	if len(onStart) != 1 {
		t.Errorf("2026-03: expected 1 task (on start), got %d", len(onStart))
	}

	after, _ := db.GetTasks("2026-04", user.ID)
	if len(after) != 1 {
		t.Errorf("2026-04: expected 1 task (after start), got %d", len(after))
	}
}

func TestGetTasks_EndDateFilter(t *testing.T) {
	db := setupTestDB(t)
	user, _ := db.CreateUser("alice", testHash(t), false)

	db.CreateTask("Expiring task", "", "", "2026-01", "2026-02", nil, user.ID, 1)

	within, _ := db.GetTasks("2026-02", user.ID)
	if len(within) != 1 {
		t.Errorf("2026-02: expected 1 task (within range), got %d", len(within))
	}

	after, _ := db.GetTasks("2026-03", user.ID)
	if len(after) != 0 {
		t.Errorf("2026-03: expected 0 tasks (after end), got %d", len(after))
	}
}

func TestGetTasks_IntervalFilter(t *testing.T) {
	db := setupTestDB(t)
	user, _ := db.CreateUser("alice", testHash(t), false)

	// Quarterly task (interval=3) anchored at 2026-01.
	db.CreateTask("Quarterly", "", "", "2026-01", "", nil, user.ID, 3)

	cases := []struct {
		month string
		want  int
	}{
		{"2026-01", 1}, // anchor month — appears
		{"2026-02", 0}, // 1 month offset — skip
		{"2026-03", 0}, // 2 months offset — skip
		{"2026-04", 1}, // 3 months offset — appears
		{"2026-07", 1}, // 6 months offset — appears
	}
	for _, tc := range cases {
		tasks, err := db.GetTasks(tc.month, user.ID)
		if err != nil {
			t.Fatalf("GetTasks(%s): %v", tc.month, err)
		}
		if len(tasks) != tc.want {
			t.Errorf("month=%s: got %d tasks, want %d", tc.month, len(tasks), tc.want)
		}
	}
}

// ── Completion CRUD ───────────────────────────────────────────────────────────

func TestCompletionCRUD(t *testing.T) {
	db := setupTestDB(t)
	user, _ := db.CreateUser("alice", testHash(t), false)
	task, _ := db.CreateTask("Pay rent", "", "payment", "2020-01", "", nil, user.ID, 1)

	// AddCompletion
	c, err := db.AddCompletion(task.ID, "2026-04")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if c.TaskID != task.ID {
		t.Errorf("task_id: got %d, want %d", c.TaskID, task.ID)
	}
	if c.Month != "2026-04" {
		t.Errorf("month: got %q, want 2026-04", c.Month)
	}
	if c.Amount != "" {
		t.Errorf("amount: want empty, got %q", c.Amount)
	}

	// GetCompletion
	got, found, err := db.GetCompletion(task.ID, "2026-04")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !found {
		t.Fatal("GetCompletion: expected found=true")
	}
	if got.TaskID != task.ID {
		t.Errorf("get: task_id mismatch")
	}

	// SetCompletionAmount
	c, err = db.SetCompletionAmount(task.ID, "2026-04", "42.50")
	if err != nil {
		t.Fatalf("set amount: %v", err)
	}
	if c.Amount != "42.50" {
		t.Errorf("amount: got %q, want 42.50", c.Amount)
	}

	// SetCompletionNote
	c, err = db.SetCompletionNote(task.ID, "2026-04", "paid via bank transfer")
	if err != nil {
		t.Fatalf("set note: %v", err)
	}
	if c.Note != "paid via bank transfer" {
		t.Errorf("note: got %q", c.Note)
	}

	// SetCompletionReceipt
	c, err = db.SetCompletionReceipt(task.ID, "2026-04", "550e8400-e29b-41d4-a716-446655440000.pdf")
	if err != nil {
		t.Fatalf("set receipt: %v", err)
	}
	if c.ReceiptFile != "550e8400-e29b-41d4-a716-446655440000.pdf" {
		t.Errorf("receipt_file: got %q", c.ReceiptFile)
	}

	// ClearCompletionReceipt
	c, err = db.ClearCompletionReceipt(task.ID, "2026-04")
	if err != nil {
		t.Fatalf("clear receipt: %v", err)
	}
	if c.ReceiptFile != "" {
		t.Errorf("receipt_file after clear: got %q, want empty", c.ReceiptFile)
	}

	// RemoveCompletion
	if err := db.RemoveCompletion(task.ID, "2026-04"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	_, found, err = db.GetCompletion(task.ID, "2026-04")
	if err != nil {
		t.Fatalf("get after remove: %v", err)
	}
	if found {
		t.Error("expected found=false after remove")
	}
}

func TestGetCompletion_NotFound(t *testing.T) {
	db := setupTestDB(t)
	user, _ := db.CreateUser("alice", testHash(t), false)
	task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, user.ID, 1)

	_, found, err := db.GetCompletion(task.ID, "2026-04")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for non-existent completion")
	}
}

// ── GetReceiptsForTask ────────────────────────────────────────────────────────

func TestGetReceiptsForTask(t *testing.T) {
	db := setupTestDB(t)
	user, _ := db.CreateUser("alice", testHash(t), false)
	task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, user.ID, 1)

	db.AddCompletion(task.ID, "2026-01")
	db.AddCompletion(task.ID, "2026-02")
	db.SetCompletionReceipt(task.ID, "2026-01", "550e8400-e29b-41d4-a716-446655440000.pdf")
	// 2026-02 has no receipt

	files, err := db.GetReceiptsForTask(task.ID)
	if err != nil {
		t.Fatalf("GetReceiptsForTask: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0] != "550e8400-e29b-41d4-a716-446655440000.pdf" {
		t.Errorf("filename: got %q", files[0])
	}
}

// ── GetCompletionsForExport ───────────────────────────────────────────────────

func TestGetCompletionsForExport(t *testing.T) {
	db := setupTestDB(t)
	alice, _ := db.CreateUser("alice", testHash(t), false)
	bob, _ := db.CreateUser("bob", testHash(t), false)

	taskA, _ := db.CreateTask("Pay rent", "", "payment", "2020-01", "", nil, alice.ID, 1)
	taskB, _ := db.CreateTask("Internet", "", "subscription", "2020-01", "", nil, alice.ID, 1)
	taskBob, _ := db.CreateTask("Bob task", "", "", "2020-01", "", nil, bob.ID, 1)

	db.AddCompletion(taskA.ID, "2026-01")
	db.AddCompletion(taskA.ID, "2026-03")
	db.AddCompletion(taskB.ID, "2026-02")
	db.AddCompletion(taskBob.ID, "2026-02") // should not appear in alice's export

	t.Run("returns rows within range", func(t *testing.T) {
		rows, err := db.GetCompletionsForExport(alice.ID, "2026-01", "2026-02")
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		if len(rows) != 2 {
			t.Errorf("got %d rows, want 2 (2026-01 and 2026-02 completions)", len(rows))
		}
	})

	t.Run("excludes completions outside range", func(t *testing.T) {
		rows, err := db.GetCompletionsForExport(alice.ID, "2026-01", "2026-02")
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		for _, r := range rows {
			if r.Month > "2026-02" || r.Month < "2026-01" {
				t.Errorf("row %q is outside range 2026-01..2026-02", r.Month)
			}
		}
	})

	t.Run("does not include other users' completions", func(t *testing.T) {
		rows, err := db.GetCompletionsForExport(alice.ID, "2026-01", "2026-12")
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		for _, r := range rows {
			if r.Title == "Bob task" {
				t.Error("alice's export should not include bob's task")
			}
		}
	})

	t.Run("HasReceipt flag is set when receipt exists", func(t *testing.T) {
		db.SetCompletionReceipt(taskA.ID, "2026-01", "550e8400-e29b-41d4-a716-446655440000.pdf")
		rows, err := db.GetCompletionsForExport(alice.ID, "2026-01", "2026-01")
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		if !rows[0].HasReceipt {
			t.Error("expected HasReceipt=true for completion with receipt")
		}
	})
}

// ── ReceiptBelongsToUser ──────────────────────────────────────────────────────

func TestReceiptBelongsToUser(t *testing.T) {
	db := setupTestDB(t)
	alice, _ := db.CreateUser("alice", testHash(t), false)
	bob, _ := db.CreateUser("bob", testHash(t), false)

	task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, alice.ID, 1)
	db.AddCompletion(task.ID, "2026-04")
	receipt := "550e8400-e29b-41d4-a716-446655440000.pdf"
	db.SetCompletionReceipt(task.ID, "2026-04", receipt)

	ok, err := db.ReceiptBelongsToUser(receipt, alice.ID)
	if err != nil {
		t.Fatalf("check alice: %v", err)
	}
	if !ok {
		t.Error("expected receipt to belong to alice")
	}

	ok, err = db.ReceiptBelongsToUser(receipt, bob.ID)
	if err != nil {
		t.Fatalf("check bob: %v", err)
	}
	if ok {
		t.Error("expected receipt to NOT belong to bob")
	}
}

// ── Webhook CRUD ──────────────────────────────────────────────────────────────

func TestWebhookCRUD(t *testing.T) {
	db := setupTestDB(t)
	alice, _ := db.CreateUser("alice", testHash(t), false)
	bob, _ := db.CreateUser("bob", testHash(t), false)

	// Create
	wh, err := db.CreateWebhook(alice.ID, "https://example.com/hook", "task.completed", "mysecret")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if wh.URL != "https://example.com/hook" {
		t.Errorf("url: got %q", wh.URL)
	}
	if wh.Events != "task.completed" {
		t.Errorf("events: got %q, want task.completed", wh.Events)
	}
	if wh.Secret != "mysecret" {
		t.Errorf("secret: got %q, want mysecret", wh.Secret)
	}
	if wh.UserID != alice.ID {
		t.Errorf("user_id: got %d, want %d", wh.UserID, alice.ID)
	}

	// ListWebhooks
	hooks, err := db.ListWebhooks(alice.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("list: got %d hooks, want 1", len(hooks))
	}
	if hooks[0].ID != wh.ID {
		t.Errorf("list: id mismatch")
	}

	// GetWebhooksForUser returns same as list (includes secret for firing)
	firing, err := db.GetWebhooksForUser(alice.ID)
	if err != nil {
		t.Fatalf("GetWebhooksForUser: %v", err)
	}
	if len(firing) != 1 || firing[0].Secret != "mysecret" {
		t.Error("GetWebhooksForUser: missing secret field")
	}

	// Bob sees no hooks
	bobHooks, err := db.ListWebhooks(bob.ID)
	if err != nil {
		t.Fatalf("list bob: %v", err)
	}
	if len(bobHooks) != 0 {
		t.Errorf("bob should see 0 hooks, got %d", len(bobHooks))
	}

	// Delete with wrong user → ErrNoRows
	if err := db.DeleteWebhook(wh.ID, bob.ID); err != sql.ErrNoRows {
		t.Errorf("wrong-owner delete: got %v, want sql.ErrNoRows", err)
	}

	// Delete with correct user → success
	if err := db.DeleteWebhook(wh.ID, alice.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hooks, _ = db.ListWebhooks(alice.ID)
	if len(hooks) != 0 {
		t.Errorf("after delete: expected 0 hooks, got %d", len(hooks))
	}
}

// ── ImportCompletionsCSV ──────────────────────────────────────────────────────

func TestImportCompletionsCSV(t *testing.T) {
	mustUser := func(t *testing.T, db *DB, name string) User {
		t.Helper()
		hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
		u, err := db.CreateUser(name, string(hash), false)
		if err != nil {
			t.Fatalf("createUser %s: %v", name, err)
		}
		return u
	}
	mustTask := func(t *testing.T, db *DB, title string, userID int64) Task {
		t.Helper()
		task, err := db.CreateTask(title, "", "payment", "", "", json.RawMessage(`{}`), userID, 1)
		if err != nil {
			t.Fatalf("createTask %s: %v", title, err)
		}
		return task
	}

	t.Run("creates task and completion when no match exists", func(t *testing.T) {
		db := setupTestDB(t)
		alice := mustUser(t, db, "alice")
		rows := []ImportRow{{Title: "New Sub", Type: "subscription", Month: "2026-03", Status: "completed", Amount: "9.99"}}
		result, err := db.ImportCompletionsCSV(alice.ID, rows)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if result.TasksCreated != 1 {
			t.Errorf("tasks_created: got %d, want 1", result.TasksCreated)
		}
		if result.CompletionsCreated != 1 {
			t.Errorf("completions_created: got %d, want 1", result.CompletionsCreated)
		}
		if result.CompletionsUpdated != 0 {
			t.Errorf("completions_updated: got %d, want 0", result.CompletionsUpdated)
		}
	})

	t.Run("matches existing task by title and type", func(t *testing.T) {
		db := setupTestDB(t)
		alice := mustUser(t, db, "alice")
		task := mustTask(t, db, "Rent", alice.ID)
		rows := []ImportRow{{Title: "Rent", Type: "payment", Month: "2026-04", Status: "completed", Amount: "750"}}
		result, err := db.ImportCompletionsCSV(alice.ID, rows)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if result.TasksCreated != 0 {
			t.Errorf("should not create a new task; got tasks_created=%d", result.TasksCreated)
		}
		c, found, _ := db.GetCompletion(task.ID, "2026-04")
		if !found {
			t.Fatal("completion not found after import")
		}
		if c.Amount != "750" {
			t.Errorf("amount: got %q, want %q", c.Amount, "750")
		}
	})

	t.Run("updates existing completion without touching receipt or note", func(t *testing.T) {
		db := setupTestDB(t)
		alice := mustUser(t, db, "alice")
		task := mustTask(t, db, "Gym", alice.ID)
		// pre-existing completion with a note and receipt stub
		db.Exec(db.q(`INSERT INTO completions (task_id, month, amount, note, receipt_file) VALUES (?, ?, ?, ?, ?)`),
			task.ID, "2026-02", "30", "see receipt", "stub-uuid.pdf")
		rows := []ImportRow{{Title: "Gym", Type: "payment", Month: "2026-02", Status: "completed", Amount: "35"}}
		result, err := db.ImportCompletionsCSV(alice.ID, rows)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if result.CompletionsUpdated != 1 {
			t.Errorf("completions_updated: got %d, want 1", result.CompletionsUpdated)
		}
		c, _, _ := db.GetCompletion(task.ID, "2026-02")
		if c.Amount != "35" {
			t.Errorf("amount not updated: got %q", c.Amount)
		}
		if c.Note != "see receipt" {
			t.Errorf("note was cleared: got %q", c.Note)
		}
		if c.ReceiptFile != "stub-uuid.pdf" {
			t.Errorf("receipt_file was cleared: got %q", c.ReceiptFile)
		}
	})

	t.Run("skipped status sets skipped=1", func(t *testing.T) {
		db := setupTestDB(t)
		alice := mustUser(t, db, "alice")
		rows := []ImportRow{{Title: "Optional", Type: "reminder", Month: "2026-05", Status: "skipped", Amount: ""}}
		if _, err := db.ImportCompletionsCSV(alice.ID, rows); err != nil {
			t.Fatalf("import: %v", err)
		}
		var skipped int
		tasks, _ := db.GetTasks("2026-05", alice.ID)
		db.QueryRow(db.q(`SELECT skipped FROM completions WHERE task_id = ? AND month = ?`), tasks[0].ID, "2026-05").Scan(&skipped)
		if skipped != 1 {
			t.Errorf("skipped: got %d, want 1", skipped)
		}
	})

	t.Run("same title different type creates two tasks", func(t *testing.T) {
		db := setupTestDB(t)
		alice := mustUser(t, db, "alice")
		rows := []ImportRow{
			{Title: "Netflix", Type: "subscription", Month: "2026-01", Status: "completed", Amount: "11"},
			{Title: "Netflix", Type: "payment", Month: "2026-01", Status: "completed", Amount: "5"},
		}
		result, err := db.ImportCompletionsCSV(alice.ID, rows)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if result.TasksCreated != 2 {
			t.Errorf("tasks_created: got %d, want 2", result.TasksCreated)
		}
	})

	t.Run("deduplicates tasks across rows for same title+type", func(t *testing.T) {
		db := setupTestDB(t)
		alice := mustUser(t, db, "alice")
		rows := []ImportRow{
			{Title: "Spotify", Type: "subscription", Month: "2026-01", Status: "completed", Amount: "10"},
			{Title: "Spotify", Type: "subscription", Month: "2026-02", Status: "completed", Amount: "10"},
		}
		result, err := db.ImportCompletionsCSV(alice.ID, rows)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if result.TasksCreated != 1 {
			t.Errorf("tasks_created: got %d, want 1 (only on first occurrence)", result.TasksCreated)
		}
		if result.CompletionsCreated != 2 {
			t.Errorf("completions_created: got %d, want 2", result.CompletionsCreated)
		}
	})

	t.Run("isolates imports between users", func(t *testing.T) {
		db := setupTestDB(t)
		alice := mustUser(t, db, "alice")
		bob := mustUser(t, db, "bob")
		mustTask(t, db, "Shared Title", alice.ID)
		rows := []ImportRow{{Title: "Shared Title", Type: "payment", Month: "2026-06", Status: "completed", Amount: "1"}}
		result, err := db.ImportCompletionsCSV(bob.ID, rows)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		// bob doesn't own alice's task → should create a new task for bob
		if result.TasksCreated != 1 {
			t.Errorf("tasks_created: got %d, want 1 (bob gets own task)", result.TasksCreated)
		}
	})
}
