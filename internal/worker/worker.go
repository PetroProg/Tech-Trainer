package worker

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/PetroProg/tech-trainer/internal/ai"
	"github.com/PetroProg/tech-trainer/internal/config"
	"github.com/PetroProg/tech-trainer/internal/db"
	"github.com/PetroProg/tech-trainer/internal/models"
)

// Worker coordinates periodic checks, question pruning, and on-demand batch generation.
type Worker struct {
	cfg         *config.Config
	database    *db.DB
	aiClient    ai.AIClient
	triggerChan chan struct{}
	mu          sync.RWMutex
	status      models.OllamaStatus
}

// NewWorker initializes a new background worker instance.
func NewWorker(cfg *config.Config, database *db.DB, aiClient ai.AIClient) *Worker {
	return &Worker{
		cfg:         cfg,
		database:    database,
		aiClient:    aiClient,
		triggerChan: make(chan struct{}, 1),
		status: models.OllamaStatus{
			ModelName: aiClient.GetModelName(),
		},
	}
}

// Start begins the background check loop and scheduled tickers.
func (w *Worker) Start(ctx context.Context) {
	log.Printf("[Worker] Background worker started. AI Model: %s (Provider: %s), Check interval: %v, Threshold: < %d, Target pool: %d/category",
		w.aiClient.GetModelName(), w.cfg.AIProvider, w.cfg.CheckIntervalHours, w.cfg.MinCategoryBuffer, w.cfg.BatchSize)

	ticker := time.NewTicker(w.cfg.CheckIntervalHours)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Worker] Worker stopped.")
			return
		case <-ticker.C:
			w.checkAndGenerate(ctx, "periodic_ticker")
		case <-w.triggerChan:
			w.checkAndGenerate(ctx, "manual_trigger")
		}
	}
}

// TriggerManual signals the worker to start an immediate check and refill if any category is below threshold.
func (w *Worker) TriggerManual() bool {
	select {
	case w.triggerChan <- struct{}{}:
		return true
	default:
		// Job already queued
		return false
	}
}

// GetStatus returns the current thread-safe worker status snapshot.
func (w *Worker) GetStatus() models.OllamaStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

// updateStatus mutates the worker status state.
func (w *Worker) updateStatus(online, running bool, result string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.IsOnline = online
	w.status.IsRunning = running
	w.status.LastCheck = time.Now()
	if result != "" {
		w.status.LastResult = result
	}
}

// checkAndGenerate enforces the question retention policy and triggers generation ONLY when a category has < 15 questions.
func (w *Worker) checkAndGenerate(ctx context.Context, source string) {
	w.mu.Lock()
	if w.status.IsRunning {
		w.mu.Unlock()
		log.Println("[Worker] Generation already in progress, skipping trigger")
		return
	}
	w.mu.Unlock()

	// 1. Mandatory retention cleanup:
	// A question is removed from the database ONLY IF:
	// - The user has answered it.
	// - AND at least 1 day has passed after its solution.
	pruned, err := w.database.PruneExpiredQuestions()
	if err != nil {
		log.Printf("[Worker] Error pruning expired questions: %v", err)
	} else if pruned > 0 {
		log.Printf("[Worker] Pruned %d expired questions (answered > 24 hours ago)", pruned)
	}

	// 2. Query category counts
	categories, err := w.database.GetCategories()
	if err != nil {
		log.Printf("[Worker] Failed to query categories: %v", err)
		return
	}

	// 3. Identify categories where available (unanswered) questions < 15
	var needRefill []models.Category
	for _, c := range categories {
		if c.AvailableQuestions < w.cfg.MinCategoryBuffer {
			needRefill = append(needRefill, c)
		}
	}

	// Launch AI generator ONLY when a category has < 15 questions left!
	if len(needRefill) == 0 {
		log.Printf("[Worker] All categories have >= %d questions. AI generator will not be launched. (source: %s)",
			w.cfg.MinCategoryBuffer, source)
		w.updateStatus(true, false, fmt.Sprintf("All categories healthy (>= %d questions). %s idle.", w.cfg.MinCategoryBuffer, w.aiClient.GetModelName()))
		return
	}

	// 4. Verify AI connectivity
	isOnline := w.aiClient.Ping(ctx)
	w.updateStatus(isOnline, false, "")

	if !isOnline {
		log.Printf("[Worker] Category refill needed (< %d), but AI service '%s' is offline. Will retry later.",
			w.cfg.MinCategoryBuffer, w.aiClient.GetModelName())
		w.updateStatus(false, false, fmt.Sprintf("AI service '%s' unreachable (refill pending for %d categories)", w.aiClient.GetModelName(), len(needRefill)))
		return
	}

	log.Printf("[Worker] AI model '%s' is online! %d categories have < %d questions. Refilling up to %d questions...",
		w.aiClient.GetModelName(), len(needRefill), w.cfg.MinCategoryBuffer, w.cfg.BatchSize)

	w.updateStatus(true, true, fmt.Sprintf("Refilling pool up to %d questions for %d categories via %s...", w.cfg.BatchSize, len(needRefill), w.aiClient.GetModelName()))
	w.generateCategoryBatches(ctx, needRefill)
}

// generateCategoryBatches produces questions to top up categories up to the batch size (50).
func (w *Worker) generateCategoryBatches(ctx context.Context, categories []models.Category) {
	if len(categories) == 0 {
		w.updateStatus(true, false, "No categories requiring refill")
		return
	}

	// Ensure GPU VRAM is completely freed after batch completes or aborts (no-op for Gemini)
	defer func() {
		_ = w.aiClient.UnloadModel(context.Background())
		log.Printf("[Worker] Completed batch session for AI model '%s'.", w.aiClient.GetModelName())
	}()

	targetPerCategory := w.cfg.BatchSize // 50 questions target per category
	totalGenerated := 0
	totalSaved := 0

	difficulties := []string{"junior"} // 100% Junior practical level (no middle complexity)

	// Rate limit delay: Gemini free tier limit is 15 RPM (~4s/req).
	sleepDur := 1 * time.Second
	if w.cfg.AIProvider == "gemini" {
		sleepDur = 3 * time.Second
	}

	for _, cat := range categories {
		needed := targetPerCategory - cat.AvailableQuestions
		if needed <= 0 {
			continue
		}
		if needed > targetPerCategory {
			needed = targetPerCategory
		}

		log.Printf("[Worker] Generating %d questions to refill category '%s' (current available: %d, target: %d)...",
			needed, cat.Name, cat.AvailableQuestions, targetPerCategory)
		catSaved := 0

		for i := 0; i < needed; i++ {
			select {
			case <-ctx.Done():
				w.updateStatus(true, false, "Generation canceled (shutdown)")
				return
			default:
			}

			diff := difficulties[rand.Intn(len(difficulties))]

			genCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
			dto, err := w.aiClient.GenerateQuestion(genCtx, cat.Name, cat.PromptInstructions, diff)
			cancel()

			if err != nil {
				log.Printf("[Worker] Error generating question for '%s': %v", cat.Name, err)
				if !w.aiClient.Ping(ctx) {
					log.Printf("[Worker] AI service '%s' disconnected mid-batch. Halting.", w.aiClient.GetModelName())
					w.updateStatus(false, false, fmt.Sprintf("AI service '%s' disconnected during generation", w.aiClient.GetModelName()))
					return
				}
				time.Sleep(2 * time.Second)
				continue
			}

			totalGenerated++
			saved, err := w.database.SaveGeneratedQuestion(*dto, cat.ID)
			if err != nil {
				log.Printf("[Worker] Failed to save question: %v", err)
			} else if saved {
				catSaved++
				totalSaved++
				log.Printf("[Worker] [%s %d/%d] Saved question (%s, answers=%d)",
					cat.Name, catSaved, needed, dto.Difficulty, len(dto.CorrectIndices))
			} else {
				log.Printf("[Worker] Duplicate question skipped for '%s'", cat.Name)
			}

			time.Sleep(sleepDur)
		}
	}

	resultMsg := fmt.Sprintf("Batch complete: generated %d, saved %d questions across %d categories (%s)", totalGenerated, totalSaved, len(categories), w.aiClient.GetModelName())
	log.Printf("[Worker] %s", resultMsg)
	w.updateStatus(true, false, resultMsg)
}
