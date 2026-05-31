package main

import (
	"context"
	"encoding/json"
	"io"
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

// ── list_tasks tests ────────────────────────────────────────────────────────

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
	tasks := []task{{ID: 1, Title: "Rent", Type: "payment", Amount: "1200", Interval: 1}}
	completions := []completion{{TaskID: 1, Month: "2026-05", Amount: "1250"}}
	srv := mockMontlyServer(tasks, completions)
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	summary := callListTasks(t, client, "2026-05")

	if summary.Tasks[0].Amount != "1200" {
		t.Errorf("amount = %q, want 1200", summary.Tasks[0].Amount)
	}
	if summary.Tasks[0].Paid != "1250" {
		t.Errorf("paid = %q, want 1250", summary.Tasks[0].Paid)
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

	if !summary.Tasks[0].HasReceipt {
		t.Error("task 1 should have receipt")
	}
	if summary.Tasks[1].HasReceipt {
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

	if summary.Tasks[0].SharedBy != "alice" {
		t.Errorf("shared_by = %q, want alice", summary.Tasks[0].SharedBy)
	}
	if summary.Tasks[1].SharedBy != "" {
		t.Errorf("shared_by = %q, want empty", summary.Tasks[1].SharedBy)
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
		t.Errorf("month = %q, want %q", gotMonth, expected)
	}
}

func TestListTasks_InvalidMonth(t *testing.T) {
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := listTasksHandler(client)
	_, _, err := handler(context.Background(), nil, listTasksInput{Month: "bad"})
	if err == nil {
		t.Fatal("expected error for invalid month")
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
		t.Fatal("expected error")
	}
}

func TestListTasks_APIErrorOnCompletions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/completions") {
			w.WriteHeader(http.StatusInternalServerError)
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
		t.Fatal("expected error")
	}
}

// ── get_report tests ────────────────────────────────────────────────────────

func TestGetReport_SummarizesMonths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"months": []map[string]any{
				{
					"month":       "2026-04",
					"is_forecast": false,
					"tasks": []map[string]any{
						{"id": 1, "title": "Rent", "type": "payment", "amount": "1200"},
						{"id": 2, "title": "Netflix", "type": "subscription", "amount": "15.99"},
					},
					"completions": []map[string]any{
						{"task_id": 1, "month": "2026-04", "amount": "1200"},
					},
				},
				{
					"month":       "2026-08",
					"is_forecast": true,
					"tasks": []map[string]any{
						{"id": 1, "title": "Rent", "type": "payment", "amount": "1200"},
					},
					"completions": []map[string]any{},
				},
			},
		})
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := getReportHandler(client)
	result, _, err := handler(context.Background(), nil, getReportInput{Month: "2026-05"})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var summary reportSummary
	json.Unmarshal([]byte(text), &summary)

	if summary.Anchor != "2026-05" {
		t.Errorf("anchor = %q", summary.Anchor)
	}
	if len(summary.Months) != 2 {
		t.Fatalf("months = %d, want 2", len(summary.Months))
	}

	m0 := summary.Months[0]
	if m0.Completed != 1 {
		t.Errorf("month[0] completed = %d, want 1", m0.Completed)
	}
	if m0.TotalPaid != 1200 {
		t.Errorf("month[0] paid = %f, want 1200", m0.TotalPaid)
	}
	if m0.IsForecast {
		t.Error("month[0] should not be forecast")
	}

	m1 := summary.Months[1]
	if !m1.IsForecast {
		t.Error("month[1] should be forecast")
	}
	if m1.Completed != 0 {
		t.Errorf("month[1] completed = %d, want 0", m1.Completed)
	}
}

func TestGetReport_ExcludesSkippedFromDue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"months": []map[string]any{
				{
					"month":       "2026-05",
					"is_forecast": false,
					"tasks": []map[string]any{
						{"id": 1, "title": "Rent", "type": "payment", "amount": "1200"},
						{"id": 2, "title": "Gym", "type": "payment", "amount": "50"},
					},
					"completions": []map[string]any{
						{"task_id": 1, "month": "2026-05", "amount": "1200"},
						{"task_id": 2, "month": "2026-05", "skipped": true},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := getReportHandler(client)
	result, _, err := handler(context.Background(), nil, getReportInput{Month: "2026-05"})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var summary reportSummary
	json.Unmarshal([]byte(text), &summary)

	m := summary.Months[0]
	if m.Completed != 1 {
		t.Errorf("completed = %d, want 1", m.Completed)
	}
	if m.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", m.Skipped)
	}
	// Skipped task ($50) should be excluded from TotalDue
	if m.TotalDue != 1200 {
		t.Errorf("total_due = %f, want 1200 (skipped task excluded)", m.TotalDue)
	}
	if m.TotalPaid != 1200 {
		t.Errorf("total_paid = %f, want 1200", m.TotalPaid)
	}
}

func TestGetReport_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := getReportHandler(client)
	_, _, err := handler(context.Background(), nil, getReportInput{Month: "2026-05"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── toggle_task tests ───────────────────────────────────────────────────────

func TestToggleTask_Completes(t *testing.T) {
	var gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"completed":true}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := toggleTaskHandler(client)
	result, _, err := handler(context.Background(), nil, toggleTaskInput{TaskID: 42, Month: "2026-05"})
	if err != nil {
		t.Fatal(err)
	}

	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if int(gotBody["task_id"].(float64)) != 42 {
		t.Errorf("task_id = %v", gotBody["task_id"])
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "completed") {
		t.Errorf("result = %q, want 'completed'", text)
	}
}

func TestToggleTask_Uncompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"completed":false}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := toggleTaskHandler(client)
	result, _, err := handler(context.Background(), nil, toggleTaskInput{TaskID: 1, Month: "2026-05"})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "pending") {
		t.Errorf("result = %q, want 'pending'", text)
	}
}

func TestToggleTask_MissingTaskID(t *testing.T) {
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := toggleTaskHandler(client)
	_, _, err := handler(context.Background(), nil, toggleTaskInput{Month: "2026-05"})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
}

func TestToggleTask_DefaultsMonth(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.Write([]byte(`{"completed":true}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := toggleTaskHandler(client)
	handler(context.Background(), nil, toggleTaskInput{TaskID: 1})

	expected := time.Now().Format("2006-01")
	if gotBody["month"] != expected {
		t.Errorf("month = %v, want %s", gotBody["month"], expected)
	}
}

// ── skip_task tests ─────────────────────────────────────────────────────────

func TestSkipTask_Skips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"skipped":true,"completion":{}}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := skipTaskHandler(client)
	result, _, err := handler(context.Background(), nil, skipTaskInput{TaskID: 1, Month: "2026-05"})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "skipped") {
		t.Errorf("result = %q", text)
	}
}

func TestSkipTask_Unskips(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"skipped":false}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := skipTaskHandler(client)
	result, _, err := handler(context.Background(), nil, skipTaskInput{TaskID: 1, Month: "2026-05"})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "pending") {
		t.Errorf("result = %q", text)
	}
}

// ── update_completion tests ─────────────────────────────────────────────────

func TestUpdateCompletion_SetsAmount(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.Write([]byte(`{"task_id":1,"month":"2026-05","amount":"150","note":""}`))
	}))
	defer srv.Close()

	amt := "150"
	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := updateCompletionHandler(client)
	result, _, err := handler(context.Background(), nil, updateCompletionInput{TaskID: 1, Month: "2026-05", Amount: &amt})
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != "/api/completions/1/2026-05" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["amount"] != "150" {
		t.Errorf("body.amount = %v", gotBody["amount"])
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "150") {
		t.Errorf("result = %q", text)
	}
}

func TestUpdateCompletion_SetsNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"task_id":1,"month":"2026-05","amount":"","note":"wire transfer"}`))
	}))
	defer srv.Close()

	note := "wire transfer"
	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := updateCompletionHandler(client)
	result, _, err := handler(context.Background(), nil, updateCompletionInput{TaskID: 1, Month: "2026-05", Note: &note})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "wire transfer") {
		t.Errorf("result = %q", text)
	}
}

func TestUpdateCompletion_RequiresAmountOrNote(t *testing.T) {
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := updateCompletionHandler(client)
	_, _, err := handler(context.Background(), nil, updateCompletionInput{TaskID: 1, Month: "2026-05"})
	if err == nil {
		t.Fatal("expected error when neither amount nor note provided")
	}
}

// ── create_task tests ───────────────────────────────────────────────────────

func TestCreateTask_SendsCorrectPayload(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":99,"title":"Gym","type":"payment","amount":"50","interval":1}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := createTaskHandler(client)
	result, _, err := handler(context.Background(), nil, createTaskInput{
		Title:  "Gym",
		Type:   "payment",
		Amount: "50",
	})
	if err != nil {
		t.Fatal(err)
	}

	if gotBody["title"] != "Gym" {
		t.Errorf("title = %v", gotBody["title"])
	}
	if gotBody["type"] != "payment" {
		t.Errorf("type = %v", gotBody["type"])
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "99") || !strings.Contains(text, "Gym") {
		t.Errorf("result = %q", text)
	}
}

func TestCreateTask_RequiresTitle(t *testing.T) {
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := createTaskHandler(client)
	_, _, err := handler(context.Background(), nil, createTaskInput{})
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestCreateTask_ValidatesStartDate(t *testing.T) {
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := createTaskHandler(client)
	_, _, err := handler(context.Background(), nil, createTaskInput{Title: "Test", StartDate: "bad"})
	if err == nil {
		t.Fatal("expected error for invalid start_date")
	}
}

func TestCreateTask_OmitsEmptyFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":1,"title":"Reminder","type":"","amount":"","interval":1}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := createTaskHandler(client)
	handler(context.Background(), nil, createTaskInput{Title: "Reminder"})

	if _, ok := gotBody["type"]; ok {
		t.Error("empty type should not be in payload")
	}
	if _, ok := gotBody["amount"]; ok {
		t.Error("empty amount should not be in payload")
	}
}

// ── write tool error + validation tests ─────────────────────────────────────

func TestToggleTask_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"task not found"}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := toggleTaskHandler(client)
	_, _, err := handler(context.Background(), nil, toggleTaskInput{TaskID: 999, Month: "2026-05"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestToggleTask_InvalidMonth(t *testing.T) {
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := toggleTaskHandler(client)
	_, _, err := handler(context.Background(), nil, toggleTaskInput{TaskID: 1, Month: "bad"})
	if err == nil {
		t.Fatal("expected error for invalid month")
	}
}

func TestSkipTask_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"task is already completed"}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := skipTaskHandler(client)
	_, _, err := handler(context.Background(), nil, skipTaskInput{TaskID: 1, Month: "2026-05"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSkipTask_MissingTaskID(t *testing.T) {
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := skipTaskHandler(client)
	_, _, err := handler(context.Background(), nil, skipTaskInput{Month: "2026-05"})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
}

func TestUpdateCompletion_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"task not marked as done"}`))
	}))
	defer srv.Close()

	amt := "100"
	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := updateCompletionHandler(client)
	_, _, err := handler(context.Background(), nil, updateCompletionInput{TaskID: 1, Month: "2026-05", Amount: &amt})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpdateCompletion_MissingTaskID(t *testing.T) {
	amt := "100"
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := updateCompletionHandler(client)
	_, _, err := handler(context.Background(), nil, updateCompletionInput{Month: "2026-05", Amount: &amt})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
}

func TestCreateTask_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"type must be one of: payment, subscription, bill, reminder, or empty"}`))
	}))
	defer srv.Close()

	client := &montlyClient{baseURL: srv.URL, token: "mt_test", http: srv.Client()}
	handler := createTaskHandler(client)
	_, _, err := handler(context.Background(), nil, createTaskInput{Title: "Test", Type: "invalid"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateTask_ValidatesEndDate(t *testing.T) {
	client := &montlyClient{baseURL: "http://unused", token: "mt_x"}
	handler := createTaskHandler(client)
	_, _, err := handler(context.Background(), nil, createTaskInput{Title: "Test", EndDate: "not-a-date"})
	if err == nil {
		t.Fatal("expected error for invalid end_date")
	}
}

// ── resolveMonth tests ──────────────────────────────────────────────────────

func TestResolveMonth_Valid(t *testing.T) {
	m, err := resolveMonth("2026-05")
	if err != nil || m != "2026-05" {
		t.Errorf("got %q, %v", m, err)
	}
}

func TestResolveMonth_Empty(t *testing.T) {
	m, err := resolveMonth("")
	if err != nil {
		t.Fatal(err)
	}
	expected := time.Now().Format("2006-01")
	if m != expected {
		t.Errorf("got %q, want %q", m, expected)
	}
}

func TestResolveMonth_Invalid(t *testing.T) {
	_, err := resolveMonth("bad")
	if err == nil {
		t.Fatal("expected error")
	}
}
