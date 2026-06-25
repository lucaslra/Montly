package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// blockedCIDRs lists address ranges webhook delivery must never reach:
// loopback, RFC 1918 private, link-local (covers cloud metadata endpoints
// like 169.254.169.254), carrier-grade NAT, and IPv6 equivalents.
var blockedCIDRs []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",   // IPv4 loopback
		"::1/128",       // IPv6 loopback
		"10.0.0.0/8",   // RFC 1918
		"172.16.0.0/12", // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // link-local / cloud metadata (AWS, GCP, Azure)
		"fe80::/10",     // IPv6 link-local
		"fc00::/7",      // IPv6 unique local
		"100.64.0.0/10", // carrier-grade NAT (RFC 6598)
		"0.0.0.0/8",    // "this" network
		"::/128",        // IPv6 unspecified
	} {
		_, block, _ := net.ParseCIDR(cidr)
		blockedCIDRs = append(blockedCIDRs, block)
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, block := range blockedCIDRs {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// safeWebhookClient returns an http.Client that refuses connections to
// private, loopback, and cloud-metadata IP ranges at the DNS-resolution
// level, and re-validates each redirect destination the same way.
func safeWebhookClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip == nil || isPrivateIP(ip) {
					return nil, fmt.Errorf("webhook: refusing connection to private/internal address %s", ipStr)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("webhook: too many redirects")
			}
			ips, err := net.DefaultResolver.LookupHost(req.Context(), req.URL.Hostname())
			if err != nil {
				return fmt.Errorf("webhook: redirect resolve failed: %w", err)
			}
			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip == nil || isPrivateIP(ip) {
					return fmt.Errorf("webhook: refusing redirect to private/internal address %s", ipStr)
				}
			}
			return nil
		},
	}
}

// allowedWebhookEvents is the set of event names clients may subscribe to.
var allowedWebhookEvents = map[string]bool{
	"task.completed":   true,
	"task.uncompleted": true,
	"task.skipped":     true,
	"month.digest":     true,
}

// WebhookHandler handles CRUD for webhooks.
type WebhookHandler struct {
	db           *DB
	client       *http.Client
	digestSecret string
}

// webhookResponse is the API-safe view of a Webhook (omits secret).
type webhookResponse struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	URL       string `json:"url"`
	Events    string `json:"events"`
	CreatedAt string `json:"created_at"`
}

func toWebhookResponse(wh Webhook) webhookResponse {
	return webhookResponse{
		ID:        wh.ID,
		UserID:    wh.UserID,
		URL:       wh.URL,
		Events:    wh.Events,
		CreatedAt: wh.CreatedAt,
	}
}

func (h *WebhookHandler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := h.db.ListWebhooks(currentUser(r).UserID)
	if err != nil {
		writeServerError(w, "failed to list webhooks", err)
		return
	}
	resp := make([]webhookResponse, len(hooks))
	for i, wh := range hooks {
		resp[i] = toWebhookResponse(wh)
	}
	if resp == nil {
		resp = []webhookResponse{}
	}
	writeJSON(w, resp)
}

func (h *WebhookHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10) // 4 KB
	var req struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
		Secret string   `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeError(w, "url is required", http.StatusBadRequest)
		return
	}
	parsed, err := url.ParseRequestURI(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		writeError(w, "url must be a valid http or https URL", http.StatusBadRequest)
		return
	}
	if len(req.URL) > 2048 {
		writeError(w, "url too long", http.StatusBadRequest)
		return
	}

	if len(req.Events) == 0 {
		writeError(w, "at least one event is required", http.StatusBadRequest)
		return
	}
	for _, ev := range req.Events {
		if !allowedWebhookEvents[ev] {
			writeError(w, fmt.Sprintf("unknown event %q; allowed: task.completed, task.uncompleted, task.skipped, month.digest", ev), http.StatusBadRequest)
			return
		}
	}
	eventsStr := strings.Join(req.Events, ",")

	if len(req.Secret) > 200 {
		writeError(w, "secret must be 200 characters or fewer", http.StatusBadRequest)
		return
	}

	userID := currentUser(r).UserID
	hook, err := h.db.CreateWebhook(userID, req.URL, eventsStr, req.Secret)
	if err != nil {
		writeServerError(w, "failed to create webhook", err)
		return
	}
	go h.db.InsertAuditLog(userID, "create_webhook", "webhook", hook.ID, hook.URL)
	writeJSONCreated(w, toWebhookResponse(hook))
}

func (h *WebhookHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.db.DeleteWebhook(id, currentUser(r).UserID); errors.Is(err, sql.ErrNoRows) {
		writeError(w, "webhook not found", http.StatusNotFound)
		return
	} else if err != nil {
		writeServerError(w, "failed to delete webhook", err)
		return
	}
	go h.db.InsertAuditLog(currentUser(r).UserID, "delete_webhook", "webhook", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (h *WebhookHandler) TestWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid id", http.StatusBadRequest)
		return
	}
	hook, err := h.db.getWebhookByID(id)
	if err != nil || hook.UserID != currentUser(r).UserID {
		writeError(w, "webhook not found", http.StatusNotFound)
		return
	}

	events := strings.Split(hook.Events, ",")
	event := strings.TrimSpace(events[0])
	now := time.Now().UTC().Format(time.RFC3339)
	month := time.Now().UTC().Format("2006-01")

	var body []byte
	if event == "month.digest" {
		body, _ = json.Marshal(digestPayload{
			Event:       "month.digest",
			Month:       month,
			TaskCount:   1,
			Tasks:       []digestTaskItem{{ID: 0, Title: "Test webhook from Montly", Type: "bill", Amount: "42.00"}},
			TotalAmount: "42.00",
			Timestamp:   now,
		})
	} else {
		body, _ = json.Marshal(webhookPayload{
			Event:     event,
			TaskID:    0,
			TaskTitle: "Test webhook from Montly",
			Month:     month,
			Timestamp: now,
		})
	}

	req, err := http.NewRequest(http.MethodPost, hook.URL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": fmt.Sprintf("invalid URL: %v", err)})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Montly-Webhook/1")
	if event == "month.digest" && h.digestSecret != "" {
		req.Header.Set("X-Montly-Secret", h.digestSecret)
	}
	if hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(hook.Secret))
		mac.Write(body)
		req.Header.Set("X-Montly-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := h.client.Do(req)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	resp.Body.Close()
	writeJSON(w, map[string]any{"ok": resp.StatusCode < 400, "status": resp.StatusCode})
}

// ── Firing ────────────────────────────────────────────────────────────────────

type webhookPayload struct {
	Event     string `json:"event"`
	TaskID    int64  `json:"task_id"`
	TaskTitle string `json:"task_title"`
	Month     string `json:"month"`
	Timestamp string `json:"timestamp"`
}

type digestTaskItem struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	Amount string `json:"amount,omitempty"`
}

type digestPayload struct {
	Event       string           `json:"event"`
	Month       string           `json:"month"`
	TaskCount   int              `json:"task_count"`
	Tasks       []digestTaskItem `json:"tasks"`
	TotalAmount string           `json:"total_amount,omitempty"`
	Timestamp   string           `json:"timestamp"`
}

// FireWebhooks sends the event to all matching webhooks for userID. Runs in a goroutine;
// failures are logged but never bubble up to the caller.
func FireWebhooks(db *DB, userID int64, event string, taskID int64, taskTitle, month string, client *http.Client) {
	hooks, err := db.GetWebhooksForUser(userID)
	if err != nil {
		log.Printf("FireWebhooks: list hooks: %v", err)
		return
	}

	payload := webhookPayload{
		Event:     event,
		TaskID:    taskID,
		TaskTitle: taskTitle,
		Month:     month,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)

	for _, wh := range hooks {
		if !strings.Contains(","+wh.Events+",", ","+event+",") {
			continue
		}
		go func(hook Webhook) {
			req, err := http.NewRequest(http.MethodPost, hook.URL, bytes.NewReader(body))
			if err != nil {
				log.Printf("FireWebhooks(%d): build request: %v", hook.ID, err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Montly-Webhook/1")
			if hook.Secret != "" {
				mac := hmac.New(sha256.New, []byte(hook.Secret))
				mac.Write(body)
				req.Header.Set("X-Montly-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			}
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("FireWebhooks(%d): deliver: %v", hook.ID, err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				log.Printf("FireWebhooks(%d): remote returned %d", hook.ID, resp.StatusCode)
			}
		}(wh)
	}
}

// maxDigestConcurrency caps the number of simultaneous outbound HTTP deliveries
// across all users during a month.digest run.
const maxDigestConcurrency = 20

// FireMonthDigest sends a month.digest webhook to every user subscribed to that
// event. Called by the monthly scheduler; runs entirely in background goroutines.
func FireMonthDigest(db *DB, month string, client *http.Client, digestSecret string) {
	users, err := db.ListUsers()
	if err != nil {
		log.Printf("FireMonthDigest: list users: %v", err)
		return
	}

	sem := make(chan struct{}, maxDigestConcurrency)

	for _, u := range users {
		hooks, err := db.GetWebhooksForUser(u.ID)
		if err != nil {
			log.Printf("FireMonthDigest: hooks for user %d: %v", u.ID, err)
			continue
		}
		var digestHooks []Webhook
		for _, h := range hooks {
			if strings.Contains(","+h.Events+",", ",month.digest,") {
				digestHooks = append(digestHooks, h)
			}
		}
		if len(digestHooks) == 0 {
			continue
		}

		tasks, err := db.GetTasks(month, u.ID)
		if err != nil {
			log.Printf("FireMonthDigest: tasks for user %d: %v", u.ID, err)
			continue
		}

		items := make([]digestTaskItem, 0, len(tasks))
		var total float64
		for _, t := range tasks {
			if v, err := strconv.ParseFloat(t.Amount, 64); err == nil {
				total += v
			}
			items = append(items, digestTaskItem{ID: t.ID, Title: t.Title, Type: t.Type, Amount: t.Amount})
		}
		var totalStr string
		if total > 0 {
			totalStr = strconv.FormatFloat(total, 'f', 2, 64)
		}

		payload := digestPayload{
			Event:       "month.digest",
			Month:       month,
			TaskCount:   len(items),
			Tasks:       items,
			TotalAmount: totalStr,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}
		body, _ := json.Marshal(payload)

		for _, hook := range digestHooks {
			sem <- struct{}{}
			go func(h Webhook) {
				defer func() { <-sem }()
				req, err := http.NewRequest(http.MethodPost, h.URL, bytes.NewReader(body))
				if err != nil {
					log.Printf("FireMonthDigest(%d): build request: %v", h.ID, err)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("User-Agent", "Montly-Webhook/1")
				if digestSecret != "" {
					req.Header.Set("X-Montly-Secret", digestSecret)
				}
				if h.Secret != "" {
					mac := hmac.New(sha256.New, []byte(h.Secret))
					mac.Write(body)
					req.Header.Set("X-Montly-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
				}
				resp, err := client.Do(req)
				if err != nil {
					log.Printf("FireMonthDigest(%d): deliver: %v", h.ID, err)
					return
				}
				resp.Body.Close()
				if resp.StatusCode >= 400 {
					log.Printf("FireMonthDigest(%d): remote returned %d", h.ID, resp.StatusCode)
				}
			}(hook)
		}
	}
}
