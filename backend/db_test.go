package main

import (
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
	if bobSettings["currency"] != "$" {
		t.Errorf("bob currency: got %q, want $ (default)", bobSettings["currency"])
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
