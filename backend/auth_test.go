package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var testSecret = []byte("test-secret-key")

// ---------- session ----------

func TestSessionRoundTrip(t *testing.T) {
	claims := sessionClaims{
		UserID:  42,
		IsAdmin: true,
		Expires: time.Now().Add(1 * time.Hour).Unix(),
	}
	tok, err := newSession(claims, testSecret)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	got, ok := parseSession(tok, testSecret)
	if !ok {
		t.Fatal("parseSession: returned false")
	}
	if got.UserID != claims.UserID {
		t.Errorf("UserID: got %d, want %d", got.UserID, claims.UserID)
	}
	if got.IsAdmin != claims.IsAdmin {
		t.Errorf("IsAdmin: got %v, want %v", got.IsAdmin, claims.IsAdmin)
	}
}

func TestSessionExpiry(t *testing.T) {
	claims := sessionClaims{
		UserID:  1,
		IsAdmin: false,
		Expires: time.Now().Add(-1 * time.Second).Unix(), // already expired
	}
	tok, err := newSession(claims, testSecret)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	_, ok := parseSession(tok, testSecret)
	if ok {
		t.Error("expected expired session to be rejected")
	}
}

func TestSessionTampering(t *testing.T) {
	claims := sessionClaims{
		UserID:  1,
		IsAdmin: false,
		Expires: time.Now().Add(1 * time.Hour).Unix(),
	}
	tok, err := newSession(claims, testSecret)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	// Flip the last byte of the token.
	tampered := []byte(tok)
	tampered[len(tampered)-1] ^= 0xFF
	_, ok := parseSession(string(tampered), testSecret)
	if ok {
		t.Error("expected tampered session to be rejected")
	}
}

func TestSessionWrongSecret(t *testing.T) {
	claims := sessionClaims{
		UserID:  1,
		Expires: time.Now().Add(1 * time.Hour).Unix(),
	}
	tok, err := newSession(claims, testSecret)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	_, ok := parseSession(tok, []byte("other-secret"))
	if ok {
		t.Error("expected session verified with wrong secret to be rejected")
	}
}

// ---------- rate limiter ----------

func TestRateLimiterBlocks(t *testing.T) {
	rl := &RateLimiter{entries: make(map[string]*ipState)}
	ip := "10.0.0.1"

	for i := 0; i < rlMaxFailures; i++ {
		if !rl.allow(ip) {
			t.Fatalf("should be allowed before %d failures", rlMaxFailures)
		}
		rl.recordFailure(ip)
	}

	// Now it should be blocked.
	if rl.allow(ip) {
		t.Error("expected IP to be blocked after max failures")
	}
}

func TestRateLimiterClearsOnSuccess(t *testing.T) {
	rl := &RateLimiter{entries: make(map[string]*ipState)}
	ip := "10.0.0.2"

	for i := 0; i < rlMaxFailures; i++ {
		rl.recordFailure(ip)
	}
	if rl.allow(ip) {
		t.Fatal("should be blocked before success")
	}

	rl.recordSuccess(ip)
	if !rl.allow(ip) {
		t.Error("expected IP to be unblocked after success")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	rl := &RateLimiter{entries: make(map[string]*ipState)}
	ip := "10.0.0.3"

	// Manually insert an expired entry with max failures.
	rl.entries[ip] = &ipState{
		failures:  rlMaxFailures,
		windowEnd: time.Now().Add(-1 * time.Second),
	}

	// Expired window → should be allowed again.
	if !rl.allow(ip) {
		t.Error("expected expired window to reset block")
	}
}

// ---------- clientIP ----------

func TestClientIPForwardedFor(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"single", "1.2.3.4", "1.2.3.4"},
		{"chain", "1.2.3.4, 10.0.0.1, 172.16.0.1", "1.2.3.4"},
		{"with spaces", " 1.2.3.4 , 10.0.0.1", "1.2.3.4"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Forwarded-For", tc.header)
			got := clientIP(req, true)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIPRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:54321"
	got := clientIP(req, false)
	if got != "192.168.1.1" {
		t.Errorf("got %q, want 192.168.1.1", got)
	}
}

// ---------- sha256hex ----------

func TestSHA256Hex(t *testing.T) {
	// Verify it produces the correct SHA-256 hex for a known input.
	// SHA-256("abc") = ba7816bf8f01cfea414140de5dae2ec73b00361bbef0469f492c...
	got := sha256hex("abc")
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Errorf("sha256hex(\"abc\") = %q, want %q", got, want)
	}
}

func TestSHA256HexDeterministic(t *testing.T) {
	a := sha256hex("hello")
	b := sha256hex("hello")
	if a != b {
		t.Error("sha256hex is not deterministic")
	}
	c := sha256hex("world")
	if a == c {
		t.Error("sha256hex should differ for different inputs")
	}
}
