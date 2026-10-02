package models

import "time"

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

type Option struct {
	ID         int64  `json:"id"`
	QuestionID int64  `json:"question_id,omitempty"`
	OptionText string `json:"option_text"`
	IsCorrect  bool   `json:"is_correct"`
	OrderIndex int    `json:"order_idx"`
}

type Question struct {
	ID           int64     `json:"id"`
	CategoryID   int64     `json:"category_id"`
	CategorySlug string    `json:"category_slug,omitempty"`
	CategoryName string    `json:"category_name,omitempty"`
	Type         string    `json:"type"` // "multiple_choice" or "code_analysis"
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

type SubmitAnswerRequest struct {
	SelectedOptionIDs []int64 `json:"selected_option_ids"`
}

type SubmitAnswerResponse struct {
	IsCorrect         bool    `json:"is_correct"`
	Explanation       string  `json:"explanation"`
	CorrectOptionIDs  []int64 `json:"correct_option_ids"`
	SelectedOptionIDs []int64 `json:"selected_option_ids"`
	Streak            int     `json:"streak"`
	TotalScore        int     `json:"total_score"`
}

type UserStats struct {
	TotalAnswered   int            `json:"total_answered"`
	TotalCorrect    int            `json:"total_correct"`
	AccuracyPercent float64        `json:"accuracy_percent"`
	CurrentStreak   int            `json:"current_streak"`
	TotalScore      int            `json:"total_score"`
	ByCategory      []CategoryStat `json:"by_category"`
}

type CategoryStat struct {
	CategoryName    string  `json:"category_name"`
	CategorySlug    string  `json:"category_slug"`
	TotalAnswered   int     `json:"total_answered"`
	TotalCorrect    int     `json:"total_correct"`
	AccuracyPercent float64 `json:"accuracy_percent"`
}

type GeneratedQuestionDTO struct {
	Type           string   `json:"type"`
	Difficulty     string   `json:"difficulty"`
	Question       string   `json:"question"`
	Code           string   `json:"code"`
	Language       string   `json:"language"`
	Options        []string `json:"options"`
	CorrectIndices []int    `json:"correct_indices"` // 0-based
	Explanation    string   `json:"explanation"`
}

type OllamaStatus struct {
	IsOnline   bool      `json:"is_online"`
	ModelName  string    `json:"model_name"`
	LastCheck  time.Time `json:"last_check"`
	IsRunning  bool      `json:"is_running"`
	LastResult string    `json:"last_result"`
}
