package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// ── test infrastructure ───────────────────────────────────────────────────────

// testServer bundles the router, DB, and helpers needed to make HTTP requests.
type testServer struct {
	db          *DB
	router      http.Handler
	secret      []byte
	receiptsDir string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	db := setupTestDB(t)
	secret := []byte("handler-test-secret")
	receiptsDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rl := newRateLimiter(ctx)

	h := &Handler{db: db, receiptsDir: receiptsDir}
	ah := &AuthHandler{db: db, secret: secret, secure: false, trustProxy: false, rl: rl}
	uh := &UserHandler{db: db}
	th := &TokenHandler{db: db}
	wh := &WebhookHandler{db: db}

	r := chi.NewRouter()
	mountRoutes := func(r chi.Router) {
		r.Get("/auth/setup", ah.SetupStatus)
		r.Post("/auth/setup", ah.Setup)
		r.Post("/auth/login", ah.Login)
		r.Post("/auth/logout", ah.Logout)

		r.Group(func(r chi.Router) {
			r.Use(requireAuth(secret, db, false))
			r.Get("/auth/me", ah.Me)
			r.Patch("/auth/password", ah.ChangePassword)
			r.Get("/auth/tokens", th.ListTokens)
			r.Post("/auth/tokens", th.CreateToken)
			r.Delete("/auth/tokens/{id}", th.RevokeToken)
			r.Get("/webhooks", wh.ListWebhooks)
			r.Post("/webhooks", wh.CreateWebhook)
			r.Delete("/webhooks/{id}", wh.DeleteWebhook)
			mountAPI(r, h)
			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/users", uh.ListUsers)
				r.Post("/users", uh.CreateUser)
				r.Delete("/users/{id}", uh.DeleteUser)
			})
		})
	}
	r.Route("/api", func(r chi.Router) { mountRoutes(r) })

	return &testServer{db: db, router: r, secret: secret, receiptsDir: receiptsDir}
}

// do executes a request against the test router and returns the recorder.
func (ts *testServer) do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	ts.router.ServeHTTP(w, req)
	return w
}

// req builds an HTTP request. body may be "" for no body.
func (ts *testServer) req(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	var br io.Reader
	if body != "" {
		br = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, br)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

// authReq is like req but attaches a valid session cookie for the given user.
func (ts *testServer) authReq(t *testing.T, method, path, body string, userID int64, isAdmin bool) *http.Request {
	t.Helper()
	r := ts.req(t, method, path, body)
	tok, err := newSession(sessionClaims{
		UserID:  userID,
		IsAdmin: isAdmin,
		Expires: time.Now().Add(time.Hour).Unix(),
	}, ts.secret)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	return r
}

// mustUser creates a user in the test DB and fails the test if it errors.
// The password is always "password123" (via testHash).
func (ts *testServer) mustUser(t *testing.T, username string, isAdmin bool) User {
	t.Helper()
	u, err := ts.db.CreateUser(username, testHash(t), isAdmin)
	if err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
	return u
}

// mustTask creates a task in the test DB and fails the test if it errors.
func (ts *testServer) mustTask(t *testing.T, title string, userID int64) Task {
	t.Helper()
	task, err := ts.db.CreateTask(title, "", "", "2020-01", "", nil, userID, 1)
	if err != nil {
		t.Fatalf("create task %q: %v", title, err)
	}
	return task
}

// assertStatus fails the test if the response status code is not want.
func assertStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, want, w.Body.String())
	}
}

// decodeJSON unmarshals the response body into v, failing the test on error.
func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decodeJSON: %v (body: %s)", err, w.Body.String())
	}
}

// hasCookie reports whether the response sets a cookie with the given name.
func hasCookie(w *httptest.ResponseRecorder, name string) bool {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return true
		}
	}
	return false
}

// ── SetupStatus ───────────────────────────────────────────────────────────────

func TestSetupStatus(t *testing.T) {
	t.Run("needs_setup=true on empty DB", func(t *testing.T) {
		ts := newTestServer(t)
		w := ts.do(ts.req(t, http.MethodGet, "/api/auth/setup", ""))
		assertStatus(t, w, http.StatusOK)
		var resp map[string]bool
		decodeJSON(t, w, &resp)
		if !resp["needs_setup"] {
			t.Error("expected needs_setup=true on empty DB")
		}
	})

	t.Run("needs_setup=false when a user exists", func(t *testing.T) {
		ts := newTestServer(t)
		ts.mustUser(t, "admin", true)
		w := ts.do(ts.req(t, http.MethodGet, "/api/auth/setup", ""))
		assertStatus(t, w, http.StatusOK)
		var resp map[string]bool
		decodeJSON(t, w, &resp)
		if resp["needs_setup"] {
			t.Error("expected needs_setup=false when user exists")
		}
	})
}

// ── Setup ─────────────────────────────────────────────────────────────────────

func TestSetup(t *testing.T) {
	t.Run("creates first admin and opens session", func(t *testing.T) {
		ts := newTestServer(t)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/setup",
			`{"username":"admin","password":"password123"}`))
		assertStatus(t, w, http.StatusCreated)
		var resp map[string]any
		decodeJSON(t, w, &resp)
		if resp["username"] != "admin" {
			t.Errorf("username: got %q, want admin", resp["username"])
		}
		if !hasCookie(w, sessionCookieName) {
			t.Error("expected session cookie to be set after setup")
		}
	})

	t.Run("409 when a user already exists", func(t *testing.T) {
		ts := newTestServer(t)
		ts.mustUser(t, "existing", true)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/setup",
			`{"username":"admin2","password":"password123"}`))
		assertStatus(t, w, http.StatusConflict)
	})

	t.Run("400 when password is too short", func(t *testing.T) {
		ts := newTestServer(t)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/setup",
			`{"username":"admin","password":"short"}`))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when username is empty", func(t *testing.T) {
		ts := newTestServer(t)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/setup",
			`{"username":"","password":"password123"}`))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when username exceeds 64 characters", func(t *testing.T) {
		ts := newTestServer(t)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/setup",
			fmt.Sprintf(`{"username":%q,"password":"password123"}`, strings.Repeat("a", 65))))
		assertStatus(t, w, http.StatusBadRequest)
	})
}

// ── Login ─────────────────────────────────────────────────────────────────────

func TestLogin(t *testing.T) {
	t.Run("success returns user and sets session cookie", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/login",
			`{"username":"alice","password":"password123"}`))
		assertStatus(t, w, http.StatusOK)
		var resp map[string]any
		decodeJSON(t, w, &resp)
		if resp["username"] != "alice" {
			t.Errorf("username: got %q, want alice", resp["username"])
		}
		if int64(resp["id"].(float64)) != alice.ID {
			t.Errorf("id mismatch: got %v, want %d", resp["id"], alice.ID)
		}
		if !hasCookie(w, sessionCookieName) {
			t.Error("expected session cookie to be set on login")
		}
	})

	t.Run("401 on wrong password", func(t *testing.T) {
		ts := newTestServer(t)
		ts.mustUser(t, "alice", false)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/login",
			`{"username":"alice","password":"wrongpassword"}`))
		assertStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("401 for unknown user", func(t *testing.T) {
		ts := newTestServer(t)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/login",
			`{"username":"nobody","password":"password123"}`))
		assertStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("400 when credentials are empty", func(t *testing.T) {
		ts := newTestServer(t)
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/login",
			`{"username":"","password":""}`))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("429 after max failed attempts from same IP", func(t *testing.T) {
		ts := newTestServer(t)
		ts.mustUser(t, "alice", false)
		for i := 0; i < rlMaxFailures; i++ {
			ts.do(ts.req(t, http.MethodPost, "/api/auth/login",
				`{"username":"alice","password":"wrongpassword"}`))
		}
		w := ts.do(ts.req(t, http.MethodPost, "/api/auth/login",
			`{"username":"alice","password":"wrongpassword"}`))
		assertStatus(t, w, http.StatusTooManyRequests)
	})
}

// ── Logout ────────────────────────────────────────────────────────────────────

func TestLogout(t *testing.T) {
	ts := newTestServer(t)
	alice := ts.mustUser(t, "alice", false)
	w := ts.do(ts.authReq(t, http.MethodPost, "/api/auth/logout", "", alice.ID, false))
	assertStatus(t, w, http.StatusNoContent)
	// Verify the session cookie is cleared (MaxAge should be -1).
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge != -1 {
			t.Errorf("session cookie not cleared: MaxAge=%d, want -1", c.MaxAge)
		}
	}
}

// ── Me ────────────────────────────────────────────────────────────────────────

func TestMe(t *testing.T) {
	t.Run("returns user info when authenticated", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/auth/me", "", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var resp map[string]any
		decodeJSON(t, w, &resp)
		if resp["username"] != "alice" {
			t.Errorf("username: got %q, want alice", resp["username"])
		}
	})

	t.Run("401 without a session", func(t *testing.T) {
		ts := newTestServer(t)
		w := ts.do(ts.req(t, http.MethodGet, "/api/auth/me", ""))
		assertStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("401 when user is deleted mid-session", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		req := ts.authReq(t, http.MethodGet, "/api/auth/me", "", alice.ID, false)
		if err := ts.db.DeleteUser(alice.ID); err != nil {
			t.Fatalf("delete user: %v", err)
		}
		w := ts.do(req)
		assertStatus(t, w, http.StatusUnauthorized)
	})
}

// ── ChangePassword ────────────────────────────────────────────────────────────

func TestChangePassword(t *testing.T) {
	t.Run("success returns 204", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPatch, "/api/auth/password",
			`{"current_password":"password123","new_password":"newpassword99"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusNoContent)
	})

	t.Run("400 when current password is wrong", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPatch, "/api/auth/password",
			`{"current_password":"wrongpassword","new_password":"newpassword99"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when new password is too short", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPatch, "/api/auth/password",
			`{"current_password":"password123","new_password":"short"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})
}

// ── ListTasks ─────────────────────────────────────────────────────────────────

func TestListTasks(t *testing.T) {
	t.Run("returns tasks for valid month", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		ts.mustTask(t, "Pay rent", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/tasks?month=2026-04", "", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var tasks []map[string]any
		decodeJSON(t, w, &tasks)
		if len(tasks) != 1 {
			t.Errorf("expected 1 task, got %d", len(tasks))
		}
		if tasks[0]["title"] != "Pay rent" {
			t.Errorf("title: got %q, want 'Pay rent'", tasks[0]["title"])
		}
	})

	t.Run("400 when month param is missing", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/tasks", "", alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 on invalid month format", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		for _, bad := range []string{"2026-13", "2026-00", "not-a-date", "2026/04"} {
			w := ts.do(ts.authReq(t, http.MethodGet, "/api/tasks?month="+bad, "", alice.ID, false))
			if w.Code != http.StatusBadRequest {
				t.Errorf("month=%q: got %d, want 400", bad, w.Code)
			}
		}
	})

	t.Run("user cannot see another user's tasks", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		ts.mustTask(t, "Alice task", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/tasks?month=2026-04", "", bob.ID, false))
		assertStatus(t, w, http.StatusOK)
		var tasks []map[string]any
		decodeJSON(t, w, &tasks)
		if len(tasks) != 0 {
			t.Errorf("bob should see 0 tasks, got %d", len(tasks))
		}
	})
}

// ── CreateTask ────────────────────────────────────────────────────────────────

func TestCreateTask(t *testing.T) {
	t.Run("valid body returns 201 with task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			`{"title":"Pay rent","type":"payment"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusCreated)
		var task map[string]any
		decodeJSON(t, w, &task)
		if task["title"] != "Pay rent" {
			t.Errorf("title: got %q, want 'Pay rent'", task["title"])
		}
		if task["type"] != "payment" {
			t.Errorf("type: got %q, want payment", task["type"])
		}
	})

	t.Run("400 when title is empty", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			`{"title":"","type":"payment"}`, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when title exceeds 200 characters", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			fmt.Sprintf(`{"title":%q}`, strings.Repeat("x", 201)),
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 on invalid type", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			`{"title":"Task","type":"invalid"}`, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 on invalid interval", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			`{"title":"Task","interval":5}`, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("accepts all valid task types", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		for _, typ := range []string{"payment", "subscription", "bill", "reminder", ""} {
			w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
				fmt.Sprintf(`{"title":"Task %s","type":%q}`, typ, typ),
				alice.ID, false))
			if w.Code != http.StatusCreated {
				t.Errorf("type=%q: got %d, want 201", typ, w.Code)
			}
		}
	})

	t.Run("accepts all valid intervals", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		for _, iv := range []int{1, 2, 3, 6, 12} {
			w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
				fmt.Sprintf(`{"title":"Task %d","interval":%d}`, iv, iv),
				alice.ID, false))
			if w.Code != http.StatusCreated {
				t.Errorf("interval=%d: got %d, want 201", iv, w.Code)
			}
		}
	})

	t.Run("400 when metadata exceeds 4096 bytes", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		// Build a metadata object whose JSON encoding exceeds 4096 bytes.
		bigVal := strings.Repeat("x", 4097)
		body := fmt.Sprintf(`{"title":"Task","metadata":{"k":%q}}`, bigVal)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks", body, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})
}

// ── UpdateTask ────────────────────────────────────────────────────────────────

func TestUpdateTask(t *testing.T) {
	t.Run("success returns updated task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Old title", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodPut,
			fmt.Sprintf("/api/tasks/%d", task.ID),
			`{"title":"New title"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var updated map[string]any
		decodeJSON(t, w, &updated)
		if updated["title"] != "New title" {
			t.Errorf("title: got %q, want 'New title'", updated["title"])
		}
	})

	t.Run("404 for another user's task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		task := ts.mustTask(t, "Alice task", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodPut,
			fmt.Sprintf("/api/tasks/%d", task.ID),
			`{"title":"Stolen"}`,
			bob.ID, false))
		assertStatus(t, w, http.StatusNotFound)
	})
}

// ── DeleteTask ────────────────────────────────────────────────────────────────

func TestDeleteTask(t *testing.T) {
	t.Run("success returns 204", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Pay rent", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/tasks/%d", task.ID), "",
			alice.ID, false))
		assertStatus(t, w, http.StatusNoContent)
	})

	t.Run("404 for another user's task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		task := ts.mustTask(t, "Alice task", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/tasks/%d", task.ID), "",
			bob.ID, false))
		assertStatus(t, w, http.StatusNotFound)
	})
}

// ── ToggleCompletion ──────────────────────────────────────────────────────────

func TestToggleCompletion(t *testing.T) {
	t.Run("marks incomplete task as done", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Pay rent", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/completions/toggle",
			fmt.Sprintf(`{"task_id":%d,"month":"2026-04"}`, task.ID),
			alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var resp map[string]bool
		decodeJSON(t, w, &resp)
		if !resp["completed"] {
			t.Error("expected completed=true after first toggle")
		}
	})

	t.Run("unmarks a done task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Pay rent", alice.ID)
		if _, err := ts.db.AddCompletion(task.ID, "2026-04"); err != nil {
			t.Fatalf("seed completion: %v", err)
		}
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/completions/toggle",
			fmt.Sprintf(`{"task_id":%d,"month":"2026-04"}`, task.ID),
			alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var resp map[string]bool
		decodeJSON(t, w, &resp)
		if resp["completed"] {
			t.Error("expected completed=false after untoggling a done task")
		}
	})

	t.Run("400 on invalid month format", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Pay rent", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/completions/toggle",
			fmt.Sprintf(`{"task_id":%d,"month":"not-a-month"}`, task.ID),
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("404 for another user's task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		task := ts.mustTask(t, "Alice task", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/completions/toggle",
			fmt.Sprintf(`{"task_id":%d,"month":"2026-04"}`, task.ID),
			bob.ID, false))
		assertStatus(t, w, http.StatusNotFound)
	})
}

// ── PatchCompletion ───────────────────────────────────────────────────────────

func TestPatchCompletion(t *testing.T) {
	// setup creates a user, a task, and marks it done for 2026-04.
	setup := func(t *testing.T) (*testServer, User, Task) {
		t.Helper()
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Pay rent", alice.ID)
		if _, err := ts.db.AddCompletion(task.ID, "2026-04"); err != nil {
			t.Fatalf("seed completion: %v", err)
		}
		return ts, alice, task
	}

	t.Run("sets amount on completed task", func(t *testing.T) {
		ts, alice, task := setup(t)
		w := ts.do(ts.authReq(t, http.MethodPatch,
			fmt.Sprintf("/api/completions/%d/2026-04", task.ID),
			`{"amount":"42.50"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var c map[string]any
		decodeJSON(t, w, &c)
		if c["amount"] != "42.50" {
			t.Errorf("amount: got %q, want '42.50'", c["amount"])
		}
	})

	t.Run("sets note on completed task", func(t *testing.T) {
		ts, alice, task := setup(t)
		w := ts.do(ts.authReq(t, http.MethodPatch,
			fmt.Sprintf("/api/completions/%d/2026-04", task.ID),
			`{"note":"paid via card"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var c map[string]any
		decodeJSON(t, w, &c)
		if c["note"] != "paid via card" {
			t.Errorf("note: got %q, want 'paid via card'", c["note"])
		}
	})

	t.Run("400 on negative amount", func(t *testing.T) {
		ts, alice, task := setup(t)
		w := ts.do(ts.authReq(t, http.MethodPatch,
			fmt.Sprintf("/api/completions/%d/2026-04", task.ID),
			`{"amount":"-1"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 on non-numeric amount", func(t *testing.T) {
		ts, alice, task := setup(t)
		w := ts.do(ts.authReq(t, http.MethodPatch,
			fmt.Sprintf("/api/completions/%d/2026-04", task.ID),
			`{"amount":"abc"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when no fields provided", func(t *testing.T) {
		ts, alice, task := setup(t)
		w := ts.do(ts.authReq(t, http.MethodPatch,
			fmt.Sprintf("/api/completions/%d/2026-04", task.ID),
			`{}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when task is not marked done", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Not done", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodPatch,
			fmt.Sprintf("/api/completions/%d/2026-04", task.ID),
			`{"amount":"10"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})
}

// ── GetSettings ───────────────────────────────────────────────────────────────

func TestGetSettings(t *testing.T) {
	ts := newTestServer(t)
	alice := ts.mustUser(t, "alice", false)
	w := ts.do(ts.authReq(t, http.MethodGet, "/api/settings", "", alice.ID, false))
	assertStatus(t, w, http.StatusOK)
	var settings map[string]string
	decodeJSON(t, w, &settings)
	if _, ok := settings["currency"]; !ok {
		t.Error("expected settings to include a 'currency' key")
	}
}

// ── UpdateSettings ────────────────────────────────────────────────────────────

func TestUpdateSettings(t *testing.T) {
	t.Run("persists currency, date_format and color_mode", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPut, "/api/settings",
			`{"currency":"€","date_format":"short","color_mode":"dark"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var settings map[string]string
		decodeJSON(t, w, &settings)
		if settings["currency"] != "€" {
			t.Errorf("currency: got %q, want €", settings["currency"])
		}
		if settings["date_format"] != "short" {
			t.Errorf("date_format: got %q, want short", settings["date_format"])
		}
		if settings["color_mode"] != "dark" {
			t.Errorf("color_mode: got %q, want dark", settings["color_mode"])
		}
	})

	t.Run("settings are isolated per user", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		ts.do(ts.authReq(t, http.MethodPut, "/api/settings",
			`{"currency":"£"}`, alice.ID, false))
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/settings", "", bob.ID, false))
		assertStatus(t, w, http.StatusOK)
		var bobSettings map[string]string
		decodeJSON(t, w, &bobSettings)
		if bobSettings["currency"] == "£" {
			t.Error("bob should not see alice's currency setting")
		}
	})

	t.Run("400 on invalid date_format", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPut, "/api/settings",
			`{"date_format":"invalid"}`, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 on invalid color_mode", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPut, "/api/settings",
			`{"color_mode":"rainbow"}`, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when currency exceeds 10 characters", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPut, "/api/settings",
			`{"currency":"TOOLONGCURRENCY"}`, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})
}

// ── ExportCSV ─────────────────────────────────────────────────────────────────

func TestExportCSV(t *testing.T) {
	t.Run("returns CSV with header row and correct headers", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet,
			"/api/export/completions.csv?from=2026-01&to=2026-04",
			"", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
			t.Errorf("Content-Type: got %q, want text/csv", ct)
		}
		if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
			t.Error("expected Content-Disposition: attachment")
		}
		if !strings.Contains(w.Body.String(), "Title,Type,Month,Status,Amount,Has Receipt") {
			t.Errorf("missing CSV header row; body: %s", w.Body.String())
		}
	})

	t.Run("includes completed tasks in range", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Pay rent", alice.ID)
		ts.db.AddCompletion(task.ID, "2026-03")
		w := ts.do(ts.authReq(t, http.MethodGet,
			"/api/export/completions.csv?from=2026-01&to=2026-04",
			"", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		if !strings.Contains(w.Body.String(), "Pay rent") {
			t.Error("expected CSV to contain the completed task title")
		}
	})

	t.Run("excludes tasks outside range", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Outside task", alice.ID)
		ts.db.AddCompletion(task.ID, "2025-06")
		w := ts.do(ts.authReq(t, http.MethodGet,
			"/api/export/completions.csv?from=2026-01&to=2026-04",
			"", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		if strings.Contains(w.Body.String(), "Outside task") {
			t.Error("CSV should not include tasks completed outside the range")
		}
	})

	t.Run("400 on invalid from", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet,
			"/api/export/completions.csv?from=badformat",
			"", alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 on invalid to", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet,
			"/api/export/completions.csv?to=2026-13",
			"", alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when from is after to", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet,
			"/api/export/completions.csv?from=2026-06&to=2026-01",
			"", alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})
}

// ── Validation helpers ────────────────────────────────────────────────────────

func TestIsValidYearMonth(t *testing.T) {
	valid := []string{"2026-01", "2026-12", "0001-01", "9999-12"}
	for _, s := range valid {
		if !isValidYearMonth(s) {
			t.Errorf("isValidYearMonth(%q): expected true", s)
		}
	}
	invalid := []string{"", "2026-0", "2026-13", "2026-00", "202604", "2026/04", "not-date", "2026-1"}
	for _, s := range invalid {
		if isValidYearMonth(s) {
			t.Errorf("isValidYearMonth(%q): expected false", s)
		}
	}
}

func TestIsValidReceiptFilename(t *testing.T) {
	valid := []string{
		"550e8400-e29b-41d4-a716-446655440000.pdf",
		"550e8400-e29b-41d4-a716-446655440000.jpg",
		"550e8400-e29b-41d4-a716-446655440000.jpeg",
		"550e8400-e29b-41d4-a716-446655440000.png",
		"550e8400-e29b-41d4-a716-446655440000.webp",
		"550e8400-e29b-41d4-a716-446655440000.gif",
	}
	for _, f := range valid {
		if !isValidReceiptFilename(f) {
			t.Errorf("isValidReceiptFilename(%q): expected true", f)
		}
	}
	invalid := []string{
		"",
		"../etc/passwd",
		"../evil.pdf",
		"notauuid.pdf",
		"550e8400-e29b-41d4-a716-446655440000.exe",
		"550e8400-e29b-41d4-a716-446655440000",
		"550e8400-e29b-41d4-a716-446655440000.PDF", // uppercase not allowed
	}
	for _, f := range invalid {
		if isValidReceiptFilename(f) {
			t.Errorf("isValidReceiptFilename(%q): expected false", f)
		}
	}
}

func TestIsAllowedMIME(t *testing.T) {
	allowed := []string{
		"image/jpeg",
		"image/jpeg; charset=utf-8",
		"image/png",
		"image/gif",
		"image/webp",
		"application/pdf",
	}
	for _, m := range allowed {
		if !isAllowedMIME(m) {
			t.Errorf("isAllowedMIME(%q): expected true", m)
		}
	}
	rejected := []string{
		"text/html",
		"application/octet-stream",
		"application/javascript",
		"image/svg+xml",
		"text/plain",
	}
	for _, m := range rejected {
		if isAllowedMIME(m) {
			t.Errorf("isAllowedMIME(%q): expected false", m)
		}
	}
}

// ── Phase 3: security, auth middleware, remaining handler gaps ────────────────

// ── safeRemoveReceipt ─────────────────────────────────────────────────────────

func TestSafeRemoveReceipt(t *testing.T) {
	t.Run("removes a file with a valid UUID filename", func(t *testing.T) {
		dir := t.TempDir()
		name := "550e8400-e29b-41d4-a716-446655440000.pdf"
		path := dir + "/" + name
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		safeRemoveReceipt(dir, name)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("expected file to be removed")
		}
	})

	t.Run("no-op for empty filename", func(t *testing.T) {
		dir := t.TempDir()
		// Should not panic or error.
		safeRemoveReceipt(dir, "")
	})

	t.Run("no-op for non-UUID filename (path traversal attempt)", func(t *testing.T) {
		dir := t.TempDir()
		// Write a file with a non-UUID name — safeRemoveReceipt must not touch it.
		name := "important.txt"
		path := dir + "/" + name
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		safeRemoveReceipt(dir, "../"+name)
		safeRemoveReceipt(dir, name) // also test without traversal — still blocked
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("non-UUID file should not have been removed")
		}
	})
}

// ── parseTaskBody additional paths ───────────────────────────────────────────

func TestParseTaskBody(t *testing.T) {
	t.Run("400 when description exceeds 5000 characters", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			fmt.Sprintf(`{"title":"T","description":%q}`, strings.Repeat("x", 5001)),
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when start_date has wrong format", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			`{"title":"T","start_date":"01/2026"}`, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when end_date has wrong format", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			`{"title":"T","end_date":"2026/04"}`, alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("valid start_date and end_date are accepted", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/tasks",
			`{"title":"T","start_date":"2026-01","end_date":"2026-12"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusCreated)
		var task map[string]any
		decodeJSON(t, w, &task)
		if task["start_date"] != "2026-01" {
			t.Errorf("start_date: got %q", task["start_date"])
		}
		if task["end_date"] != "2026-12" {
			t.Errorf("end_date: got %q", task["end_date"])
		}
	})
}

// ── requireAuth: Bearer token path ───────────────────────────────────────────

func TestRequireAuth_BearerToken(t *testing.T) {
	t.Run("accepts a valid API token via Authorization header", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)

		// Create a token directly in the DB so we know the plaintext.
		plaintext := "mt_testtoken123"
		hash := sha256hex(plaintext)
		ts.db.CreateToken(alice.ID, "test", hash)

		req := ts.req(t, http.MethodGet, "/api/auth/me", "")
		req.Header.Set("Authorization", "Bearer "+plaintext)
		w := ts.do(req)
		assertStatus(t, w, http.StatusOK)
		var resp map[string]any
		decodeJSON(t, w, &resp)
		if resp["username"] != "alice" {
			t.Errorf("username: got %q, want alice", resp["username"])
		}
	})

	t.Run("401 with an invalid Bearer token", func(t *testing.T) {
		ts := newTestServer(t)
		req := ts.req(t, http.MethodGet, "/api/auth/me", "")
		req.Header.Set("Authorization", "Bearer invalidtoken")
		w := ts.do(req)
		assertStatus(t, w, http.StatusUnauthorized)
	})
}

// ── requireAdmin middleware ───────────────────────────────────────────────────

func TestRequireAdmin(t *testing.T) {
	t.Run("403 when non-admin accesses admin-only endpoint", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false) // not admin
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/users", "", alice.ID, false))
		assertStatus(t, w, http.StatusForbidden)
	})

	t.Run("admin can access admin-only endpoint", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/users", "", admin.ID, true))
		assertStatus(t, w, http.StatusOK)
	})
}

// ── Admin user management ─────────────────────────────────────────────────────

func TestListUsers(t *testing.T) {
	ts := newTestServer(t)
	admin := ts.mustUser(t, "admin", true)
	ts.mustUser(t, "alice", false)
	ts.mustUser(t, "bob", false)

	w := ts.do(ts.authReq(t, http.MethodGet, "/api/users", "", admin.ID, true))
	assertStatus(t, w, http.StatusOK)
	var users []map[string]any
	decodeJSON(t, w, &users)
	if len(users) != 3 {
		t.Errorf("got %d users, want 3", len(users))
	}
	// Password hash must never be in the response.
	for _, u := range users {
		if _, ok := u["password_hash"]; ok {
			t.Error("password_hash must not be serialised")
		}
	}
}

func TestAdminCreateUser(t *testing.T) {
	t.Run("admin creates a new user", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/users",
			`{"username":"newuser","password":"password123","is_admin":false}`,
			admin.ID, true))
		assertStatus(t, w, http.StatusCreated)
		var u map[string]any
		decodeJSON(t, w, &u)
		if u["username"] != "newuser" {
			t.Errorf("username: got %q", u["username"])
		}
		if u["is_admin"].(bool) {
			t.Error("expected is_admin=false")
		}
	})

	t.Run("409 on duplicate username", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		ts.mustUser(t, "existing", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/users",
			`{"username":"existing","password":"password123"}`,
			admin.ID, true))
		assertStatus(t, w, http.StatusConflict)
	})

	t.Run("400 when password is too short", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/users",
			`{"username":"newuser","password":"short"}`,
			admin.ID, true))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when username is empty", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/users",
			`{"username":"","password":"password123"}`,
			admin.ID, true))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("403 when non-admin tries to create a user", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/users",
			`{"username":"newuser","password":"password123"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusForbidden)
	})
}

func TestAdminDeleteUser(t *testing.T) {
	t.Run("admin deletes another user", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/users/%d", alice.ID), "",
			admin.ID, true))
		assertStatus(t, w, http.StatusNoContent)
	})

	t.Run("400 when admin tries to delete themselves", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/users/%d", admin.ID), "",
			admin.ID, true))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when deleting the last admin", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		alice := ts.mustUser(t, "alice", false)
		// alice is not admin; admin is the only admin
		_ = alice
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/users/%d", admin.ID), "",
			admin.ID, true))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("404 for non-existent user", func(t *testing.T) {
		ts := newTestServer(t)
		admin := ts.mustUser(t, "admin", true)
		w := ts.do(ts.authReq(t, http.MethodDelete, "/api/users/99999", "",
			admin.ID, true))
		assertStatus(t, w, http.StatusNotFound)
	})
}

// ── Token management ──────────────────────────────────────────────────────────

func TestListTokens(t *testing.T) {
	ts := newTestServer(t)
	alice := ts.mustUser(t, "alice", false)

	// Empty list returns [].
	w := ts.do(ts.authReq(t, http.MethodGet, "/api/auth/tokens", "", alice.ID, false))
	assertStatus(t, w, http.StatusOK)
	var tokens []any
	decodeJSON(t, w, &tokens)
	if len(tokens) != 0 {
		t.Errorf("expected empty list, got %d tokens", len(tokens))
	}

	// After creating a token via DB, it appears in the list.
	ts.db.CreateToken(alice.ID, "my token", "hash1")
	w = ts.do(ts.authReq(t, http.MethodGet, "/api/auth/tokens", "", alice.ID, false))
	assertStatus(t, w, http.StatusOK)
	decodeJSON(t, w, &tokens)
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}
}

func TestCreateToken(t *testing.T) {
	t.Run("creates token and returns plaintext once", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/auth/tokens",
			`{"name":"ci-token"}`, alice.ID, false))
		assertStatus(t, w, http.StatusCreated)
		var resp map[string]any
		decodeJSON(t, w, &resp)
		plaintext, ok := resp["plaintext"].(string)
		if !ok || !strings.HasPrefix(plaintext, "mt_") {
			t.Errorf("plaintext: got %q, want mt_... prefix", plaintext)
		}
		tok, ok := resp["token"].(map[string]any)
		if !ok {
			t.Fatal("expected token object in response")
		}
		if tok["name"] != "ci-token" {
			t.Errorf("token.name: got %q, want ci-token", tok["name"])
		}
	})

	t.Run("400 when name exceeds 100 characters", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/auth/tokens",
			fmt.Sprintf(`{"name":%q}`, strings.Repeat("a", 101)),
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})
}

func TestRevokeToken(t *testing.T) {
	t.Run("revokes own token", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		tok, _ := ts.db.CreateToken(alice.ID, "test", "hash1")
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/auth/tokens/%d", tok.ID), "",
			alice.ID, false))
		assertStatus(t, w, http.StatusNoContent)
	})

	t.Run("404 when revoking another user's token", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		tok, _ := ts.db.CreateToken(alice.ID, "alice-token", "hash2")
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/auth/tokens/%d", tok.ID), "",
			bob.ID, false))
		assertStatus(t, w, http.StatusNotFound)
	})
}

// ── GetTask ───────────────────────────────────────────────────────────────────

func TestGetTask(t *testing.T) {
	t.Run("returns own task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Pay rent", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodGet,
			fmt.Sprintf("/api/tasks/%d", task.ID), "", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var got map[string]any
		decodeJSON(t, w, &got)
		if got["title"] != "Pay rent" {
			t.Errorf("title: got %q", got["title"])
		}
	})

	t.Run("404 for another user's task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		task := ts.mustTask(t, "Alice task", alice.ID)
		w := ts.do(ts.authReq(t, http.MethodGet,
			fmt.Sprintf("/api/tasks/%d", task.ID), "", bob.ID, false))
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("404 for non-existent task", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/tasks/99999", "", alice.ID, false))
		assertStatus(t, w, http.StatusNotFound)
	})
}

// ── ListCompletions ───────────────────────────────────────────────────────────

func TestListCompletions(t *testing.T) {
	t.Run("returns completions for month", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		task := ts.mustTask(t, "Pay rent", alice.ID)
		ts.db.AddCompletion(task.ID, "2026-04")

		w := ts.do(ts.authReq(t, http.MethodGet, "/api/completions?month=2026-04",
			"", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var completions []map[string]any
		decodeJSON(t, w, &completions)
		if len(completions) != 1 {
			t.Errorf("expected 1 completion, got %d", len(completions))
		}
	})

	t.Run("400 when month param is missing", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/completions", "", alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("user cannot see another user's completions", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		task := ts.mustTask(t, "Alice task", alice.ID)
		ts.db.AddCompletion(task.ID, "2026-04")

		w := ts.do(ts.authReq(t, http.MethodGet, "/api/completions?month=2026-04",
			"", bob.ID, false))
		assertStatus(t, w, http.StatusOK)
		var completions []map[string]any
		decodeJSON(t, w, &completions)
		if len(completions) != 0 {
			t.Errorf("bob should see 0 completions, got %d", len(completions))
		}
	})
}

// ── Phase 4: webhook HTTP handlers and FireWebhooks delivery ─────────────────

// ── Webhook HTTP handlers ─────────────────────────────────────────────────────

func TestWebhooksHTTP_List(t *testing.T) {
	t.Run("returns empty array (not null) when no webhooks", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodGet, "/api/webhooks", "", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		// Must decode as array, not null.
		var hooks []map[string]any
		decodeJSON(t, w, &hooks)
		if hooks == nil {
			t.Error("expected empty array [], got null")
		}
		if len(hooks) != 0 {
			t.Errorf("expected 0 hooks, got %d", len(hooks))
		}
	})

	t.Run("returns created webhook without secret field", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		ts.db.CreateWebhook(alice.ID, "https://example.com/hook", "task.completed", "s3cr3t")

		w := ts.do(ts.authReq(t, http.MethodGet, "/api/webhooks", "", alice.ID, false))
		assertStatus(t, w, http.StatusOK)
		var hooks []map[string]any
		decodeJSON(t, w, &hooks)
		if len(hooks) != 1 {
			t.Fatalf("expected 1 hook, got %d", len(hooks))
		}
		if hooks[0]["url"] != "https://example.com/hook" {
			t.Errorf("url: got %q", hooks[0]["url"])
		}
		if _, hasSecret := hooks[0]["secret"]; hasSecret {
			t.Error("secret must not be returned in list response")
		}
	})

	t.Run("user cannot see another user's webhooks", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		ts.db.CreateWebhook(alice.ID, "https://example.com/hook", "task.completed", "")

		w := ts.do(ts.authReq(t, http.MethodGet, "/api/webhooks", "", bob.ID, false))
		assertStatus(t, w, http.StatusOK)
		var hooks []map[string]any
		decodeJSON(t, w, &hooks)
		if len(hooks) != 0 {
			t.Errorf("bob should see 0 webhooks, got %d", len(hooks))
		}
	})
}

func TestWebhooksHTTP_Create(t *testing.T) {
	t.Run("creates webhook with valid URL and events", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/webhooks",
			`{"url":"https://example.com/hook","events":["task.completed"],"secret":"mysecret"}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusCreated)
		var hook map[string]any
		decodeJSON(t, w, &hook)
		if hook["url"] != "https://example.com/hook" {
			t.Errorf("url: got %q", hook["url"])
		}
		if hook["events"] != "task.completed" {
			t.Errorf("events: got %q, want task.completed", hook["events"])
		}
		// Secret must not appear in the create response.
		if _, hasSecret := hook["secret"]; hasSecret {
			t.Error("secret must not be returned in create response")
		}
	})

	t.Run("accepts both event types", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/webhooks",
			`{"url":"https://example.com/h","events":["task.completed","task.uncompleted"]}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusCreated)
	})

	t.Run("400 when URL scheme is not http/https", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/webhooks",
			`{"url":"ftp://example.com/hook","events":["task.completed"]}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when URL is empty", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/webhooks",
			`{"url":"","events":["task.completed"]}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when no events provided", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/webhooks",
			`{"url":"https://example.com/hook","events":[]}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 on unknown event name", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/webhooks",
			`{"url":"https://example.com/hook","events":["task.deleted"]}`,
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("400 when secret exceeds 200 characters", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		w := ts.do(ts.authReq(t, http.MethodPost, "/api/webhooks",
			fmt.Sprintf(`{"url":"https://example.com/hook","events":["task.completed"],"secret":%q}`,
				strings.Repeat("s", 201)),
			alice.ID, false))
		assertStatus(t, w, http.StatusBadRequest)
	})
}

func TestWebhooksHTTP_Delete(t *testing.T) {
	t.Run("deletes own webhook", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		wh, _ := ts.db.CreateWebhook(alice.ID, "https://example.com/hook", "task.completed", "")
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/webhooks/%d", wh.ID), "",
			alice.ID, false))
		assertStatus(t, w, http.StatusNoContent)
	})

	t.Run("404 when deleting another user's webhook", func(t *testing.T) {
		ts := newTestServer(t)
		alice := ts.mustUser(t, "alice", false)
		bob := ts.mustUser(t, "bob", false)
		wh, _ := ts.db.CreateWebhook(alice.ID, "https://example.com/hook", "task.completed", "")
		w := ts.do(ts.authReq(t, http.MethodDelete,
			fmt.Sprintf("/api/webhooks/%d", wh.ID), "",
			bob.ID, false))
		assertStatus(t, w, http.StatusNotFound)
	})
}

// ── FireWebhooks ──────────────────────────────────────────────────────────────

// webhookCapture is a test HTTP server that captures incoming deliveries.
type webhookCapture struct {
	hits chan webhookHit
	srv  *httptest.Server
}

type webhookHit struct {
	body    []byte
	headers http.Header
}

func newWebhookCapture() *webhookCapture {
	c := &webhookCapture{hits: make(chan webhookHit, 10)}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.hits <- webhookHit{body: body, headers: r.Header.Clone()}
		w.WriteHeader(http.StatusOK)
	}))
	return c
}

func (c *webhookCapture) close() { c.srv.Close() }

// waitHit waits up to 2 seconds for a delivery and returns it, or fails the test.
func (c *webhookCapture) waitHit(t *testing.T) webhookHit {
	t.Helper()
	select {
	case hit := <-c.hits:
		return hit
	case <-time.After(2 * time.Second):
		t.Fatal("webhook delivery not received within 2s")
		return webhookHit{}
	}
}

// expectNoHit asserts no delivery arrives within 200 ms.
func (c *webhookCapture) expectNoHit(t *testing.T) {
	t.Helper()
	select {
	case <-c.hits:
		t.Error("unexpected webhook delivery received")
	case <-time.After(200 * time.Millisecond):
		// good — nothing arrived
	}
}

func TestFireWebhooks_Delivery(t *testing.T) {
	t.Run("delivers payload with correct fields", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Pay rent", "", "payment", "2020-01", "", nil, user.ID, 1)

		cap := newWebhookCapture()
		defer cap.close()
		db.CreateWebhook(user.ID, cap.srv.URL, "task.completed", "")

		FireWebhooks(db, user.ID, "task.completed", task.ID, task.Title, "2026-04")

		hit := cap.waitHit(t)

		var payload webhookPayload
		if err := json.Unmarshal(hit.body, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.Event != "task.completed" {
			t.Errorf("event: got %q, want task.completed", payload.Event)
		}
		if payload.TaskID != task.ID {
			t.Errorf("task_id: got %d, want %d", payload.TaskID, task.ID)
		}
		if payload.TaskTitle != "Pay rent" {
			t.Errorf("task_title: got %q, want 'Pay rent'", payload.TaskTitle)
		}
		if payload.Month != "2026-04" {
			t.Errorf("month: got %q, want 2026-04", payload.Month)
		}
		if payload.Timestamp == "" {
			t.Error("timestamp should not be empty")
		}
	})

	t.Run("sets correct Content-Type header", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, user.ID, 1)

		cap := newWebhookCapture()
		defer cap.close()
		db.CreateWebhook(user.ID, cap.srv.URL, "task.completed", "")

		FireWebhooks(db, user.ID, "task.completed", task.ID, task.Title, "2026-04")

		hit := cap.waitHit(t)
		if ct := hit.headers.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type: got %q, want application/json", ct)
		}
	})

	t.Run("includes HMAC signature when secret is set", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, user.ID, 1)
		secret := "webhook-secret-123"

		cap := newWebhookCapture()
		defer cap.close()
		db.CreateWebhook(user.ID, cap.srv.URL, "task.completed", secret)

		FireWebhooks(db, user.ID, "task.completed", task.ID, task.Title, "2026-04")

		hit := cap.waitHit(t)

		sig := hit.headers.Get("X-Montly-Signature")
		if sig == "" {
			t.Fatal("expected X-Montly-Signature header to be set")
		}

		// Recompute expected HMAC and compare.
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(hit.body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if sig != expected {
			t.Errorf("signature mismatch:\n  got  %q\n  want %q", sig, expected)
		}
	})

	t.Run("no signature header when secret is empty", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, user.ID, 1)

		cap := newWebhookCapture()
		defer cap.close()
		db.CreateWebhook(user.ID, cap.srv.URL, "task.completed", "") // no secret

		FireWebhooks(db, user.ID, "task.completed", task.ID, task.Title, "2026-04")

		hit := cap.waitHit(t)
		if sig := hit.headers.Get("X-Montly-Signature"); sig != "" {
			t.Errorf("expected no signature header, got %q", sig)
		}
	})
}

func TestFireWebhooks_EventFilter(t *testing.T) {
	t.Run("only fires webhook subscribed to the matching event", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, user.ID, 1)

		completedCap := newWebhookCapture()
		defer completedCap.close()
		uncompletedCap := newWebhookCapture()
		defer uncompletedCap.close()

		db.CreateWebhook(user.ID, completedCap.srv.URL, "task.completed", "")
		db.CreateWebhook(user.ID, uncompletedCap.srv.URL, "task.uncompleted", "")

		// Fire only the completed event.
		FireWebhooks(db, user.ID, "task.completed", task.ID, task.Title, "2026-04")

		completedCap.waitHit(t)          // must arrive
		uncompletedCap.expectNoHit(t)    // must NOT arrive
	})

	t.Run("fires webhook subscribed to both events", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, user.ID, 1)

		cap := newWebhookCapture()
		defer cap.close()
		db.CreateWebhook(user.ID, cap.srv.URL, "task.completed,task.uncompleted", "")

		FireWebhooks(db, user.ID, "task.uncompleted", task.ID, task.Title, "2026-04")
		cap.waitHit(t) // must arrive for uncompleted too
	})

	t.Run("does not fire when user has no webhooks", func(t *testing.T) {
		db := setupTestDB(t)
		user, _ := db.CreateUser("alice", testHash(t), false)
		task, _ := db.CreateTask("Task", "", "", "2020-01", "", nil, user.ID, 1)

		cap := newWebhookCapture()
		defer cap.close()
		// No webhooks registered — cap should receive nothing.
		FireWebhooks(db, user.ID, "task.completed", task.ID, task.Title, "2026-04")
		cap.expectNoHit(t)
	})
}
