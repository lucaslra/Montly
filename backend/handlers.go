package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	db          *DB
	receiptsDir string
}

// receiptFilenameRe enforces that stored/served filenames are always UUID-based.
var receiptFilenameRe = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(pdf|jpg|jpeg|png|webp|gif)$`,
)

var allowedExtensions = map[string]bool{
	".pdf":  true,
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
	".gif":  true,
}

// allowedMIMEPrefixes are the content-sniffed types we accept.
// Using prefix match so "image/jpeg; charset=..." still matches "image/jpeg".
var allowedMIMEPrefixes = []string{
	"image/jpeg",
	"image/png",
	"image/gif",
	"image/webp",
	"application/pdf",
}

func isAllowedMIME(detected string) bool {
	for _, prefix := range allowedMIMEPrefixes {
		if strings.HasPrefix(detected, prefix) {
			return true
		}
	}
	return false
}

func isValidReceiptFilename(f string) bool {
	return receiptFilenameRe.MatchString(f)
}

var allowedTaskTypes = map[string]bool{
	"":             true,
	"payment":      true,
	"subscription": true,
	"bill":         true,
	"reminder":     true,
}

var allowedDateFormats = map[string]bool{
	"long": true, "short": true, "numeric": true, "iso": true,
}

var allowedColorModes = map[string]bool{
	"system": true, "light": true, "dark": true,
}

var allowedTaskSorts = map[string]bool{
	"type": true, "name": true, "default": true,
}

var allowedNumberFormats = map[string]bool{
	"en": true, "eu": true,
}

// taskOwnerCheck loads the task and returns it only if it belongs to userID.
// Writes the appropriate error and returns false on failure.
func (h *Handler) taskOwnerCheck(w http.ResponseWriter, taskID, userID int64) (Task, bool) {
	task, err := h.db.GetTaskByID(taskID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, "task not found", http.StatusNotFound)
		return Task{}, false
	}
	if err != nil {
		writeServerError(w, "failed to get task", err)
		return Task{}, false
	}
	if task.UserID != userID {
		// 404 rather than 403 to avoid leaking existence of other users' tasks.
		writeError(w, "task not found", http.StatusNotFound)
		return Task{}, false
	}
	return task, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: encode error: %v", err)
	}
}

func writeJSONCreated(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// writeServerError logs err server-side and sends a generic 500 to the client.
func writeServerError(w http.ResponseWriter, msg string, err error) {
	log.Printf("error: %s: %v", msg, err)
	writeError(w, msg, http.StatusInternalServerError)
}

// isValidYearMonth checks that s is a valid YYYY-MM string.
func isValidYearMonth(s string) bool {
	if len(s) != 7 || s[4] != '-' {
		return false
	}
	_, e1 := strconv.Atoi(s[:4])
	m, e2 := strconv.Atoi(s[5:])
	return e1 == nil && e2 == nil && m >= 1 && m <= 12
}

// safeRemoveReceipt deletes a receipt file only if the filename is a valid UUID-based name.
func safeRemoveReceipt(receiptsDir, filename string) {
	if filename != "" && isValidReceiptFilename(filename) {
		if err := os.Remove(filepath.Join(receiptsDir, filename)); err != nil && !os.IsNotExist(err) {
			log.Printf("safeRemoveReceipt: %v", err)
		}
	}
}

// --- Settings ---

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.db.GetSettings(currentUser(r).UserID)
	if err != nil {
		writeServerError(w, "failed to get settings", err)
		return
	}
	writeJSON(w, settings)
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	filtered := make(map[string]string)

	if v, ok := req["currency"]; ok {
		v = strings.TrimSpace(v)
		if len(v) > 10 {
			writeError(w, "currency symbol too long", http.StatusBadRequest)
			return
		}
		filtered["currency"] = v
	}
	if v, ok := req["date_format"]; ok {
		if !allowedDateFormats[v] {
			writeError(w, "date_format must be one of: long, short, numeric, iso", http.StatusBadRequest)
			return
		}
		filtered["date_format"] = v
	}
	if v, ok := req["color_mode"]; ok {
		if !allowedColorModes[v] {
			writeError(w, "color_mode must be one of: system, light, dark", http.StatusBadRequest)
			return
		}
		filtered["color_mode"] = v
	}
	if v, ok := req["task_sort"]; ok {
		if !allowedTaskSorts[v] {
			writeError(w, "task_sort must be one of: type, name, default", http.StatusBadRequest)
			return
		}
		filtered["task_sort"] = v
	}
	if v, ok := req["completed_last"]; ok {
		if v != "true" && v != "false" {
			writeError(w, "completed_last must be true or false", http.StatusBadRequest)
			return
		}
		filtered["completed_last"] = v
	}
	if v, ok := req["fiscal_year_start"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 12 {
			writeError(w, "fiscal_year_start must be 1–12", http.StatusBadRequest)
			return
		}
		filtered["fiscal_year_start"] = strconv.Itoa(n)
	}
	if v, ok := req["number_format"]; ok {
		if !allowedNumberFormats[v] {
			writeError(w, "number_format must be one of: en, eu", http.StatusBadRequest)
			return
		}
		filtered["number_format"] = v
	}

	userID := currentUser(r).UserID
	if err := h.db.SaveSettings(userID, filtered); err != nil {
		writeServerError(w, "failed to save settings", err)
		return
	}
	settings, err := h.db.GetSettings(userID)
	if err != nil {
		writeServerError(w, "failed to get settings", err)
		return
	}
	writeJSON(w, settings)
}

// --- Tasks ---

func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	if month == "" {
		writeError(w, "month query param required (YYYY-MM)", http.StatusBadRequest)
		return
	}
	if !isValidYearMonth(month) {
		writeError(w, "month must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	tasks, err := h.db.GetTasks(month, currentUser(r).UserID)
	if err != nil {
		writeServerError(w, "failed to list tasks", err)
		return
	}
	writeJSON(w, tasks)
}

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid id", http.StatusBadRequest)
		return
	}
	task, ok := h.taskOwnerCheck(w, id, currentUser(r).UserID)
	if !ok {
		return
	}
	writeJSON(w, task)
}

var allowedIntervals = map[int]bool{1: true, 2: true, 3: true, 6: true, 12: true}

// taskBody holds the decoded and validated fields shared by CreateTask and UpdateTask.
type taskBody struct {
	Title       string
	Description string
	Type        string
	Metadata    json.RawMessage
	StartDate   string
	EndDate     string
	Interval    int
}

// parseTaskBody decodes and validates the JSON body for task create/update requests.
// On failure it writes the appropriate error response and returns false.
func parseTaskBody(w http.ResponseWriter, r *http.Request) (taskBody, bool) {
	var raw struct {
		Title       string          `json:"title"`
		Description string          `json:"description"`
		Type        string          `json:"type"`
		Metadata    json.RawMessage `json:"metadata"`
		StartDate   string          `json:"start_date"`
		EndDate     string          `json:"end_date"`
		Interval    int             `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return taskBody{}, false
	}
	if raw.Title == "" {
		writeError(w, "title is required", http.StatusBadRequest)
		return taskBody{}, false
	}
	if len(raw.Title) > 200 {
		writeError(w, "title must be 200 characters or fewer", http.StatusBadRequest)
		return taskBody{}, false
	}
	if len(raw.Description) > 5000 {
		writeError(w, "description must be 5000 characters or fewer", http.StatusBadRequest)
		return taskBody{}, false
	}
	if !allowedTaskTypes[raw.Type] {
		writeError(w, "type must be one of: payment, subscription, bill, reminder, or empty", http.StatusBadRequest)
		return taskBody{}, false
	}
	if len(raw.Metadata) > 4096 {
		writeError(w, "metadata too large", http.StatusBadRequest)
		return taskBody{}, false
	}
	if len(raw.Metadata) > 0 && !json.Valid(raw.Metadata) {
		writeError(w, "metadata must be valid JSON", http.StatusBadRequest)
		return taskBody{}, false
	}
	if raw.StartDate != "" && !isValidYearMonth(raw.StartDate) {
		writeError(w, "start_date must be YYYY-MM format", http.StatusBadRequest)
		return taskBody{}, false
	}
	if raw.EndDate != "" && !isValidYearMonth(raw.EndDate) {
		writeError(w, "end_date must be YYYY-MM format", http.StatusBadRequest)
		return taskBody{}, false
	}
	if raw.Interval == 0 {
		raw.Interval = 1
	}
	if !allowedIntervals[raw.Interval] {
		writeError(w, "interval must be one of: 1, 2, 3, 6, 12", http.StatusBadRequest)
		return taskBody{}, false
	}
	return taskBody{
		Title:       raw.Title,
		Description: raw.Description,
		Type:        raw.Type,
		Metadata:    raw.Metadata,
		StartDate:   raw.StartDate,
		EndDate:     raw.EndDate,
		Interval:    raw.Interval,
	}, true
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	req, ok := parseTaskBody(w, r)
	if !ok {
		return
	}
	userID := currentUser(r).UserID
	task, err := h.db.CreateTask(req.Title, req.Description, req.Type, req.StartDate, req.EndDate, req.Metadata, userID, req.Interval)
	if err != nil {
		writeServerError(w, "failed to create task", err)
		return
	}
	go h.db.InsertAuditLog(userID, "create", "task", task.ID, task.Title)
	writeJSONCreated(w, task)
}

func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if _, ok := h.taskOwnerCheck(w, id, currentUser(r).UserID); !ok {
		return
	}
	req, ok := parseTaskBody(w, r)
	if !ok {
		return
	}
	task, err := h.db.UpdateTaskWithAmountBackfill(id, req.Title, req.Description, req.Type, req.StartDate, req.EndDate, req.Metadata, req.Interval)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, "task not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeServerError(w, "failed to update task", err)
		return
	}
	go h.db.InsertAuditLog(currentUser(r).UserID, "update", "task", task.ID, task.Title)
	writeJSON(w, task)
}

func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, "invalid id", http.StatusBadRequest)
		return
	}
	if _, ok := h.taskOwnerCheck(w, id, currentUser(r).UserID); !ok {
		return
	}
	// Collect receipt filenames before deletion (cascade will remove DB rows).
	receipts, err := h.db.GetReceiptsForTask(id)
	if err != nil {
		log.Printf("GetReceiptsForTask(%d): %v", id, err)
	}
	// Delete DB record first; only remove files after the DB confirms success.
	if err = h.db.DeleteTask(id); err != nil {
		writeServerError(w, "failed to delete task", err)
		return
	}
	for _, f := range receipts {
		safeRemoveReceipt(h.receiptsDir, f)
	}
	go h.db.InsertAuditLog(currentUser(r).UserID, "delete", "task", id, "")
	w.WriteHeader(http.StatusNoContent)
}

// --- Completions ---

func (h *Handler) ListCompletions(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	if month == "" {
		writeError(w, "month query param required (YYYY-MM)", http.StatusBadRequest)
		return
	}
	if !isValidYearMonth(month) {
		writeError(w, "month must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	completions, err := h.db.GetCompletions(month, currentUser(r).UserID)
	if err != nil {
		writeServerError(w, "failed to list completions", err)
		return
	}
	writeJSON(w, completions)
}

func (h *Handler) ToggleCompletion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID int64  `json:"task_id"`
		Month  string `json:"month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.TaskID == 0 || req.Month == "" {
		writeError(w, "task_id and month are required", http.StatusBadRequest)
		return
	}
	if !isValidYearMonth(req.Month) {
		writeError(w, "month must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	task, ok := h.taskOwnerCheck(w, req.TaskID, currentUser(r).UserID)
	if !ok {
		return
	}

	existing, found, err := h.db.GetCompletion(req.TaskID, req.Month)
	if err != nil {
		writeServerError(w, "failed to check completion", err)
		return
	}

	userID := currentUser(r).UserID

	if found && !existing.Skipped {
		// Was completed → uncomplete.
		receiptFile := existing.ReceiptFile
		if err := h.db.RemoveCompletion(req.TaskID, req.Month); err != nil {
			writeServerError(w, "failed to remove completion", err)
			return
		}
		safeRemoveReceipt(h.receiptsDir, receiptFile)
		writeJSON(w, map[string]bool{"completed": false})
		go FireWebhooks(h.db, userID, "task.uncompleted", task.ID, task.Title, req.Month)
		go h.db.InsertAuditLog(userID, "uncomplete", "completion", task.ID, task.Title)
	} else if found && existing.Skipped {
		// Was skipped → mark as completed.
		if _, err := h.db.CompleteSkipped(req.TaskID, req.Month); err != nil {
			writeServerError(w, "failed to complete task", err)
			return
		}
		writeJSON(w, map[string]bool{"completed": true})
		go FireWebhooks(h.db, userID, "task.completed", task.ID, task.Title, req.Month)
		go h.db.InsertAuditLog(userID, "complete", "completion", task.ID, task.Title)
	} else {
		// Pending → complete.
		if _, err := h.db.AddCompletion(req.TaskID, req.Month); err != nil {
			writeServerError(w, "failed to add completion", err)
			return
		}
		writeJSON(w, map[string]bool{"completed": true})
		go FireWebhooks(h.db, userID, "task.completed", task.ID, task.Title, req.Month)
		go h.db.InsertAuditLog(userID, "complete", "completion", task.ID, task.Title)
	}
}

func (h *Handler) PatchCompletion(w http.ResponseWriter, r *http.Request) {
	taskID, err := strconv.ParseInt(chi.URLParam(r, "task_id"), 10, 64)
	if err != nil {
		writeError(w, "invalid task_id", http.StatusBadRequest)
		return
	}
	month := chi.URLParam(r, "month")
	if !isValidYearMonth(month) {
		writeError(w, "month must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if _, ok := h.taskOwnerCheck(w, taskID, currentUser(r).UserID); !ok {
		return
	}

	var req struct {
		Amount *string `json:"amount"`
		Note   *string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Amount == nil && req.Note == nil {
		writeError(w, "amount or note required", http.StatusBadRequest)
		return
	}

	existing, found, err := h.db.GetCompletion(taskID, month)
	if err != nil {
		writeServerError(w, "failed to get completion", err)
		return
	}
	if !found {
		writeError(w, "task not marked as done for this month", http.StatusBadRequest)
		return
	}
	if existing.Skipped {
		writeError(w, "task is marked as skipped for this month", http.StatusBadRequest)
		return
	}
	completion := existing

	if req.Amount != nil {
		if *req.Amount != "" {
			if v, parseErr := strconv.ParseFloat(*req.Amount, 64); parseErr != nil || v < 0 {
				writeError(w, "amount must be a non-negative number", http.StatusBadRequest)
				return
			}
		}
		completion, err = h.db.SetCompletionAmount(taskID, month, *req.Amount)
		if err != nil {
			writeServerError(w, "failed to update completion amount", err)
			return
		}
	}

	if req.Note != nil {
		if len(*req.Note) > 1000 {
			writeError(w, "note must be 1000 characters or fewer", http.StatusBadRequest)
			return
		}
		completion, err = h.db.SetCompletionNote(taskID, month, *req.Note)
		if err != nil {
			writeServerError(w, "failed to update completion note", err)
			return
		}
	}

	writeJSON(w, completion)
}

func (h *Handler) UploadCompletionReceipt(w http.ResponseWriter, r *http.Request) {
	taskID, err := strconv.ParseInt(chi.URLParam(r, "task_id"), 10, 64)
	if err != nil {
		writeError(w, "invalid task_id", http.StatusBadRequest)
		return
	}
	month := chi.URLParam(r, "month")
	if !isValidYearMonth(month) {
		writeError(w, "month must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if _, ok := h.taskOwnerCheck(w, taskID, currentUser(r).UserID); !ok {
		return
	}

	existing, found, err := h.db.GetCompletion(taskID, month)
	if err != nil {
		writeServerError(w, "failed to check completion", err)
		return
	}
	if !found {
		writeError(w, "task not marked as done for this month", http.StatusBadRequest)
		return
	}
	if existing.Skipped {
		writeError(w, "task is marked as skipped for this month", http.StatusBadRequest)
		return
	}

	const maxBytes = 10 << 20 // 10 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, "file too large (max 10 MB)", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, "file field required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		writeError(w, "unsupported file type (pdf, jpg, png, webp, gif)", http.StatusBadRequest)
		return
	}

	// Validate actual file content via magic bytes — prevents extension spoofing.
	sniffBuf := make([]byte, 512)
	n, _ := file.Read(sniffBuf)
	detectedMIME := http.DetectContentType(sniffBuf[:n])
	if !isAllowedMIME(detectedMIME) {
		writeError(w, "file content does not match an allowed type", http.StatusBadRequest)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeServerError(w, "failed to process file", err)
		return
	}

	filename := uuid.New().String() + ext
	dst, err := os.Create(filepath.Join(h.receiptsDir, filename))
	if err != nil {
		writeServerError(w, "failed to save file", err)
		return
	}
	// Close dst explicitly — never rely on defer when os.Remove must follow immediately.
	_, copyErr := io.Copy(dst, file)
	closeErr := dst.Close()
	if copyErr != nil {
		os.Remove(filepath.Join(h.receiptsDir, filename))
		writeServerError(w, "failed to save file", copyErr)
		return
	}
	if closeErr != nil {
		os.Remove(filepath.Join(h.receiptsDir, filename))
		writeServerError(w, "failed to flush file", closeErr)
		return
	}

	// Update DB first; only remove old file after the DB confirms success.
	oldReceipt := existing.ReceiptFile
	completion, err := h.db.SetCompletionReceipt(taskID, month, filename)
	if err != nil {
		os.Remove(filepath.Join(h.receiptsDir, filename))
		writeServerError(w, "failed to update completion", err)
		return
	}
	safeRemoveReceipt(h.receiptsDir, oldReceipt)
	writeJSON(w, completion)
}

func (h *Handler) DeleteCompletionReceipt(w http.ResponseWriter, r *http.Request) {
	taskID, err := strconv.ParseInt(chi.URLParam(r, "task_id"), 10, 64)
	if err != nil {
		writeError(w, "invalid task_id", http.StatusBadRequest)
		return
	}
	month := chi.URLParam(r, "month")
	if !isValidYearMonth(month) {
		writeError(w, "month must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if _, ok := h.taskOwnerCheck(w, taskID, currentUser(r).UserID); !ok {
		return
	}

	existing, found, err := h.db.GetCompletion(taskID, month)
	if err != nil {
		writeServerError(w, "failed to check completion", err)
		return
	}
	if !found {
		writeError(w, "task not marked as done for this month", http.StatusBadRequest)
		return
	}
	if existing.ReceiptFile == "" {
		writeError(w, "no receipt attached", http.StatusBadRequest)
		return
	}

	// Clear DB record first; only remove file after the DB confirms success.
	receiptFile := existing.ReceiptFile
	completion, err := h.db.ClearCompletionReceipt(taskID, month)
	if err != nil {
		writeServerError(w, "failed to update completion", err)
		return
	}
	safeRemoveReceipt(h.receiptsDir, receiptFile)
	writeJSON(w, completion)
}

func (h *Handler) SkipCompletion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID int64  `json:"task_id"`
		Month  string `json:"month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.TaskID == 0 || req.Month == "" {
		writeError(w, "task_id and month are required", http.StatusBadRequest)
		return
	}
	if !isValidYearMonth(req.Month) {
		writeError(w, "month must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	task, ok := h.taskOwnerCheck(w, req.TaskID, currentUser(r).UserID)
	if !ok {
		return
	}

	userID := currentUser(r).UserID
	completion, nowSkipped, err := h.db.SkipCompletion(req.TaskID, req.Month)
	if err != nil {
		if err.Error() == "task is already completed" {
			writeError(w, "task is already completed, unmark it first", http.StatusBadRequest)
			return
		}
		writeServerError(w, "failed to toggle skip", err)
		return
	}
	if nowSkipped {
		writeJSON(w, map[string]any{"skipped": true, "completion": completion})
		go h.db.InsertAuditLog(userID, "skip", "completion", task.ID, task.Title)
	} else {
		writeJSON(w, map[string]any{"skipped": false})
		go h.db.InsertAuditLog(userID, "unskip", "completion", task.ID, task.Title)
	}
}

func (h *Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit, offset := 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	logs, total, err := h.db.GetAuditLogs(limit, offset)
	if err != nil {
		writeServerError(w, "failed to list audit logs", err)
		return
	}
	writeJSON(w, map[string]any{"logs": logs, "total": total})
}

func (h *Handler) ServeReceipt(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if !isValidReceiptFilename(filename) {
		writeError(w, "invalid filename", http.StatusBadRequest)
		return
	}
	// Verify the receipt belongs to a completion owned by the requesting user.
	ok, err := h.db.ReceiptBelongsToUser(filename, currentUser(r).UserID)
	if err != nil {
		writeServerError(w, "failed to verify access", err)
		return
	}
	if !ok {
		writeError(w, "not found", http.StatusNotFound)
		return
	}
	ext := filepath.Ext(filename)
	w.Header().Set("Content-Disposition", `attachment; filename="receipt`+ext+`"`)
	http.ServeFile(w, r, filepath.Join(h.receiptsDir, filename))
}

// ExportCSV streams all completions for the user in the requested month range as a CSV file.
// Query params:
//
//	from  YYYY-MM  first month inclusive (defaults to January of the current year)
//	to    YYYY-MM  last month inclusive  (defaults to current month)
func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	defaultFrom := now.Format("2006") + "-01"
	defaultTo := now.Format("2006-01")

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	if from == "" {
		from = defaultFrom
	}
	if to == "" {
		to = defaultTo
	}
	if !isValidYearMonth(from) {
		writeError(w, "from must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if !isValidYearMonth(to) {
		writeError(w, "to must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if from > to {
		writeError(w, "from must not be after to", http.StatusBadRequest)
		return
	}

	rows, err := h.db.GetCompletionsForExport(currentUser(r).UserID, from, to)
	if err != nil {
		writeServerError(w, "failed to export", err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="montly-export-`+from+`-`+to+`.csv"`)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Title", "Type", "Month", "Status", "Amount", "Has Receipt"})
	for _, row := range rows {
		hasReceipt := "no"
		if row.HasReceipt {
			hasReceipt = "yes"
		}
		_ = cw.Write([]string{row.Title, row.Type, row.Month, row.Status, row.Amount, hasReceipt})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		log.Printf("ExportCSV flush: %v", err)
	}
}

// ImportCSV accepts a multipart upload of a CSV file that matches the export
// format (Title,Type,Month,Status,Amount,Has Receipt) and upserts completions.
// The "Has Receipt" column is accepted but ignored — receipts cannot be
// imported as binary attachments via CSV.
//
// All rows are validated before any DB writes; the entire import is atomic.
// Returns {"tasks_created":N,"completions_created":N,"completions_updated":N}.
func (h *Handler) ImportCSV(w http.ResponseWriter, r *http.Request) {
	const maxBytes = 1 << 20 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, "file too large (max 1 MB)", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, "file field required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	cr := csv.NewReader(file)
	headerRow, err := cr.Read()
	if err != nil {
		writeError(w, "could not read CSV header", http.StatusBadRequest)
		return
	}
	expected := []string{"Title", "Type", "Month", "Status", "Amount", "Has Receipt"}
	if len(headerRow) != len(expected) {
		writeError(w, "unexpected CSV header: must match export format", http.StatusBadRequest)
		return
	}
	for i, col := range expected {
		if headerRow[i] != col {
			writeError(w, "unexpected CSV header: column "+strconv.Itoa(i+1)+" must be "+col, http.StatusBadRequest)
			return
		}
	}

	validTypes := map[string]bool{
		"payment":      true,
		"subscription": true,
		"bill":         true,
		"reminder":     true,
	}

	var rows []ImportRow
	lineNum := 1
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		lineNum++
		if err != nil {
			writeError(w, "line "+strconv.Itoa(lineNum)+": parse error: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(rec) != 6 {
			writeError(w, "line "+strconv.Itoa(lineNum)+": expected 6 columns", http.StatusBadRequest)
			return
		}
		title, taskType, month, status, amount := rec[0], rec[1], rec[2], rec[3], rec[4]
		if title == "" {
			writeError(w, "line "+strconv.Itoa(lineNum)+": title must not be empty", http.StatusBadRequest)
			return
		}
		if !validTypes[taskType] {
			writeError(w, "line "+strconv.Itoa(lineNum)+": unknown task type "+taskType, http.StatusBadRequest)
			return
		}
		if !isValidYearMonth(month) {
			writeError(w, "line "+strconv.Itoa(lineNum)+": invalid month "+month, http.StatusBadRequest)
			return
		}
		if status != "completed" && status != "skipped" {
			writeError(w, "line "+strconv.Itoa(lineNum)+": status must be 'completed' or 'skipped'", http.StatusBadRequest)
			return
		}
		rows = append(rows, ImportRow{
			Title:  title,
			Type:   taskType,
			Month:  month,
			Status: status,
			Amount: amount,
		})
	}

	if len(rows) == 0 {
		writeError(w, "CSV contains no data rows", http.StatusBadRequest)
		return
	}

	result, err := h.db.ImportCompletionsCSV(currentUser(r).UserID, rows)
	if err != nil {
		writeServerError(w, "import failed", err)
		return
	}
	writeJSON(w, result)
}
