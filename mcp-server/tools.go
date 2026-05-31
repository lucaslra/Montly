package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── Response types ──────────────────────────────────────────────────────────

type enrichedTask struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Type       string `json:"type"`
	Amount     string `json:"amount,omitempty"`
	Status     string `json:"status"`
	Interval   int    `json:"interval"`
	Paid       string `json:"paid,omitempty"`
	Note       string `json:"note,omitempty"`
	HasReceipt bool   `json:"has_receipt"`
	SharedBy   string `json:"shared_by,omitempty"`
}

type listTasksSummary struct {
	Month     string         `json:"month"`
	Total     int            `json:"total"`
	Completed int            `json:"completed"`
	Skipped   int            `json:"skipped"`
	Pending   int            `json:"pending"`
	Tasks     []enrichedTask `json:"tasks"`
}

type reportMonth struct {
	Month      string `json:"month"`
	IsForecast bool   `json:"is_forecast"`
	TaskCount  int    `json:"task_count"`
	Completed  int    `json:"completed"`
	Skipped    int    `json:"skipped"`
	TotalDue   float64 `json:"total_due"`
	TotalPaid  float64 `json:"total_paid"`
}

type reportSummary struct {
	Anchor string        `json:"anchor"`
	Months []reportMonth `json:"months"`
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func resolveMonth(month string) (string, error) {
	if month == "" {
		return time.Now().Format("2006-01"), nil
	}
	if !isValidMonth(month) {
		return "", fmt.Errorf("invalid month format %q, expected YYYY-MM", month)
	}
	return month, nil
}

func textResult(v any) (*mcp.CallToolResult, any, error) {
	out, _ := json.MarshalIndent(v, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
	}, nil, nil
}

func msgResult(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, nil, nil
}

// ── Tool registration ───────────────────────────────────────────────────────

func registerTools(server *mcp.Server, client *montlyClient) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_tasks",
		Description: "List recurring tasks for a given month with their completion status. " +
			"Returns each task's title, type (payment/subscription/bill/reminder), " +
			"amount, status (pending/completed/skipped), and whether a receipt is attached.",
	}, listTasksHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_report",
		Description: "Get a spending report with 6 months of history and 3 months of forecast. " +
			"Returns per-month totals: task count, completed, skipped, total due, and total paid.",
	}, getReportHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name: "toggle_task",
		Description: "Mark a task as done for a given month, or undo it if already completed. " +
			"If the task is currently skipped, toggling it marks it as completed.",
	}, toggleTaskHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name: "skip_task",
		Description: "Skip a task for a given month (or un-skip if already skipped). " +
			"A skipped task is excluded from progress and spending totals. " +
			"Cannot skip a task that is already completed — toggle it first.",
	}, skipTaskHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name: "update_completion",
		Description: "Update the paid amount or note on a completed task for a given month. " +
			"The task must already be marked as done (not pending or skipped). " +
			"Use this to record the actual amount paid or add context.",
	}, updateCompletionHandler(client))

	mcp.AddTool(server, &mcp.Tool{
		Name: "create_task",
		Description: "Create a new recurring task. " +
			"Provide a title and optionally a type (payment/subscription/bill/reminder), " +
			"amount, interval (1=monthly, 2=bimonthly, 3=quarterly, 6=semi-annual, 12=annual), " +
			"and start/end dates in YYYY-MM format.",
	}, createTaskHandler(client))
}

// ── list_tasks ──────────────────────────────────────────────────────────────

type listTasksInput struct {
	Month string `json:"month,omitempty" jsonschema:"Month in YYYY-MM format (e.g. 2026-05). Defaults to current month."`
}

func listTasksHandler(client *montlyClient) func(context.Context, *mcp.CallToolRequest, listTasksInput) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in listTasksInput) (*mcp.CallToolResult, any, error) {
		month, err := resolveMonth(in.Month)
		if err != nil {
			return nil, nil, err
		}

		tasksJSON, err := client.get("/tasks" + monthQuery(month))
		if err != nil {
			return nil, nil, fmt.Errorf("fetching tasks: %w", err)
		}
		complJSON, err := client.get("/completions" + monthQuery(month))
		if err != nil {
			return nil, nil, fmt.Errorf("fetching completions: %w", err)
		}

		var tasks []task
		var completions []completion
		if err := json.Unmarshal(tasksJSON, &tasks); err != nil {
			return nil, nil, fmt.Errorf("decoding tasks: %w", err)
		}
		if err := json.Unmarshal(complJSON, &completions); err != nil {
			return nil, nil, fmt.Errorf("decoding completions: %w", err)
		}

		cMap := make(map[int64]completion, len(completions))
		for _, c := range completions {
			cMap[c.TaskID] = c
		}

		enriched := make([]enrichedTask, 0, len(tasks))
		var nCompleted, nSkipped int
		for _, t := range tasks {
			et := enrichedTask{
				ID:       t.ID,
				Title:    t.Title,
				Type:     t.Type,
				Amount:   t.Amount,
				Status:   "pending",
				Interval: t.Interval,
			}
			if t.IsShared {
				et.SharedBy = t.OwnerName
			}
			if c, ok := cMap[t.ID]; ok {
				if c.Skipped {
					et.Status = "skipped"
					nSkipped++
				} else {
					et.Status = "completed"
					nCompleted++
				}
				et.Paid = c.Amount
				et.Note = c.Note
				et.HasReceipt = c.ReceiptFile != ""
			}
			enriched = append(enriched, et)
		}

		summary := listTasksSummary{
			Month:     month,
			Total:     len(enriched),
			Completed: nCompleted,
			Skipped:   nSkipped,
			Pending:   len(enriched) - nCompleted - nSkipped,
			Tasks:     enriched,
		}

		return textResult(summary)
	}
}

// ── get_report ──────────────────────────────────────────────────────────────

type getReportInput struct {
	Month string `json:"month,omitempty" jsonschema:"Anchor month in YYYY-MM format. Report covers 6 months before and 3 months after this month. Defaults to current month."`
}

func getReportHandler(client *montlyClient) func(context.Context, *mcp.CallToolRequest, getReportInput) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in getReportInput) (*mcp.CallToolResult, any, error) {
		month, err := resolveMonth(in.Month)
		if err != nil {
			return nil, nil, err
		}

		body, err := client.get("/report?anchor=" + url.QueryEscape(month))
		if err != nil {
			return nil, nil, fmt.Errorf("fetching report: %w", err)
		}

		var raw struct {
			Months []struct {
				Month       string `json:"month"`
				IsForecast  bool   `json:"is_forecast"`
				Tasks       []task       `json:"tasks"`
				Completions []completion `json:"completions"`
			} `json:"months"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, nil, fmt.Errorf("decoding report: %w", err)
		}

		// Summarize each month into digestible stats for the LLM.
		months := make([]reportMonth, 0, len(raw.Months))
		for _, m := range raw.Months {
			cMap := make(map[int64]completion, len(m.Completions))
			for _, c := range m.Completions {
				cMap[c.TaskID] = c
			}

			rm := reportMonth{
				Month:      m.Month,
				IsForecast: m.IsForecast,
				TaskCount:  len(m.Tasks),
			}
			for _, t := range m.Tasks {
				amount := parseFloat(t.Amount)
				if c, ok := cMap[t.ID]; ok {
					if c.Skipped {
						rm.Skipped++
						continue // skipped tasks excluded from due/paid totals
					}
					rm.Completed++
					paid := parseFloat(c.Amount)
					if paid == 0 {
						paid = amount
					}
					rm.TotalPaid += paid
				}
				rm.TotalDue += amount
			}
			months = append(months, rm)
		}

		return textResult(reportSummary{Anchor: month, Months: months})
	}
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// ── toggle_task ─────────────────────────────────────────────────────────────

type toggleTaskInput struct {
	TaskID int64  `json:"task_id" jsonschema:"required,ID of the task to toggle."`
	Month  string `json:"month,omitempty" jsonschema:"Month in YYYY-MM format. Defaults to current month."`
}

func toggleTaskHandler(client *montlyClient) func(context.Context, *mcp.CallToolRequest, toggleTaskInput) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in toggleTaskInput) (*mcp.CallToolResult, any, error) {
		month, err := resolveMonth(in.Month)
		if err != nil {
			return nil, nil, err
		}
		if in.TaskID == 0 {
			return nil, nil, fmt.Errorf("task_id is required")
		}

		body, err := client.post("/completions/toggle", map[string]any{
			"task_id": in.TaskID,
			"month":   month,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("toggling task: %w", err)
		}

		var result struct {
			Completed bool `json:"completed"`
		}
		json.Unmarshal(body, &result)

		status := "completed"
		if !result.Completed {
			status = "pending"
		}
		return msgResult(fmt.Sprintf("Task %d is now %s for %s.", in.TaskID, status, month))
	}
}

// ── skip_task ───────────────────────────────────────────────────────────────

type skipTaskInput struct {
	TaskID int64  `json:"task_id" jsonschema:"required,ID of the task to skip or un-skip."`
	Month  string `json:"month,omitempty" jsonschema:"Month in YYYY-MM format. Defaults to current month."`
}

func skipTaskHandler(client *montlyClient) func(context.Context, *mcp.CallToolRequest, skipTaskInput) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in skipTaskInput) (*mcp.CallToolResult, any, error) {
		month, err := resolveMonth(in.Month)
		if err != nil {
			return nil, nil, err
		}
		if in.TaskID == 0 {
			return nil, nil, fmt.Errorf("task_id is required")
		}

		body, err := client.post("/completions/skip", map[string]any{
			"task_id": in.TaskID,
			"month":   month,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("skipping task: %w", err)
		}

		var result struct {
			Skipped bool `json:"skipped"`
		}
		json.Unmarshal(body, &result)

		status := "skipped"
		if !result.Skipped {
			status = "no longer skipped (pending)"
		}
		return msgResult(fmt.Sprintf("Task %d is now %s for %s.", in.TaskID, status, month))
	}
}

// ── update_completion ───────────────────────────────────────────────────────

type updateCompletionInput struct {
	TaskID int64   `json:"task_id" jsonschema:"required,ID of the task."`
	Month  string  `json:"month,omitempty" jsonschema:"Month in YYYY-MM format. Defaults to current month."`
	Amount *string `json:"amount,omitempty" jsonschema:"Actual amount paid. Pass empty string to clear the override."`
	Note   *string `json:"note,omitempty" jsonschema:"Note to attach to this completion."`
}

func updateCompletionHandler(client *montlyClient) func(context.Context, *mcp.CallToolRequest, updateCompletionInput) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in updateCompletionInput) (*mcp.CallToolResult, any, error) {
		month, err := resolveMonth(in.Month)
		if err != nil {
			return nil, nil, err
		}
		if in.TaskID == 0 {
			return nil, nil, fmt.Errorf("task_id is required")
		}
		if in.Amount == nil && in.Note == nil {
			return nil, nil, fmt.Errorf("at least one of amount or note is required")
		}

		payload := map[string]any{}
		if in.Amount != nil {
			payload["amount"] = *in.Amount
		}
		if in.Note != nil {
			payload["note"] = *in.Note
		}

		path := fmt.Sprintf("/completions/%d/%s", in.TaskID, url.PathEscape(month))
		body, err := client.patch(path, payload)
		if err != nil {
			return nil, nil, fmt.Errorf("updating completion: %w", err)
		}

		var c completion
		json.Unmarshal(body, &c)

		return msgResult(fmt.Sprintf("Updated task %d for %s. Amount: %q, Note: %q.", in.TaskID, month, c.Amount, c.Note))
	}
}

// ── create_task ─────────────────────────────────────────────────────────────

type createTaskInput struct {
	Title       string `json:"title" jsonschema:"required,Name of the recurring task."`
	Type        string `json:"type,omitempty" jsonschema:"Task type: payment, subscription, bill, reminder, or empty."`
	Amount      string `json:"amount,omitempty" jsonschema:"Default amount (e.g. 1200.00). Only for payment/subscription/bill types."`
	Description string `json:"description,omitempty" jsonschema:"Optional description or details."`
	Interval    int    `json:"interval,omitempty" jsonschema:"Recurrence: 1=monthly (default), 2=bimonthly, 3=quarterly, 6=semi-annual, 12=annual."`
	StartDate   string `json:"start_date,omitempty" jsonschema:"First active month in YYYY-MM format."`
	EndDate     string `json:"end_date,omitempty" jsonschema:"Last active month in YYYY-MM format."`
}

func createTaskHandler(client *montlyClient) func(context.Context, *mcp.CallToolRequest, createTaskInput) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in createTaskInput) (*mcp.CallToolResult, any, error) {
		if in.Title == "" {
			return nil, nil, fmt.Errorf("title is required")
		}
		if in.StartDate != "" && !isValidMonth(in.StartDate) {
			return nil, nil, fmt.Errorf("start_date must be YYYY-MM format")
		}
		if in.EndDate != "" && !isValidMonth(in.EndDate) {
			return nil, nil, fmt.Errorf("end_date must be YYYY-MM format")
		}

		payload := map[string]any{
			"title": in.Title,
		}
		if in.Type != "" {
			payload["type"] = in.Type
		}
		if in.Amount != "" {
			payload["amount"] = in.Amount
		}
		if in.Description != "" {
			payload["description"] = in.Description
		}
		if in.Interval > 0 {
			payload["interval"] = in.Interval
		}
		if in.StartDate != "" {
			payload["start_date"] = in.StartDate
		}
		if in.EndDate != "" {
			payload["end_date"] = in.EndDate
		}

		body, err := client.post("/tasks", payload)
		if err != nil {
			return nil, nil, fmt.Errorf("creating task: %w", err)
		}

		var t task
		json.Unmarshal(body, &t)

		return msgResult(fmt.Sprintf("Created task %d: %q (type: %s, amount: %s, interval: %d).", t.ID, t.Title, t.Type, t.Amount, t.Interval))
	}
}
