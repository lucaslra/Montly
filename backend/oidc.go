package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ── Config ──────────────────────────────────────────────────────────────────

// OIDCConfig holds the resolved OpenID Connect configuration. It is nil when
// OIDC_ISSUER is unset (SSO disabled).
type OIDCConfig struct {
	Issuer         string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	Scopes         []string
	ProviderName   string
	UsernameClaim  string
	GroupsClaim    string
	AdminGroup     string
	AllowSignup    bool
	LinkByEmail    bool
	LinkByUsername bool
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return def
	}
}

func parseScopes(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' })
	seen := map[string]bool{}
	scopes := []string{}
	for _, f := range append([]string{oidc.ScopeOpenID}, fields...) {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		scopes = append(scopes, f)
	}
	return scopes
}

// loadOIDCConfig reads OIDC settings from the environment. Returns nil when
// OIDC_ISSUER is unset.
func loadOIDCConfig() *OIDCConfig {
	issuer := strings.TrimSpace(os.Getenv("OIDC_ISSUER"))
	if issuer == "" {
		return nil
	}
	scopesEnv := os.Getenv("OIDC_SCOPES")
	if scopesEnv == "" {
		scopesEnv = "profile email"
	}
	return &OIDCConfig{
		Issuer:         strings.TrimRight(issuer, "/"),
		ClientID:       strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		ClientSecret:   os.Getenv("OIDC_CLIENT_SECRET"),
		RedirectURL:    strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL")),
		Scopes:         parseScopes(scopesEnv),
		ProviderName:   envOr("OIDC_PROVIDER_NAME", "SSO"),
		UsernameClaim:  envOr("OIDC_USERNAME_CLAIM", "preferred_username"),
		GroupsClaim:    envOr("OIDC_GROUPS_CLAIM", "groups"),
		AdminGroup:     strings.TrimSpace(os.Getenv("OIDC_ADMIN_GROUP")),
		AllowSignup:    envBool("OIDC_ALLOW_SIGNUP", true),
		LinkByEmail:    envBool("OIDC_LINK_BY_EMAIL", true),
		LinkByUsername: envBool("OIDC_LINK_BY_USERNAME", true),
	}
}

// Validate reports configuration errors that must abort startup.
func (c *OIDCConfig) Validate() error {
	if c.ClientID == "" {
		return errors.New("OIDC_CLIENT_ID is required when OIDC_ISSUER is set")
	}
	if c.ClientSecret == "" {
		return errors.New("OIDC_CLIENT_SECRET is required when OIDC_ISSUER is set")
	}
	if c.RedirectURL == "" {
		return errors.New("OIDC_REDIRECT_URL is required when OIDC_ISSUER is set")
	}
	u, err := url.ParseRequestURI(c.RedirectURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("OIDC_REDIRECT_URL must be an absolute http(s) URL")
	}
	return nil
}

// ── Provider abstraction (testable) ─────────────────────────────────────────

// OIDCClaims are the normalized identity claims extracted from a verified ID token.
type OIDCClaims struct {
	Issuer        string
	Subject       string
	Username      string
	Email         string
	EmailVerified bool
	Groups        []string
}

// OIDCProvider abstracts the authorization-code flow so handlers can be unit
// tested against a fake without a live IdP.
type OIDCProvider interface {
	AuthCodeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, nonce, verifier string) (*OIDCClaims, error)
}

type realOIDCProvider struct {
	cfg      OIDCConfig
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func newRealOIDCProvider(ctx context.Context, cfg OIDCConfig) (OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %q: %w", cfg.Issuer, err)
	}
	return &realOIDCProvider{
		cfg: cfg,
		oauth2: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

func (p *realOIDCProvider) AuthCodeURL(state, nonce, verifier string) string {
	return p.oauth2.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
}

func (p *realOIDCProvider) Exchange(ctx context.Context, code, nonce, verifier string) (*OIDCClaims, error) {
	tok, err := p.oauth2.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, errors.New("no id_token in token response")
	}
	idToken, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	if idToken.Nonce != nonce {
		return nil, errors.New("nonce mismatch")
	}
	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	return p.cfg.extractClaims(idToken.Issuer, idToken.Subject, raw), nil
}

func (c OIDCConfig) extractClaims(issuer, subject string, raw map[string]any) *OIDCClaims {
	claims := &OIDCClaims{Issuer: issuer, Subject: subject}
	if v, ok := raw[c.UsernameClaim].(string); ok {
		claims.Username = strings.TrimSpace(v)
	}
	if claims.Username == "" {
		if v, ok := raw["preferred_username"].(string); ok {
			claims.Username = strings.TrimSpace(v)
		}
	}
	if v, ok := raw["email"].(string); ok {
		claims.Email = strings.TrimSpace(v)
	}
	if claims.Username == "" {
		claims.Username = claims.Email
	}
	if v, ok := raw["email_verified"].(bool); ok {
		claims.EmailVerified = v
	}
	claims.Groups = toStringSlice(raw[c.GroupsClaim])
	return claims
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	default:
		return nil
	}
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// ── State cookie (HMAC-signed, holds CSRF state + nonce + PKCE verifier) ─────

const (
	oidcStateCookie = "_montly_oidc"
	oidcStateTTL    = 10 * time.Minute
)

type oidcState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Expires  int64  `json:"e"`
}

func signOIDCState(st oidcState, secret []byte) (string, error) {
	payload, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	return enc + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func parseOIDCState(token string, secret []byte) (oidcState, bool) {
	i := strings.LastIndex(token, ".")
	if i < 0 {
		return oidcState{}, false
	}
	enc, sig := token[:i], token[i+1:]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return oidcState{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return oidcState{}, false
	}
	var st oidcState
	if err := json.Unmarshal(payload, &st); err != nil {
		return oidcState{}, false
	}
	if time.Now().Unix() > st.Expires {
		return oidcState{}, false
	}
	return st, true
}

func randToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is fatal-worthy; caller treats "" as an error path.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ── Controlled auth-error reasons (surfaced on the login page) ──────────────

type authReasonError struct{ reason string }

func (e authReasonError) Error() string { return e.reason }

func errAuthReason(reason string) error { return authReasonError{reason} }

// ── Handlers (methods on AuthHandler; struct is defined in auth.go) ──────────

// authConfigResponse tells the frontend which sign-in methods are available.
type authConfigResponse struct {
	PasswordLogin bool           `json:"password_login"`
	OIDC          oidcConfigView `json:"oidc"`
}

type oidcConfigView struct {
	Enabled      bool   `json:"enabled"`
	ProviderName string `json:"provider_name,omitempty"`
}

// AuthConfig is a public endpoint describing available login methods.
func (h *AuthHandler) AuthConfig(w http.ResponseWriter, r *http.Request) {
	resp := authConfigResponse{PasswordLogin: !h.passwordDisabled}
	if h.oidc != nil && h.oidcCfg != nil {
		resp.OIDC = oidcConfigView{Enabled: true, ProviderName: h.oidcCfg.ProviderName}
	}
	writeJSON(w, resp)
}

// OIDCLogin starts the authorization-code flow: it mints CSRF state, a nonce,
// and a PKCE verifier, stores them in a short-lived signed cookie, and redirects
// the browser to the identity provider.
func (h *AuthHandler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		writeError(w, "sso is not configured", http.StatusNotFound)
		return
	}
	state, nonce, verifier := randToken(), randToken(), oauth2.GenerateVerifier()
	if state == "" || nonce == "" {
		writeServerError(w, "failed to start sso", errors.New("rand failure"))
		return
	}
	signed, err := signOIDCState(oidcState{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		Expires:  time.Now().Add(oidcStateTTL).Unix(),
	}, h.secret)
	if err != nil {
		writeServerError(w, "failed to start sso", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteLaxMode, // Lax: cookie must survive the top-level redirect back from the IdP
		MaxAge:   int(oidcStateTTL.Seconds()),
	})
	http.Redirect(w, r, h.oidc.AuthCodeURL(state, nonce, verifier), http.StatusFound)
}

// OIDCCallback completes the flow: verifies state, exchanges the code, verifies
// the ID token, resolves (links or provisions) the user, and opens a session.
func (h *AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if h.oidc == nil {
		writeError(w, "sso is not configured", http.StatusNotFound)
		return
	}
	// Always clear the state cookie once we've read it.
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})

	if e := r.URL.Query().Get("error"); e != "" {
		h.redirectAuthError(w, r, "provider_error")
		return
	}
	cookie, err := r.Cookie(oidcStateCookie)
	if err != nil {
		h.redirectAuthError(w, r, "missing_state")
		return
	}
	st, ok := parseOIDCState(cookie.Value, h.secret)
	if !ok {
		h.redirectAuthError(w, r, "invalid_state")
		return
	}
	if st.State == "" || r.URL.Query().Get("state") != st.State {
		h.redirectAuthError(w, r, "state_mismatch")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		h.redirectAuthError(w, r, "missing_code")
		return
	}

	claims, err := h.oidc.Exchange(r.Context(), code, st.Nonce, st.Verifier)
	if err != nil {
		log.Printf("oidc: exchange failed: %v", err)
		h.redirectAuthError(w, r, "exchange_failed")
		return
	}
	if claims.Subject == "" {
		h.redirectAuthError(w, r, "missing_subject")
		return
	}

	user, err := h.resolveOIDCUser(claims)
	if err != nil {
		var reason authReasonError
		if errors.As(err, &reason) {
			h.redirectAuthError(w, r, reason.reason)
			return
		}
		log.Printf("oidc: resolve user: %v", err)
		h.redirectAuthError(w, r, "server_error")
		return
	}

	sess := sessionClaims{
		UserID:  user.ID,
		IsAdmin: user.IsAdmin,
		Expires: time.Now().Add(sessionDuration).Unix(),
	}
	if err := setSession(w, sess, h.secret, h.secure); err != nil {
		writeServerError(w, "failed to create session", err)
		return
	}
	go h.db.InsertAuditLog(user.ID, "login_oidc", "user", user.ID, user.Username)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *AuthHandler) redirectAuthError(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/?auth_error="+url.QueryEscape(reason), http.StatusFound)
}

// resolveOIDCUser maps verified claims to a Montly user, in priority order:
//  1. an account already linked to this (issuer, subject)
//  2. link to an existing local account by verified email, then by username
//  3. just-in-time provisioning of a new SSO-only account
//
// Admin status is synced from the IdP group when OIDC_ADMIN_GROUP is set; the
// first-ever user is always bootstrapped as admin.
func (h *AuthHandler) resolveOIDCUser(c *OIDCClaims) (User, error) {
	cfg := h.oidcCfg
	inAdminGroup := cfg.AdminGroup != "" && containsString(c.Groups, cfg.AdminGroup)

	// 1. Already linked.
	if u, found, err := h.db.GetUserByOIDC(c.Issuer, c.Subject); err != nil {
		return User{}, err
	} else if found {
		h.applyAdminSync(&u, inAdminGroup)
		return u, nil
	}

	// 2a. Link by verified email.
	if cfg.LinkByEmail && c.Email != "" && c.EmailVerified {
		if u, found, err := h.db.GetUserByEmail(c.Email); err != nil {
			return User{}, err
		} else if found && u.OIDCSubject == "" {
			if err := h.db.LinkOIDCIdentity(u.ID, c.Issuer, c.Subject, c.Email); err != nil {
				return User{}, err
			}
			h.applyAdminSync(&u, inAdminGroup)
			return u, nil
		}
	}

	// 2b. Link by username.
	if cfg.LinkByUsername && c.Username != "" {
		if u, found, err := h.db.GetUserByUsernameFull(c.Username); err != nil {
			return User{}, err
		} else if found && u.OIDCSubject == "" {
			if err := h.db.LinkOIDCIdentity(u.ID, c.Issuer, c.Subject, c.Email); err != nil {
				return User{}, err
			}
			h.applyAdminSync(&u, inAdminGroup)
			return u, nil
		}
	}

	// 3. Just-in-time provisioning.
	if !cfg.AllowSignup {
		return User{}, errAuthReason("signup_disabled")
	}
	n, err := h.db.CountUsers()
	if err != nil {
		return User{}, err
	}
	username, err := h.uniqueUsername(c.Username)
	if err != nil {
		return User{}, err
	}
	return h.db.CreateOIDCUser(username, c.Email, c.Issuer, c.Subject, inAdminGroup || n == 0)
}

// applyAdminSync updates the user's admin flag from group membership, but only
// when an admin group is configured. Without OIDC_ADMIN_GROUP the flag is left
// untouched (so a manually promoted admin stays admin).
func (h *AuthHandler) applyAdminSync(u *User, inAdminGroup bool) {
	if h.oidcCfg.AdminGroup == "" || u.IsAdmin == inAdminGroup {
		return
	}
	if err := h.db.SetUserAdmin(u.ID, inAdminGroup); err != nil {
		log.Printf("oidc: admin sync for user %d: %v", u.ID, err)
		return
	}
	u.IsAdmin = inAdminGroup
}

func sanitizeUsername(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// uniqueUsername derives a username that does not collide with an existing one,
// appending a numeric suffix if needed (never links to the existing account).
func (h *AuthHandler) uniqueUsername(base string) (string, error) {
	base = sanitizeUsername(base)
	if base == "" {
		base = "user"
	}
	candidate := base
	for i := 2; i < 10000; i++ {
		_, found, err := h.db.GetUserByUsernameFull(candidate)
		if err != nil {
			return "", err
		}
		if !found {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", errors.New("could not derive a unique username")
}
