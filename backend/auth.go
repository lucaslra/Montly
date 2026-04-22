package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

// ---------- helpers ----------

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---------- session tokens ----------

type ctxKey string

const ctxUserKey ctxKey = "user"

type sessionClaims struct {
	UserID  int64 `json:"uid"`
	IsAdmin bool  `json:"adm"`
	Expires int64 `json:"exp"`
}

const (
	sessionCookieName = "_montly"
	sessionDuration   = 24 * time.Hour
)

// dummyHash is used to perform a constant-time bcrypt comparison when a
// username is not found, preventing user-enumeration via response timing.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("_dummy_"), bcrypt.DefaultCost)

func newSession(claims sessionClaims, secret []byte) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return enc + "." + sig, nil
}

func parseSession(token string, secret []byte) (sessionClaims, bool) {
	i := strings.LastIndex(token, ".")
	if i < 0 {
		return sessionClaims{}, false
	}
	enc, sig := token[:i], token[i+1:]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(enc))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return sessionClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return sessionClaims{}, false
	}
	var claims sessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return sessionClaims{}, false
	}
	if time.Now().Unix() > claims.Expires {
		return sessionClaims{}, false
	}
	return claims, true
}

func setSession(w http.ResponseWriter, claims sessionClaims, secret []byte, secure bool) error {
	token, err := newSession(claims, secret)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
	return nil
}

func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// ---------- middleware ----------

// requireAuth validates the session cookie or Bearer token and injects claims
// into the request context.
func requireAuth(secret []byte, db *DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Bearer token (for API clients / mobile).
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				tokenStr := strings.TrimPrefix(auth, "Bearer ")
				hash := sha256hex(tokenStr)
				tok, err := db.GetTokenByHash(hash)
				if err != nil {
					writeError(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				user, err := db.GetUserByID(tok.UserID)
				if err != nil {
					writeError(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				go db.UpdateTokenLastUsed(tok.ID)
				claims := sessionClaims{
					UserID:  user.ID,
					IsAdmin: user.IsAdmin,
					Expires: time.Now().Add(24 * time.Hour).Unix(),
				}
				ctx := context.WithValue(r.Context(), ctxUserKey, claims)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 2. Session cookie (for browser clients).
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil {
				writeError(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			claims, ok := parseSession(cookie.Value, secret)
			if !ok {
				clearSession(w)
				writeError(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ctxUserKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireAdmin rejects non-admin users with 403. Must be used after requireAuth.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !currentUser(r).IsAdmin {
			writeError(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// currentUser extracts the session claims injected by requireAuth.
func currentUser(r *http.Request) sessionClaims {
	claims, _ := r.Context().Value(ctxUserKey).(sessionClaims)
	return claims
}

// ---------- auth handlers ----------

type AuthHandler struct {
	db          *DB
	secret      []byte
	secure      bool
	trustProxy  bool
	rl          *RateLimiter
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, h.trustProxy)
	if !h.rl.allow(ip) {
		writeError(w, "too many failed attempts — try again later", http.StatusTooManyRequests)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, "username and password required", http.StatusBadRequest)
		return
	}

	user, err := h.db.GetUserByUsername(req.Username)
	if err != nil {
		// Constant-time: perform bcrypt even on miss to prevent timing attacks.
		bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		h.rl.recordFailure(ip)
		writeError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		h.rl.recordFailure(ip)
		writeError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	h.rl.recordSuccess(ip)
	claims := sessionClaims{
		UserID:  user.ID,
		IsAdmin: user.IsAdmin,
		Expires: time.Now().Add(sessionDuration).Unix(),
	}
	if err := setSession(w, claims, h.secret, h.secure); err != nil {
		writeServerError(w, "failed to create session", err)
		return
	}
	writeJSON(w, map[string]any{"id": user.ID, "username": user.Username, "is_admin": user.IsAdmin})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := currentUser(r)
	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil {
		clearSession(w)
		writeError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"id": user.ID, "username": user.Username, "is_admin": user.IsAdmin})
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, "new password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	user, err := h.db.GetUserByID(currentUser(r).UserID)
	if err != nil {
		writeServerError(w, "user not found", err)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		writeError(w, "current password is incorrect", http.StatusBadRequest)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeServerError(w, "failed to hash password", err)
		return
	}
	if err := h.db.UpdateUserPassword(user.ID, string(newHash)); err != nil {
		writeServerError(w, "failed to update password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- user management handlers (admin only) ----------

type UserHandler struct {
	db *DB
}

func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.db.ListUsers()
	if err != nil {
		writeServerError(w, "failed to list users", err)
		return
	}
	writeJSON(w, users)
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Username == "" {
		writeError(w, "username is required", http.StatusBadRequest)
		return
	}
	if len(req.Username) > 64 {
		writeError(w, "username must be 64 characters or fewer", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		writeError(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeServerError(w, "failed to hash password", err)
		return
	}
	user, err := h.db.CreateUser(req.Username, string(hash), req.IsAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			writeError(w, "username already exists", http.StatusConflict)
			return
		}
		writeServerError(w, "failed to create user", err)
		return
	}
	writeJSONCreated(w, map[string]any{"id": user.ID, "username": user.Username, "is_admin": user.IsAdmin, "created_at": user.CreatedAt})
}

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if id == currentUser(r).UserID {
		writeError(w, "cannot delete your own account", http.StatusBadRequest)
		return
	}
	// Ensure at least one admin remains.
	target, err := h.db.GetUserByID(id)
	if err == sql.ErrNoRows {
		writeError(w, "user not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeServerError(w, "failed to get user", err)
		return
	}
	if target.IsAdmin {
		n, err := h.db.CountAdmins()
		if err != nil {
			writeServerError(w, "failed to check admins", err)
			return
		}
		if n <= 1 {
			writeError(w, "cannot delete the last admin account", http.StatusBadRequest)
			return
		}
	}
	if err := h.db.DeleteUser(id); err != nil {
		writeServerError(w, "failed to delete user", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- token handlers ----------

type TokenHandler struct {
	db *DB
}

func (h *TokenHandler) ListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.db.ListTokens(currentUser(r).UserID)
	if err != nil {
		writeServerError(w, "failed to list tokens", err)
		return
	}
	if tokens == nil {
		tokens = []APIToken{}
	}
	writeJSON(w, tokens)
}

func (h *TokenHandler) CreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 100 {
		writeError(w, "token name must be 100 characters or fewer", http.StatusBadRequest)
		return
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		writeServerError(w, "failed to generate token", err)
		return
	}
	plaintext := "mt_" + base64.RawURLEncoding.EncodeToString(b)
	hash := sha256hex(plaintext)

	tok, err := h.db.CreateToken(currentUser(r).UserID, req.Name, hash)
	if err != nil {
		writeServerError(w, "failed to create token", err)
		return
	}

	writeJSONCreated(w, map[string]any{
		"token":     tok,
		"plaintext": plaintext,
	})
}

func (h *TokenHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.db.RevokeToken(id, currentUser(r).UserID); err == sql.ErrNoRows {
		writeError(w, "token not found", http.StatusNotFound)
		return
	} else if err != nil {
		writeServerError(w, "failed to revoke token", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
