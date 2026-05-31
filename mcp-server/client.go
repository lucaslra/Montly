package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var monthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

func isValidMonth(m string) bool {
	return monthRe.MatchString(m)
}

type montlyClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func newMontlyClient() *montlyClient {
	base := os.Getenv("MONTLY_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	token := os.Getenv("MONTLY_TOKEN")
	if token == "" {
		log.Fatal("MONTLY_TOKEN is required. Create one in Montly → Settings → API Tokens.")
	}
	return &montlyClient{
		baseURL: strings.TrimRight(base, "/"),
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *montlyClient) do(method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+"/api"+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("montly request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB cap
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("montly API %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

func (c *montlyClient) get(path string) ([]byte, error) {
	return c.do(http.MethodGet, path, nil)
}

func (c *montlyClient) post(path string, body any) ([]byte, error) {
	return c.do(http.MethodPost, path, body)
}

func (c *montlyClient) patch(path string, body any) ([]byte, error) {
	return c.do(http.MethodPatch, path, body)
}

func monthQuery(month string) string {
	return "?month=" + url.QueryEscape(month)
}

type task struct {
	ID          int64           `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Amount      string          `json:"amount"`
	Interval    int             `json:"interval"`
	IsShared    bool            `json:"is_shared,omitempty"`
	OwnerName   string          `json:"owner_name,omitempty"`
	Metadata    json.RawMessage `json:"metadata"`
}

type completion struct {
	TaskID      int64  `json:"task_id"`
	Month       string `json:"month"`
	CompletedAt string `json:"completed_at"`
	ReceiptFile string `json:"receipt_file"`
	Amount      string `json:"amount"`
	Note        string `json:"note"`
	Skipped     bool   `json:"skipped"`
}
