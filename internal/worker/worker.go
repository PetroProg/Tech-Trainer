package worker

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/PetroProg/tech-trainer/internal/config"
	"github.com/PetroProg/tech-trainer/internal/db"
	"github.com/PetroProg/tech-trainer/internal/models"
	"github.com/PetroProg/tech-trainer/internal/ollama"
)

// Worker coordinates periodic health checks and on-demand batch question generation.
type Worker struct {
	cfg          *config.Config
	database     *db.DB
	ollamaClient *ollama.Client
	triggerChan  chan struct{}
	mu           sync.RWMutex
	status       models.OllamaStatus
}

// NewWorker initializes a new background worker instance.
func NewWorker(cfg *config.Config, database *db.DB, ollamaClient *ollama.Client) *Worker {
	return &Worker{
		cfg:          cfg,
		database:     database,
		ollamaClient: ollamaClient,
		triggerChan:  make(chan struct{}, 1),
		status: models.OllamaStatus{
			ModelName: cfg.OllamaModel,
		},
	}
}

// Start begins the background check loop and scheduled tickers.
func (w *Worker) Start(ctx context.Context) {
	log.Printf("[Worker] Starting background worker. Check interval: %v", w.cfg.CheckIntervalHours)

	// Execute initial check 5 seconds after launch
	go func() {
		select {
		case <-time.After(5 * time.Second):
			w.checkAndGenerate(ctx, "startup")
		case <-ctx.Done():
			return
		}
	}()

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

// TriggerManual signals the worker to start an immediate check and generation run.
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

// checkAndGenerate verifies Ollama availability and initiates batch generation when needed.
func (w *Worker) checkAndGenerate(ctx context.Context, source string) {
	w.mu.Lock()
	if w.status.IsRunning {
		w.mu.Unlock()
		log.Println("[Worker] Generation already in progress, skipping trigger")
		return
	}
	w.mu.Unlock()

	isOnline := w.ollamaClient.Ping(ctx)
	w.updateStatus(isOnline, false, "")

	if !isOnline {
		log.Printf("[Worker] Ollama is offline (%s). Will retry later.", w.cfg.OllamaURL)
		w.updateStatus(false, false, fmt.Sprintf("Ollama unreachable at %s", w.cfg.OllamaURL))
		return
	}

	log.Printf("[Worker] Ollama is online! Checking category buffers (source: %s)...", source)

	categories, err := w.database.GetCategories()
	if err != nil {
		log.Printf("[Worker] Failed to query categories: %v", err)
		return
	}

	// Identify categories requiring refill (< MinCategoryBuffer)
	var needRefill []models.Category
	for _, c := range categories {
		if c.AvailableQuestions < w.cfg.MinCategoryBuffer {
			needRefill = append(needRefill, c)
		}
	}

	if len(needRefill) == 0 && source == "periodic_ticker" {
		log.Printf("[Worker] All categories have >= %d questions. No refill needed.", w.cfg.MinCategoryBuffer)
		w.updateStatus(true, false, "Question buffer healthy across all categories")
		return
	}

	// If triggered manually and all categories are above threshold, distribute evenly across all
	targets := needRefill
	if len(targets) == 0 {
		targets = categories
	}

	w.updateStatus(true, true, fmt.Sprintf("Generating batch (target: %d)...", w.cfg.BatchSize))
	w.generateBatch(ctx, targets)
}

// generateBatch produces questions in round-robin fashion across target categories.
func (w *Worker) generateBatch(ctx context.Context, categories []models.Category) {
	if len(categories) == 0 {
		w.updateStatus(true, false, "No active categories available")
		return
	}

	totalToGenerate := w.cfg.BatchSize
	generatedCount := 0
	savedCount := 0

	difficulties := []string{"junior", "junior", "middle"} // 2/3 Junior, 1/3 Middle

	log.Printf("[Worker] Starting batch generation: target=%d questions across %d categories", totalToGenerate, len(categories))

	for i := 0; i < totalToGenerate; i++ {
		select {
		case <-ctx.Done():
			w.updateStatus(true, false, "Generation canceled (shutdown)")
			return
		default:
		}

		cat := categories[i%len(categories)]
		diff := difficulties[rand.Intn(len(difficulties))]

		genCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		dto, err := w.ollamaClient.GenerateQuestion(genCtx, cat.Name, cat.PromptInstructions, diff)
		cancel()

		if err != nil {
			log.Printf("[Worker] Error generating question for '%s': %v", cat.Name, err)
			if !w.ollamaClient.Ping(ctx) {
				log.Println("[Worker] Ollama disconnected mid-batch. Halting.")
				w.updateStatus(false, false, "Ollama disconnected during generation")
				return
			}
			time.Sleep(2 * time.Second)
			continue
		}

		generatedCount++
		saved, err := w.database.SaveGeneratedQuestion(*dto, cat.ID)
		if err != nil {
			log.Printf("[Worker] Failed to save question: %v", err)
		} else if saved {
			savedCount++
			log.Printf("[Worker] [%d/%d] Saved question for '%s' (%s, answers=%d)", 
				savedCount, totalToGenerate, cat.Name, dto.Difficulty, len(dto.CorrectIndices))
		} else {
			log.Printf("[Worker] Duplicate question skipped for '%s'", cat.Name)
		}

		// 1-second pause to prevent GPU thermal spikes
		time.Sleep(1 * time.Second)
	}

	resultMsg := fmt.Sprintf("Success: generated %d, saved %d new unique questions", generatedCount, savedCount)
	log.Printf("[Worker] Batch finished. %s", resultMsg)
	w.updateStatus(true, false, resultMsg)
}
