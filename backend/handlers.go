package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	db          *DB
	receiptsDir string
}

// receiptFilenameRe enforces that stored/served filenames are always UUID-based.
var receiptFilenameRe = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(pdf|txt|jpg|jpeg|png|webp|gif)$`,
)

var allowedExtensions = map[string]bool{
	".pdf":  true,
	".txt":  true,
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
	".gif":  true,
}

// allowedMIMEPrefixes are the content-sniffed types we accept.
// Using prefix match so "text/plain; charset=utf-8" matches "text/plain".
var allowedMIMEPrefixes = []string{
	"image/jpeg",
	"image/png",
	"image/gif",
	"image/webp",
	"application/pdf",
	"text/plain",
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

// taskOwnerCheck loads the task and returns it only if it belongs to userID.
// Writes the appropriate error and returns false on failure.
func (h *Handler) taskOwnerCheck(w http.ResponseWriter, taskID, userID int64) (Task, bool) {
	task, err := h.db.GetTaskByID(taskID)
	if err == sql.ErrNoRows {
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

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string          `json:"title"`
		Description string          `json:"description"`
		Type        string          `json:"type"`
		Metadata    json.RawMessage `json:"metadata"`
		StartDate   string          `json:"start_date"`
		EndDate     string          `json:"end_date"`
		Interval    int             `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		writeError(w, "title is required", http.StatusBadRequest)
		return
	}
	if len(req.Title) > 200 {
		writeError(w, "title must be 200 characters or fewer", http.StatusBadRequest)
		return
	}
	if len(req.Description) > 5000 {
		writeError(w, "description must be 5000 characters or fewer", http.StatusBadRequest)
		return
	}
	if !allowedTaskTypes[req.Type] {
		writeError(w, "type must be one of: payment, subscription, bill, reminder, or empty", http.StatusBadRequest)
		return
	}
	if len(req.Metadata) > 4096 {
		writeError(w, "metadata too large", http.StatusBadRequest)
		return
	}
	if req.StartDate != "" && !isValidYearMonth(req.StartDate) {
		writeError(w, "start_date must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if req.EndDate != "" && !isValidYearMonth(req.EndDate) {
		writeError(w, "end_date must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if req.Interval == 0 {
		req.Interval = 1
	}
	if !allowedIntervals[req.Interval] {
		writeError(w, "interval must be one of: 1, 2, 3, 6, 12", http.StatusBadRequest)
		return
	}
	task, err := h.db.CreateTask(req.Title, req.Description, req.Type, req.StartDate, req.EndDate, req.Metadata, currentUser(r).UserID, req.Interval)
	if err != nil {
		writeServerError(w, "failed to create task", err)
		return
	}
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
	var req struct {
		Title       string          `json:"title"`
		Description string          `json:"description"`
		Type        string          `json:"type"`
		Metadata    json.RawMessage `json:"metadata"`
		StartDate   string          `json:"start_date"`
		EndDate     string          `json:"end_date"`
		Interval    int             `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		writeError(w, "title is required", http.StatusBadRequest)
		return
	}
	if len(req.Title) > 200 {
		writeError(w, "title must be 200 characters or fewer", http.StatusBadRequest)
		return
	}
	if len(req.Description) > 5000 {
		writeError(w, "description must be 5000 characters or fewer", http.StatusBadRequest)
		return
	}
	if !allowedTaskTypes[req.Type] {
		writeError(w, "type must be one of: payment, subscription, bill, reminder, or empty", http.StatusBadRequest)
		return
	}
	if len(req.Metadata) > 4096 {
		writeError(w, "metadata too large", http.StatusBadRequest)
		return
	}
	if req.StartDate != "" && !isValidYearMonth(req.StartDate) {
		writeError(w, "start_date must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if req.EndDate != "" && !isValidYearMonth(req.EndDate) {
		writeError(w, "end_date must be YYYY-MM format", http.StatusBadRequest)
		return
	}
	if req.Interval == 0 {
		req.Interval = 1
	}
	if !allowedIntervals[req.Interval] {
		writeError(w, "interval must be one of: 1, 2, 3, 6, 12", http.StatusBadRequest)
		return
	}
	task, err := h.db.UpdateTask(id, req.Title, req.Description, req.Type, req.StartDate, req.EndDate, req.Metadata, req.Interval)
	if err != nil {
		writeServerError(w, "failed to update task", err)
		return
	}
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
	if _, ok := h.taskOwnerCheck(w, req.TaskID, currentUser(r).UserID); !ok {
		return
	}

	existing, found, err := h.db.GetCompletion(req.TaskID, req.Month)
	if err != nil {
		writeServerError(w, "failed to check completion", err)
		return
	}

	if found {
		// Remove DB record first; delete file only after DB confirms success.
		receiptFile := existing.ReceiptFile
		if err := h.db.RemoveCompletion(req.TaskID, req.Month); err != nil {
			writeServerError(w, "failed to remove completion", err)
			return
		}
		safeRemoveReceipt(h.receiptsDir, receiptFile)
		writeJSON(w, map[string]bool{"completed": false})
	} else {
		if _, err := h.db.AddCompletion(req.TaskID, req.Month); err != nil {
			writeServerError(w, "failed to add completion", err)
			return
		}
		writeJSON(w, map[string]bool{"completed": true})
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

	_, found, err := h.db.GetCompletion(taskID, month)
	if err != nil {
		writeServerError(w, "failed to check completion", err)
		return
	}
	if !found {
		writeError(w, "task not marked as done for this month", http.StatusBadRequest)
		return
	}

	var req struct {
		Amount string `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Amount != "" {
		if v, err := strconv.ParseFloat(req.Amount, 64); err != nil || v < 0 {
			writeError(w, "amount must be a non-negative number", http.StatusBadRequest)
			return
		}
	}

	completion, err := h.db.SetCompletionAmount(taskID, month, req.Amount)
	if err != nil {
		writeServerError(w, "failed to update completion", err)
		return
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
		writeError(w, "unsupported file type (pdf, txt, jpg, png, webp, gif)", http.StatusBadRequest)
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
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(filepath.Join(h.receiptsDir, filename))
		writeServerError(w, "failed to save file", err)
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
	w.Header().Set("Content-Disposition", "attachment")
	http.ServeFile(w, r, filepath.Join(h.receiptsDir, filename))
}
