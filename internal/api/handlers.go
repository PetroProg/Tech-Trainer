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

type API struct {
	database *db.DB
	worker   *worker.Worker
}

func NewAPI(database *db.DB, worker *worker.Worker) *API {
	return &API{
		database: database,
		worker:   worker,
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (a *API) GetCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := a.database.GetCategories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, categories)
}

type CreateCategoryRequest struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	PromptInstructions  string `json:"prompt_instructions"`
}

var nonAlphaNumRegex = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphaNumRegex.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func (a *API) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var req CreateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON")
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
		writeError(w, http.StatusBadRequest, "Category already exists or error: "+err.Error())
		return
	}
	cat.ID = id

	// Запускаем фоновое наполнение для новой категории
	a.worker.TriggerManual()

	writeJSON(w, http.StatusCreated, cat)
}

func (a *API) GetRandomQuestion(w http.ResponseWriter, r *http.Request) {
	categorySlug := r.URL.Query().Get("category")
	difficulty := r.URL.Query().Get("difficulty")

	q, err := a.database.GetRandomQuestion(categorySlug, difficulty)
	if err != nil {
		writeError(w, http.StatusNotFound, "Задач пока нет в базе. Запустите генерацию или дождитесь воркера.")
		return
	}

	// Скрываем is_correct у вариантов ответов перед отправкой клиенту!
	sanitizedOptions := make([]models.Option, len(q.Options))
	for i, opt := range q.Options {
		sanitizedOptions[i] = models.Option{
			ID:         opt.ID,
			OptionText: opt.OptionText,
			OrderIndex: opt.OrderIndex,
			IsCorrect:  false, // Скрыто до отправки ответа
		}
	}
	q.Options = sanitizedOptions

	writeJSON(w, http.StatusOK, q)
}

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

	writeJSON(w, http.StatusOK, map[string]string{"message": "Question deleted"})
}

func (a *API) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.database.GetUserStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) GetWorkerStatus(w http.ResponseWriter, r *http.Request) {
	status := a.worker.GetStatus()
	writeJSON(w, http.StatusOK, status)
}

func (a *API) TriggerWorker(w http.ResponseWriter, r *http.Request) {
	triggered := a.worker.TriggerManual()
	if triggered {
		writeJSON(w, http.StatusOK, map[string]string{"message": "Генерация запущена"})
	} else {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "Генерация уже выполняется или стоит в очереди"})
	}
}
