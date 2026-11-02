package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewServer_DoesNotPanic(t *testing.T) {
	client := &montlyClient{baseURL: "http://localhost:8080", token: "mt_test"}
	server := newServer(client)
	if server == nil {
		t.Fatal("newServer returned nil")
	}
}

func TestNewServer_RegistersToolsWithAnnotations(t *testing.T) {
	ctx := context.Background()
	server := newServer(&montlyClient{baseURL: "http://localhost:8080", token: "mt_test"})

	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}

	readOnly := []string{"list_tasks", "get_report", "get_task", "list_archived_tasks", "export_completions", "get_settings"}
	writes := []string{"complete_task", "uncomplete_task", "skip_task", "update_completion", "create_task", "update_task", "archive_task", "delete_task", "update_settings"}
	if len(tools) != len(readOnly)+len(writes) {
		t.Errorf("registered %d tools, want %d", len(tools), len(readOnly)+len(writes))
	}
	for _, name := range readOnly {
		tool, ok := tools[name]
		if !ok {
			t.Errorf("%s not registered", name)
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s should be read-only", name)
		}
	}
	for _, name := range writes {
		tool, ok := tools[name]
		if !ok {
			t.Errorf("%s not registered", name)
			continue
		}
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			t.Errorf("%s should not be read-only", name)
		}
	}

	// Writes that lose data are destructive; the rest say so explicitly so
	// clients do not fall back to the spec's destructive default.
	destructive := map[string]bool{
		"uncomplete_task": true, "delete_task": true,
		"complete_task": false, "skip_task": false, "update_completion": false, "create_task": false,
		"update_task": false, "archive_task": false, "update_settings": false,
	}
	for name, want := range destructive {
		tool, ok := tools[name]
		if !ok {
			continue // already reported above
		}
		if a := tool.Annotations; a == nil || a.DestructiveHint == nil || *a.DestructiveHint != want {
			t.Errorf("%s: destructiveHint should be %v", name, want)
		}
	}
}
