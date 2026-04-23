package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// allowedWebhookEvents is the set of event names clients may subscribe to.
var allowedWebhookEvents = map[string]bool{
	"task.completed":   true,
	"task.uncompleted": true,
}

// WebhookHandler handles CRUD for webhooks.
type WebhookHandler struct {
	db *DB
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
			writeError(w, fmt.Sprintf("unknown event %q; allowed: task.completed, task.uncompleted", ev), http.StatusBadRequest)
			return
		}
	}
	eventsStr := strings.Join(req.Events, ",")

	if len(req.Secret) > 200 {
		writeError(w, "secret must be 200 characters or fewer", http.StatusBadRequest)
		return
	}

	hook, err := h.db.CreateWebhook(currentUser(r).UserID, req.URL, eventsStr, req.Secret)
	if err != nil {
		writeServerError(w, "failed to create webhook", err)
		return
	}
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
	w.WriteHeader(http.StatusNoContent)
}

// ── Firing ────────────────────────────────────────────────────────────────────

type webhookPayload struct {
	Event     string `json:"event"`
	TaskID    int64  `json:"task_id"`
	TaskTitle string `json:"task_title"`
	Month     string `json:"month"`
	Timestamp string `json:"timestamp"`
}

// FireWebhooks sends the event to all matching webhooks for userID. Runs in a goroutine;
// failures are logged but never bubble up to the caller.
func FireWebhooks(db *DB, userID int64, event string, taskID int64, taskTitle, month string) {
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

	client := &http.Client{Timeout: 10 * time.Second}

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
