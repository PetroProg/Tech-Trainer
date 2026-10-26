package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/PetroProg/tech-trainer/internal/db"
	"github.com/PetroProg/tech-trainer/internal/models"
	"github.com/PetroProg/tech-trainer/internal/worker"
	"github.com/go-chi/chi/v5"
)

// API encapsulates HTTP handlers for the application endpoints.
type API struct {
	database *db.DB
	worker   *worker.Worker
}

// NewAPI constructs a new API handler instance.
func NewAPI(database *db.DB, worker *worker.Worker) *API {
	return &API{
		database: database,
		worker:   worker,
	}
}

// writeJSON encodes an entity as JSON to the response writer.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError outputs a formatted error JSON object.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// GetCategories returns all active categories and their question counters.
func (a *API) GetCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := a.database.GetCategories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, categories)
}

// GetOfflinePack downloads active categories and questions for offline usage.
func (a *API) GetOfflinePack(w http.ResponseWriter, r *http.Request) {
	pack, err := a.database.GetOfflinePack()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pack)
}

// SyncAnswers processes a batch of offline answers.
func (a *API) SyncAnswers(w http.ResponseWriter, r *http.Request) {
	var req models.SyncAnswersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	resp, err := a.database.SyncAnswers(req.Answers)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateCategoryRequest defines the payload for creating a custom category.
type CreateCategoryRequest struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	PromptInstructions  string `json:"prompt_instructions"`
}

var nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a human-readable title into a URL-friendly slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphaNumRegex.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// CreateCategory adds a new category and triggers background question generation.
func (a *API) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}

	slug := slugify(name)
	if slug == "" {
		slug = "cat-" + strconv.FormatInt(strconv.IntSize, 10)
	}

	cat := models.Category{
		Slug:               slug,
		Name:               name,
		Description:        strings.TrimSpace(req.Description),
		PromptInstructions: strings.TrimSpace(req.PromptInstructions),
		IsActive:           true,
	}

	id, err := a.database.CreateCategory(cat)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Category already exists or creation failed: "+err.Error())
		return
	}
	cat.ID = id

	// Queue generation for the newly created category (it starts with 0 questions < 15)
	a.worker.TriggerManual()

	writeJSON(w, http.StatusCreated, cat)
}

// UpdateCategoryRequest defines the payload for updating an existing category.
type UpdateCategoryRequest struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	PromptInstructions  string `json:"prompt_instructions"`
}

// UpdateCategory modifies an existing category's name, description, and prompt instructions.
func (a *API) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid category ID")
		return
	}

	var req UpdateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Category name is required")
		return
	}

	prompt := strings.TrimSpace(req.PromptInstructions)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "Prompt instructions are required")
		return
	}

	err = a.database.UpdateCategory(id, name, strings.TrimSpace(req.Description), prompt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to update category: "+err.Error())
		return
	}

	updated, err := a.database.GetCategoryByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to fetch updated category: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// GetRandomQuestion retrieves a random unsolved question matching filters.
func (a *API) GetRandomQuestion(w http.ResponseWriter, r *http.Request) {
	categorySlug := r.URL.Query().Get("category")
	difficulty := r.URL.Query().Get("difficulty")

	q, err := a.database.GetRandomQuestion(categorySlug, difficulty)
	if err != nil {
		writeError(w, http.StatusNotFound, "No questions available yet. Trigger generation or wait for the worker.")
		return
	}

	// Redact is_correct flags before dispatching question to the frontend
	sanitizedOptions := make([]models.Option, len(q.Options))
	for i, opt := range q.Options {
		sanitizedOptions[i] = models.Option{
			ID:         opt.ID,
			OptionText: opt.OptionText,
			OrderIndex: opt.OrderIndex,
			IsCorrect:  false, // Obscured until answer validation
		}
	}
	q.Options = sanitizedOptions

	writeJSON(w, http.StatusOK, q)
}


// GetPrefetchQuestions returns a batch of questions for offline buffering.
func (a *API) GetPrefetchQuestions(w http.ResponseWriter, r *http.Request) {
	categorySlug := r.URL.Query().Get("category")
	difficulty := r.URL.Query().Get("difficulty")
	countStr := r.URL.Query().Get("count")
	count := 5
	if c, err := strconv.Atoi(countStr); err == nil && c > 0 && c <= 20 {
		count = c
	}

	excludeStr := r.URL.Query().Get("exclude")
	var excludeIDs []int64
	if excludeStr != "" {
		for _, part := range strings.Split(excludeStr, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil {
				excludeIDs = append(excludeIDs, id)
			}
		}
	}

	questions, err := a.database.GetPrefetchQuestions(categorySlug, difficulty, count, excludeIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, questions)
}

// SubmitAnswer validates the user's selected options and returns scoring.
func (a *API) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	questionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid question ID")
		return
	}

	var req models.SubmitAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	resp, err := a.database.SubmitAnswer(questionID, req.SelectedOptionIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// DeleteQuestion permanently deletes a question from the database.
func (a *API) DeleteQuestion(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	questionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid question ID")
		return
	}

	if err := a.database.DeleteQuestion(questionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Question deleted successfully"})
}

// GetStats returns aggregated user statistics.
func (a *API) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.database.GetUserStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ResetStats clears user answers and resets performance metrics.
func (a *API) ResetStats(w http.ResponseWriter, r *http.Request) {
	if err := a.database.ResetStats(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "User statistics and progress have been reset."})
}

// GetWorkerStatus returns the current Ollama and background worker state.
func (a *API) GetWorkerStatus(w http.ResponseWriter, r *http.Request) {
	status := a.worker.GetStatus()
	writeJSON(w, http.StatusOK, status)
}

// TriggerWorker starts an immediate check and refill if any category has < 15 questions.
func (a *API) TriggerWorker(w http.ResponseWriter, r *http.Request) {
	triggered := a.worker.TriggerManual()
	if triggered {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Check triggered. If any category has < 15 questions, Ollama will generate 50 questions."})
	} else {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "Worker is currently running or a check is already queued."})
	}
}
