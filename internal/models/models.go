package models

import "time"

// Category represents a distinct topic category with prompt instructions.
type Category struct {
	ID                 int64     `json:"id"`
	Slug               string    `json:"slug"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	PromptInstructions string    `json:"prompt_instructions"`
	IsActive           bool      `json:"is_active"`
	AvailableQuestions int       `json:"available_questions"`
	TotalQuestions     int       `json:"total_questions"`
	SolvedQuestions    int       `json:"solved_questions"`
	CreatedAt          time.Time `json:"created_at"`
}

// Option represents one of the four choices for a question.
type Option struct {
	ID         int64  `json:"id"`
	QuestionID int64  `json:"question_id,omitempty"`
	OptionText string `json:"option_text"`
	IsCorrect  bool   `json:"is_correct"`
	OrderIndex int    `json:"order_idx"`
}

// Question represents a challenge with options and an explanation.
type Question struct {
	ID           int64     `json:"id"`
	CategoryID   int64     `json:"category_id"`
	CategorySlug string    `json:"category_slug,omitempty"`
	CategoryName string    `json:"category_name,omitempty"`
	Type         string    `json:"type"`       // "multiple_choice" or "code_analysis"
	Difficulty   string    `json:"difficulty"` // "junior" or "middle"
	QuestionText string    `json:"question_text"`
	CodeSnippet  string    `json:"code_snippet"`
	Language     string    `json:"language"`
	Explanation  string    `json:"explanation"`
	Hash         string    `json:"-"`
	Options      []Option  `json:"options"`
	IsSolved     bool      `json:"is_solved"`
	CreatedAt    time.Time `json:"created_at"`
}

// OfflineQuestion includes preloaded validation data for autonomous offline execution.
type OfflineQuestion struct {
	ID               int64     `json:"id"`
	CategoryID       int64     `json:"category_id"`
	CategorySlug     string    `json:"category_slug"`
	CategoryName     string    `json:"category_name"`
	Type             string    `json:"type"`
	Difficulty       string    `json:"difficulty"`
	QuestionText     string    `json:"question_text"`
	CodeSnippet      string    `json:"code_snippet"`
	Language         string    `json:"language"`
	Explanation      string    `json:"explanation"`
	Options          []Option  `json:"options"`
	CorrectOptionIDs []int64   `json:"correct_option_ids"`
	IsSolved         bool      `json:"is_solved"`
	CreatedAt        time.Time `json:"created_at"`
}

// OfflinePack represents all categories and questions downloaded for disconnected play.
type OfflinePack struct {
	Categories []Category        `json:"categories"`
	Questions  []OfflineQuestion `json:"questions"`
	SyncedAt   time.Time         `json:"synced_at"`
}

// SyncAnswerItem defines one answer recorded while offline.
type SyncAnswerItem struct {
	QuestionID        int64     `json:"question_id"`
	SelectedOptionIDs []int64   `json:"selected_option_ids"`
	AnsweredAt        time.Time `json:"answered_at"`
}

// SyncAnswersRequest represents a batch of answers submitted upon reconnecting online.
type SyncAnswersRequest struct {
	Answers []SyncAnswerItem `json:"answers"`
}

// SyncAnswersResponse returns updated stats and verification outcome.
type SyncAnswersResponse struct {
	SyncedCount int        `json:"synced_count"`
	TotalScore  int        `json:"total_score"`
	Streak      int        `json:"streak"`
	UserStats   *UserStats `json:"user_stats"`
}

// SubmitAnswerRequest represents the user's selected option IDs.
type SubmitAnswerRequest struct {
	SelectedOptionIDs []int64 `json:"selected_option_ids"`
}

// SubmitAnswerResponse represents the result of validating a user answer.
type SubmitAnswerResponse struct {
	IsCorrect         bool    `json:"is_correct"`
	Explanation       string  `json:"explanation"`
	CorrectOptionIDs  []int64 `json:"correct_option_ids"`
	SelectedOptionIDs []int64 `json:"selected_option_ids"`
	Streak            int     `json:"streak"`
	TotalScore        int     `json:"total_score"`
}

// UserStats represents overall user performance metrics.
type UserStats struct {
	TotalAnswered   int            `json:"total_answered"`
	TotalCorrect    int            `json:"total_correct"`
	AccuracyPercent float64        `json:"accuracy_percent"`
	CurrentStreak   int            `json:"current_streak"`
	TotalScore      int            `json:"total_score"`
	ByCategory      []CategoryStat `json:"by_category"`
}

// CategoryStat represents user metrics for a specific category.
type CategoryStat struct {
	CategoryName    string  `json:"category_name"`
	CategorySlug    string  `json:"category_slug"`
	TotalAnswered   int     `json:"total_answered"`
	TotalCorrect    int     `json:"total_correct"`
	AccuracyPercent float64 `json:"accuracy_percent"`
}

// GeneratedQuestionDTO is the structured payload expected from Ollama.
type GeneratedQuestionDTO struct {
	Type           string   `json:"type"`
	Difficulty     string   `json:"difficulty"`
	Question       string   `json:"question"`
	Code           string   `json:"code"`
	Language       string   `json:"language"`
	Options        []string `json:"options"`
	CorrectIndices []int    `json:"correct_indices"` // 0-based indices
	Explanation    string   `json:"explanation"`
}

// OllamaStatus represents the current state of the Ollama inference engine.
type OllamaStatus struct {
	IsOnline   bool      `json:"is_online"`
	ModelName  string    `json:"model_name"`
	LastCheck  time.Time `json:"last_check"`
	IsRunning  bool      `json:"is_running"`
	LastResult string    `json:"last_result"`
}
