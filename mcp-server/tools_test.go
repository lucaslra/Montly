package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mockMontlyServer(tasks []task, completions []completion) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/tasks"):
			json.NewEncoder(w).Encode(tasks)
		case strings.HasPrefix(r.URL.Path, "/api/completions"):
			json.NewEncoder(w).Encode(completions)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func callListTasks(t *testing.T, client *montlyClient, month string) listTasksSummary {
	t.Helper()
	handler := listTasksHandler(client)
	result, _, err := handler(context.Background(), nil, listTasksInput{Month: month})
	if err != nil {
		t.Fatalf("listTasksHandler returned error: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("empty content in result")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var summary listTasksSummary
	if err := json.Unmarshal([]byte(text), &summary); err != nil {
		t.Fatalf("failed to decode summary: %v", err)
	}
	return summary
}

func TestListTasks_AllPending(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "Rent", Type: "payment", Amount: "1200", Interval: 1},
		{ID: 2, Title: "Netflix", Type: "subscription", Amount: "15.99", Interval: 1},
	}
	srv := mockMontlyServer(tasks, nil)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	if summary.Month != "2026-05" {
		t.Errorf("month = %q, want 2026-05", summary.Month)
	}
	if summary.Total != 2 {
		t.Errorf("total = %d, want 2", summary.Total)
	}
	if summary.Pending != 2 {
		t.Errorf("pending = %d, want 2", summary.Pending)
	}
	if summary.Completed != 0 {
		t.Errorf("completed = %d, want 0", summary.Completed)
	}
	for _, et := range summary.Tasks {
		if et.Status != "pending" {
			t.Errorf("task %d status = %q, want pending", et.ID, et.Status)
		}
	}
}

func TestListTasks_MixedStatuses(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "Rent", Type: "payment", Amount: "1200", Interval: 1},
		{ID: 2, Title: "Netflix", Type: "subscription", Amount: "15.99", Interval: 1},
		{ID: 3, Title: "Insurance", Type: "bill", Amount: "300", Interval: 1},
	}
	completions := []completion{
		{TaskID: 1, Month: "2026-05", CompletedAt: "2026-05-01T10:00:00Z", Amount: "1250"},
		{TaskID: 3, Month: "2026-05", Skipped: true},
	}
	srv := mockMontlyServer(tasks, completions)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	if summary.Completed != 1 {
		t.Errorf("completed = %d, want 1", summary.Completed)
	}
	if summary.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", summary.Skipped)
	}
	if summary.Pending != 1 {
		t.Errorf("pending = %d, want 1", summary.Pending)
	}

	statusMap := map[int64]string{}
	for _, et := range summary.Tasks {
		statusMap[et.ID] = et.Status
	}
	if statusMap[1] != "completed" {
		t.Errorf("task 1 status = %q, want completed", statusMap[1])
	}
	if statusMap[2] != "pending" {
		t.Errorf("task 2 status = %q, want pending", statusMap[2])
	}
	if statusMap[3] != "skipped" {
		t.Errorf("task 3 status = %q, want skipped", statusMap[3])
	}
}

func TestListTasks_PaidAmountOverride(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "Rent", Type: "payment", Amount: "1200", Interval: 1},
	}
	completions := []completion{
		{TaskID: 1, Month: "2026-05", Amount: "1250"},
	}
	srv := mockMontlyServer(tasks, completions)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	et := summary.Tasks[0]
	if et.Amount != "1200" {
		t.Errorf("amount = %q, want 1200 (task default)", et.Amount)
	}
	if et.Paid != "1250" {
		t.Errorf("paid = %q, want 1250 (completion override)", et.Paid)
	}
}

func TestListTasks_ReceiptDetection(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "With receipt", Type: "payment", Interval: 1},
		{ID: 2, Title: "No receipt", Type: "payment", Interval: 1},
	}
	completions := []completion{
		{TaskID: 1, Month: "2026-05", ReceiptFile: "abc-123.pdf"},
		{TaskID: 2, Month: "2026-05"},
	}
	srv := mockMontlyServer(tasks, completions)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	receiptMap := map[int64]bool{}
	for _, et := range summary.Tasks {
		receiptMap[et.ID] = et.HasReceipt
	}
	if !receiptMap[1] {
		t.Error("task 1 should have receipt")
	}
	if receiptMap[2] {
		t.Error("task 2 should not have receipt")
	}
}

func TestListTasks_SharedTask(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "Shared rent", Type: "payment", Interval: 1, IsShared: true, OwnerName: "alice"},
		{ID: 2, Title: "Own task", Type: "reminder", Interval: 1},
	}
	srv := mockMontlyServer(tasks, nil)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	sharedMap := map[int64]string{}
	for _, et := range summary.Tasks {
		sharedMap[et.ID] = et.SharedBy
	}
	if sharedMap[1] != "alice" {
		t.Errorf("task 1 shared_by = %q, want alice", sharedMap[1])
	}
	if sharedMap[2] != "" {
		t.Errorf("task 2 shared_by = %q, want empty", sharedMap[2])
	}
}

func TestListTasks_NotePreserved(t *testing.T) {
	tasks := []task{{ID: 1, Title: "Rent", Type: "payment", Interval: 1}}
	completions := []completion{{TaskID: 1, Month: "2026-05", Note: "paid via wire transfer"}}
	srv := mockMontlyServer(tasks, completions)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	if summary.Tasks[0].Note != "paid via wire transfer" {
		t.Errorf("note = %q", summary.Tasks[0].Note)
	}
}

func TestListTasks_EmptyList(t *testing.T) {
	srv := mockMontlyServer(nil, nil)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	if summary.Total != 0 {
		t.Errorf("total = %d, want 0", summary.Total)
	}
	if len(summary.Tasks) != 0 {
		t.Errorf("tasks len = %d, want 0", len(summary.Tasks))
	}
}

func TestListTasks_DefaultsToCurrentMonth(t *testing.T) {
	var gotMonth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotMonth == "" {
			gotMonth = r.URL.Query().Get("month")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := listTasksHandler(client)
	handler(context.Background(), nil, listTasksInput{})

	expected := time.Now().Format("2006-01")
	if gotMonth != expected {
		t.Errorf("month = %q, want %q (current)", gotMonth, expected)
	}
}

func TestListTasks_PassesMonthToAPI(t *testing.T) {
	var months []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		months = append(months, r.URL.Query().Get("month"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := listTasksHandler(client)
	handler(context.Background(), nil, listTasksInput{Month: "2025-12"})

	for _, m := range months {
		if m != "2025-12" {
			t.Errorf("API received month = %q, want 2025-12", m)
		}
	}
}

func TestListTasks_APIErrorOnTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/tasks") {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`server error`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := listTasksHandler(client)
	_, _, err := handler(context.Background(), nil, listTasksInput{Month: "2026-01"})
	if err == nil {
		t.Fatal("expected error when tasks API fails")
	}
	if !strings.Contains(err.Error(), "fetching tasks") {
		t.Errorf("error = %q, want to contain 'fetching tasks'", err.Error())
	}
}

func TestListTasks_APIErrorOnCompletions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/completions") {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`server error`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]task{})
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := listTasksHandler(client)
	_, _, err := handler(context.Background(), nil, listTasksInput{Month: "2026-01"})
	if err == nil {
		t.Fatal("expected error when completions API fails")
	}
	if !strings.Contains(err.Error(), "fetching completions") {
		t.Errorf("error = %q, want to contain 'fetching completions'", err.Error())
	}
}

func TestListTasks_IntervalPreserved(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "Quarterly", Type: "bill", Interval: 3},
		{ID: 2, Title: "Annual", Type: "subscription", Interval: 12},
	}
	srv := mockMontlyServer(tasks, nil)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	intervalMap := map[int64]int{}
	for _, et := range summary.Tasks {
		intervalMap[et.ID] = et.Interval
	}
	if intervalMap[1] != 3 {
		t.Errorf("task 1 interval = %d, want 3", intervalMap[1])
	}
	if intervalMap[2] != 12 {
		t.Errorf("task 2 interval = %d, want 12", intervalMap[2])
	}
}
