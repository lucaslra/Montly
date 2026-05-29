package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

type listTasksInput struct {
	Month string `json:"month,omitempty" jsonschema:"Month in YYYY-MM format (e.g. 2026-05). Defaults to current month."`
}

func registerTools(server *mcp.Server, client *montlyClient) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_tasks",
		Description: "List recurring tasks for a given month with their completion status. " +
			"Returns each task's title, type (payment/subscription/bill/reminder), " +
			"amount, status (pending/completed/skipped), and whether a receipt is attached.",
	}, listTasksHandler(client))
}

func listTasksHandler(client *montlyClient) func(context.Context, *mcp.CallToolRequest, listTasksInput) (*mcp.CallToolResult, any, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in listTasksInput) (*mcp.CallToolResult, any, error) {
		month := in.Month
		if month == "" {
			month = time.Now().Format("2006-01")
		}

		tasksJSON, err := client.get("/tasks?month=" + month)
		if err != nil {
			return nil, nil, fmt.Errorf("fetching tasks: %w", err)
		}
		complJSON, err := client.get("/completions?month=" + month)
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

		out, _ := json.MarshalIndent(summary, "", "  ")
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(out)}},
		}, nil, nil
	}
}
