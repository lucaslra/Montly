package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

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

func (c *montlyClient) get(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("montly request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("montly API %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
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
