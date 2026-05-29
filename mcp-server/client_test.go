package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientGet_SendsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := &montlyClient{baseURL: srv.URL, token: "mt_test123", http: srv.Client()}
	_, err := c.get("/tasks")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer mt_test123" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer mt_test123")
	}
}

func TestClientGet_BuildsCorrectURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &montlyClient{baseURL: srv.URL, token: "mt_x", http: srv.Client()}
	c.get("/tasks?month=2026-05")

	if gotPath != "/api/tasks?month=2026-05" {
		t.Errorf("path = %q, want %q", gotPath, "/api/tasks?month=2026-05")
	}
}

func TestClientGet_ReturnsBodyOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := &montlyClient{baseURL: srv.URL, token: "mt_x", http: srv.Client()}
	body, err := c.get("/test")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q", body)
	}
}

func TestClientGet_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`not found`))
	}))
	defer srv.Close()

	c := &montlyClient{baseURL: srv.URL, token: "mt_x", http: srv.Client()}
	_, err := c.get("/missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if got := err.Error(); got != "montly API 404: not found" {
		t.Errorf("error = %q", got)
	}
}

func TestClientGet_ReturnsErrorOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal error`))
	}))
	defer srv.Close()

	c := &montlyClient{baseURL: srv.URL, token: "mt_x", http: srv.Client()}
	_, err := c.get("/boom")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestNewMontlyClient_TrimsTrailingSlash(t *testing.T) {
	t.Setenv("MONTLY_URL", "http://example.com/")
	t.Setenv("MONTLY_TOKEN", "mt_test")
	c := newMontlyClient()
	if c.baseURL != "http://example.com" {
		t.Errorf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

func TestNewMontlyClient_DefaultsURL(t *testing.T) {
	t.Setenv("MONTLY_URL", "")
	t.Setenv("MONTLY_TOKEN", "mt_test")
	c := newMontlyClient()
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want default", c.baseURL)
	}
}
