package main

import (
	"testing"
)

func TestNewServer_DoesNotPanic(t *testing.T) {
	client := &montlyClient{baseURL: "http://localhost:8080", token: "mt_test"}
	server := newServer(client)
	if server == nil {
		t.Fatal("newServer returned nil")
	}
}
