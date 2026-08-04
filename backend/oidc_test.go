package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ── fakes & helpers ─────────────────────────────────────────────────────────

type fakeOIDCProvider struct {
	authURL string
	claims  *OIDCClaims
	err     error
}

func (f *fakeOIDCProvider) AuthCodeURL(state, nonce, verifier string) string {
	if f.authURL == "" {
		f.authURL = "https://idp.example/authorize"
	}
	return f.authURL + "?state=" + state + "&nonce=" + nonce
}

func (f *fakeOIDCProvider) Exchange(_ context.Context, _, _, _ string) (*OIDCClaims, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.claims, nil
}

func testOIDCCfg() *OIDCConfig {
	return &OIDCConfig{
		Issuer:         "https://idp.example",
		ProviderName:   "Test IdP",
		UsernameClaim:  "preferred_username",
		GroupsClaim:    "groups",
		AllowSignup:    true,
		LinkByEmail:    true,
		LinkByUsername: true,
	}
}

func newOIDCHandler(t *testing.T, cfg *OIDCConfig, provider OIDCProvider) (*AuthHandler, *DB) {
	t.Helper()
	db := setupTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &AuthHandler{
		db:      db,
		secret:  []byte("oidc-test-secret"),
		rl:      newRateLimiter(ctx),
		oidc:    provider,
		oidcCfg: cfg,
	}, db
}

func setUserEmail(t *testing.T, db *DB, userID int64, email string) {
	t.Helper()
	if _, err := db.Exec(db.q(`UPDATE users SET email = ? WHERE id = ?`), email, userID); err != nil {
		t.Fatalf("set email: %v", err)
	}
}

// ── config parsing ──────────────────────────────────────────────────────────

func TestParseScopes(t *testing.T) {
	got := parseScopes("profile,email openid profile")
	// openid always first, deduped, order preserved.
	want := []string{"openid", "profile", "email"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("X_FLAG", "yes")
	if !envBool("X_FLAG", false) {
		t.Error("yes should be true")
	}
	t.Setenv("X_FLAG", "0")
	if envBool("X_FLAG", true) {
		t.Error("0 should be false")
	}
	if !envBool("X_MISSING", true) {
		t.Error("missing should return default")
	}
}

func TestLoadOIDCConfig(t *testing.T) {
	t.Setenv("OIDC_ISSUER", "https://idp.example/")
	t.Setenv("OIDC_CLIENT_ID", "cid")
	t.Setenv("OIDC_CLIENT_SECRET", "secret")
	t.Setenv("OIDC_REDIRECT_URL", "https://app.example/api/auth/oidc/callback")
	t.Setenv("OIDC_ADMIN_GROUP", "montly-admins")

	cfg := loadOIDCConfig()
	if cfg == nil {
		t.Fatal("expected config")
	}
	if cfg.Issuer != "https://idp.example" { // trailing slash trimmed
		t.Errorf("issuer = %q", cfg.Issuer)
	}
	if !cfg.AllowSignup || !cfg.LinkByEmail || !cfg.LinkByUsername {
		t.Error("defaults should be on")
	}
	if cfg.AdminGroup != "montly-admins" {
		t.Errorf("admin group = %q", cfg.AdminGroup)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestLoadOIDCConfigDisabled(t *testing.T) {
	t.Setenv("OIDC_ISSUER", "")
	if loadOIDCConfig() != nil {
		t.Error("expected nil config when issuer unset")
	}
}

func TestOIDCConfigValidate(t *testing.T) {
	cfg := &OIDCConfig{Issuer: "https://idp.example"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing client id")
	}
	cfg.ClientID = "c"
	cfg.ClientSecret = "s"
	cfg.RedirectURL = "not-a-url"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for bad redirect url")
	}
}

// ── state cookie ────────────────────────────────────────────────────────────

func TestOIDCStateRoundTrip(t *testing.T) {
	secret := []byte("s3cr3t")
	st := oidcState{State: "abc", Nonce: "xyz", Verifier: "verif", Expires: time.Now().Add(time.Minute).Unix()}
	token, err := signOIDCState(st, secret)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parseOIDCState(token, secret)
	if !ok || got.State != "abc" || got.Nonce != "xyz" || got.Verifier != "verif" {
		t.Fatalf("roundtrip failed: %+v ok=%v", got, ok)
	}
	// Tampered signature rejected.
	if _, ok := parseOIDCState(token+"x", secret); ok {
		t.Error("tampered token accepted")
	}
	// Wrong secret rejected.
	if _, ok := parseOIDCState(token, []byte("other")); ok {
		t.Error("token verified with wrong secret")
	}
}

func TestOIDCStateExpired(t *testing.T) {
	secret := []byte("s3cr3t")
	token, _ := signOIDCState(oidcState{State: "a", Expires: time.Now().Add(-time.Minute).Unix()}, secret)
	if _, ok := parseOIDCState(token, secret); ok {
		t.Error("expired state accepted")
	}
}

// ── claim extraction ────────────────────────────────────────────────────────

func TestExtractClaims(t *testing.T) {
	cfg := testOIDCCfg()
	raw := map[string]any{
		"preferred_username": "alice",
		"email":              "alice@example.com",
		"email_verified":     true,
		"groups":             []any{"users", "montly-admins"},
	}
	c := cfg.extractClaims("https://idp.example", "sub-1", raw)
	if c.Username != "alice" || c.Email != "alice@example.com" || !c.EmailVerified {
		t.Fatalf("bad claims: %+v", c)
	}
	if !containsString(c.Groups, "montly-admins") {
		t.Error("groups not parsed")
	}
	// Falls back to email when username claim missing.
	c2 := cfg.extractClaims("i", "s", map[string]any{"email": "bob@example.com"})
	if c2.Username != "bob@example.com" {
		t.Errorf("username fallback = %q", c2.Username)
	}
}

// ── resolveOIDCUser ─────────────────────────────────────────────────────────

func TestResolveOIDCUser_JITFirstUserIsAdmin(t *testing.T) {
	ah, db := newOIDCHandler(t, testOIDCCfg(), nil)
	u, err := ah.resolveOIDCUser(&OIDCClaims{Issuer: "https://idp.example", Subject: "s1", Username: "alice", Email: "alice@x", EmailVerified: true})
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin {
		t.Error("first user should be admin")
	}
	if u.Username != "alice" {
		t.Errorf("username = %q", u.Username)
	}
	// Subsequent lookup returns the same account (no duplicate).
	got, found, _ := db.GetUserByOIDC("https://idp.example", "s1")
	if !found || got.ID != u.ID {
		t.Error("account not linked")
	}
}

func TestResolveOIDCUser_JITSecondUserNotAdmin(t *testing.T) {
	ah, db := newOIDCHandler(t, testOIDCCfg(), nil)
	if _, err := db.CreateUser("existing", "hash", true); err != nil {
		t.Fatal(err)
	}
	u, err := ah.resolveOIDCUser(&OIDCClaims{Issuer: "https://idp.example", Subject: "s2", Username: "newbie", Email: "n@x"})
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAdmin {
		t.Error("second user should not be admin without group")
	}
}

func TestResolveOIDCUser_AlreadyLinked(t *testing.T) {
	ah, db := newOIDCHandler(t, testOIDCCfg(), nil)
	created, _ := db.CreateOIDCUser("alice", "alice@x", "https://idp.example", "s1", false)
	n0, _ := db.CountUsers()
	u, err := ah.resolveOIDCUser(&OIDCClaims{Issuer: "https://idp.example", Subject: "s1", Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != created.ID {
		t.Error("should return existing linked user")
	}
	if n1, _ := db.CountUsers(); n1 != n0 {
		t.Error("should not create a new user")
	}
}

func TestResolveOIDCUser_LinkByVerifiedEmail(t *testing.T) {
	ah, db := newOIDCHandler(t, testOIDCCfg(), nil)
	carol, _ := db.CreateUser("carol", "hash", false)
	setUserEmail(t, db, carol.ID, "carol@example.com")

	u, err := ah.resolveOIDCUser(&OIDCClaims{
		Issuer: "https://idp.example", Subject: "s-carol",
		Username: "carol-sso", Email: "carol@example.com", EmailVerified: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != carol.ID {
		t.Errorf("should link to carol (id %d), got %d", carol.ID, u.ID)
	}
	if linked, found, _ := db.GetUserByOIDC("https://idp.example", "s-carol"); !found || linked.ID != carol.ID {
		t.Error("carol not linked to subject")
	}
}

func TestResolveOIDCUser_UnverifiedEmailDoesNotLink(t *testing.T) {
	ah, db := newOIDCHandler(t, testOIDCCfg(), nil)
	carol, _ := db.CreateUser("carol", "hash", false)
	setUserEmail(t, db, carol.ID, "carol@example.com")

	u, err := ah.resolveOIDCUser(&OIDCClaims{
		Issuer: "https://idp.example", Subject: "s-x",
		Username: "carol-sso", Email: "carol@example.com", EmailVerified: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == carol.ID {
		t.Error("should NOT link on unverified email")
	}
}

func TestResolveOIDCUser_LinkByUsername(t *testing.T) {
	ah, db := newOIDCHandler(t, testOIDCCfg(), nil)
	dave, _ := db.CreateUser("dave", "hash", false)
	u, err := ah.resolveOIDCUser(&OIDCClaims{Issuer: "https://idp.example", Subject: "s-dave", Username: "dave"})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != dave.ID {
		t.Error("should link by username")
	}
}

func TestResolveOIDCUser_UsernameDedupeWhenLinkingOff(t *testing.T) {
	cfg := testOIDCCfg()
	cfg.LinkByUsername = false
	cfg.LinkByEmail = false
	ah, db := newOIDCHandler(t, cfg, nil)
	if _, err := db.CreateUser("erin", "hash", false); err != nil {
		t.Fatal(err)
	}
	u, err := ah.resolveOIDCUser(&OIDCClaims{Issuer: "https://idp.example", Subject: "s-erin", Username: "erin"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "erin-2" {
		t.Errorf("expected deduped username erin-2, got %q", u.Username)
	}
}

func TestResolveOIDCUser_SignupDisabled(t *testing.T) {
	cfg := testOIDCCfg()
	cfg.AllowSignup = false
	cfg.LinkByEmail = false
	cfg.LinkByUsername = false
	ah, _ := newOIDCHandler(t, cfg, nil)
	_, err := ah.resolveOIDCUser(&OIDCClaims{Issuer: "https://idp.example", Subject: "s-none", Username: "ghost"})
	if err == nil || err.Error() != "signup_disabled" {
		t.Fatalf("expected signup_disabled, got %v", err)
	}
}

func TestResolveOIDCUser_AdminGroupSync(t *testing.T) {
	cfg := testOIDCCfg()
	cfg.AdminGroup = "montly-admins"
	ah, db := newOIDCHandler(t, cfg, nil)
	// Pre-existing non-admin, already linked.
	linked, _ := db.CreateOIDCUser("frank", "frank@x", "https://idp.example", "s-frank", false)

	// Login with the admin group → promoted.
	u, err := ah.resolveOIDCUser(&OIDCClaims{Issuer: "https://idp.example", Subject: "s-frank", Username: "frank", Groups: []string{"montly-admins"}})
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin {
		t.Error("should be promoted to admin")
	}
	if got, _, _ := db.GetUserByOIDC("https://idp.example", "s-frank"); !got.IsAdmin {
		t.Error("admin flag not persisted")
	}

	// Login again without the group → demoted.
	u2, _ := ah.resolveOIDCUser(&OIDCClaims{Issuer: "https://idp.example", Subject: "s-frank", Username: "frank", Groups: []string{"users"}})
	if u2.IsAdmin {
		t.Error("should be demoted when no longer in admin group")
	}
	_ = linked
}

// ── handlers ────────────────────────────────────────────────────────────────

func TestAuthConfigHandler(t *testing.T) {
	ah, _ := newOIDCHandler(t, testOIDCCfg(), &fakeOIDCProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
	w := httptest.NewRecorder()
	ah.AuthConfig(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"provider_name":"Test IdP"`) {
		t.Errorf("unexpected config body: %s", body)
	}
	if !strings.Contains(body, `"password_login":true`) {
		t.Errorf("password_login should be true: %s", body)
	}
}

func TestAuthConfigHandler_OIDCDisabled(t *testing.T) {
	ah, _ := newOIDCHandler(t, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
	w := httptest.NewRecorder()
	ah.AuthConfig(w, req)
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Errorf("oidc should be disabled: %s", w.Body.String())
	}
}

func TestOIDCLoginRedirects(t *testing.T) {
	ah, _ := newOIDCHandler(t, testOIDCCfg(), &fakeOIDCProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil)
	w := httptest.NewRecorder()
	ah.OIDCLogin(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "idp.example/authorize") {
		t.Errorf("bad redirect: %s", loc)
	}
	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == oidcStateCookie && c.Value != "" {
			found = true
			if c.SameSite != http.SameSiteLaxMode {
				t.Error("state cookie should be SameSite=Lax")
			}
		}
	}
	if !found {
		t.Error("state cookie not set")
	}
}

func TestOIDCLoginNotConfigured(t *testing.T) {
	ah, _ := newOIDCHandler(t, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil)
	w := httptest.NewRecorder()
	ah.OIDCLogin(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOIDCCallbackSuccess(t *testing.T) {
	provider := &fakeOIDCProvider{claims: &OIDCClaims{
		Issuer: "https://idp.example", Subject: "s1", Username: "alice", Email: "a@x", EmailVerified: true,
	}}
	ah, db := newOIDCHandler(t, testOIDCCfg(), provider)

	signed, _ := signOIDCState(oidcState{State: "st", Nonce: "nn", Verifier: "vv", Expires: time.Now().Add(time.Minute).Unix()}, ah.secret)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=st&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: signed})
	w := httptest.NewRecorder()
	ah.OIDCCallback(w, req)

	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("status=%d loc=%s", w.Code, w.Header().Get("Location"))
	}
	var hasSession bool
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			hasSession = true
		}
	}
	if !hasSession {
		t.Error("session cookie not set")
	}
	if _, found, _ := db.GetUserByOIDC("https://idp.example", "s1"); !found {
		t.Error("user not provisioned")
	}
}

func TestOIDCCallbackStateMismatch(t *testing.T) {
	ah, _ := newOIDCHandler(t, testOIDCCfg(), &fakeOIDCProvider{})
	signed, _ := signOIDCState(oidcState{State: "real", Expires: time.Now().Add(time.Minute).Unix()}, ah.secret)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=forged&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: signed})
	w := httptest.NewRecorder()
	ah.OIDCCallback(w, req)
	if !strings.Contains(w.Header().Get("Location"), "auth_error=state_mismatch") {
		t.Errorf("expected state_mismatch, got %s", w.Header().Get("Location"))
	}
}

func TestOIDCCallbackMissingStateCookie(t *testing.T) {
	ah, _ := newOIDCHandler(t, testOIDCCfg(), &fakeOIDCProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=x&code=abc", nil)
	w := httptest.NewRecorder()
	ah.OIDCCallback(w, req)
	if !strings.Contains(w.Header().Get("Location"), "auth_error=missing_state") {
		t.Errorf("got %s", w.Header().Get("Location"))
	}
}

func TestOIDCCallbackExchangeError(t *testing.T) {
	ah, _ := newOIDCHandler(t, testOIDCCfg(), &fakeOIDCProvider{err: context.DeadlineExceeded})
	signed, _ := signOIDCState(oidcState{State: "st", Nonce: "nn", Verifier: "vv", Expires: time.Now().Add(time.Minute).Unix()}, ah.secret)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state=st&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: signed})
	w := httptest.NewRecorder()
	ah.OIDCCallback(w, req)
	if !strings.Contains(w.Header().Get("Location"), "auth_error=exchange_failed") {
		t.Errorf("got %s", w.Header().Get("Location"))
	}
}

func TestLoginRejectedWhenPasswordDisabled(t *testing.T) {
	ah, _ := newOIDCHandler(t, testOIDCCfg(), &fakeOIDCProvider{})
	ah.passwordDisabled = true
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"a","password":"b"}`))
	w := httptest.NewRecorder()
	ah.Login(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}
