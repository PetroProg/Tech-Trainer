package ai

import (
	"context"

	"github.com/PetroProg/tech-trainer/internal/models"
)

// AIClient defines the unified interface for LLM question generation backends.
type AIClient interface {
	GenerateQuestion(ctx context.Context, categoryName, promptInstructions, difficulty string) (*models.GeneratedQuestionDTO, error)
	Ping(ctx context.Context) bool
	UnloadModel(ctx context.Context) error
	GetModelName() string
}
