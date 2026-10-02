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

type Worker struct {
	cfg          *config.Config
	database     *db.DB
	ollamaClient *ollama.Client
	triggerChan  chan struct{}
	mu           sync.RWMutex
	status       models.OllamaStatus
}

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

func (w *Worker) Start(ctx context.Context) {
	log.Printf("[Worker] Starting background worker. Check interval: %v", w.cfg.CheckIntervalHours)

	// Запуск первой проверки через 5 секунд после старта
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

func (w *Worker) TriggerManual() bool {
	select {
	case w.triggerChan <- struct{}{}:
		return true
	default:
		// Уже стоит в очереди
		return false
	}
}

func (w *Worker) GetStatus() models.OllamaStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

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
		w.updateStatus(false, false, fmt.Sprintf("Ollama недоступен на %s", w.cfg.OllamaURL))
		return
	}

	log.Printf("[Worker] Ollama is online! Checking category buffers (source: %s)...", source)

	categories, err := w.database.GetCategories()
	if err != nil {
		log.Printf("[Worker] Failed to get categories: %v", err)
		return
	}

	// Поиск категорий с дефицитом (< MinCategoryBuffer)
	var needRefill []models.Category
	for _, c := range categories {
		if c.AvailableQuestions < w.cfg.MinCategoryBuffer {
			needRefill = append(needRefill, c)
		}
	}

	if len(needRefill) == 0 && source == "periodic_ticker" {
		log.Printf("[Worker] All categories have >= %d questions. No refill needed.", w.cfg.MinCategoryBuffer)
		w.updateStatus(true, false, "Запас задач в норме (все категории заполнены)")
		return
	}

	// Если все полны, но был ручной триггер, генерируем для всех по очереди
	targets := needRefill
	if len(targets) == 0 {
		targets = categories
	}

	w.updateStatus(true, true, fmt.Sprintf("Генерация пачки задач (цель: %d)...", w.cfg.BatchSize))
	w.generateBatch(ctx, targets)
}

func (w *Worker) generateBatch(ctx context.Context, categories []models.Category) {
	if len(categories) == 0 {
		w.updateStatus(true, false, "Нет активных категорий")
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
			w.updateStatus(true, false, "Генерация прервана (shutdown)")
			return
		default:
		}

		// Выбираем категорию по очереди
		cat := categories[i%len(categories)]
		diff := difficulties[rand.Intn(len(difficulties))]

		// Запрос к Ollama с таймаутом на одну задачу
		genCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
		dto, err := w.ollamaClient.GenerateQuestion(genCtx, cat.Name, cat.PromptInstructions, diff)
		cancel()

		if err != nil {
			log.Printf("[Worker] Error generating question for '%s': %v", cat.Name, err)
			// Если Ollama перестал отвечать — прерываем пачку
			if !w.ollamaClient.Ping(ctx) {
				log.Println("[Worker] Ollama disconnected mid-batch. Halting.")
				w.updateStatus(false, false, "Ollama отключился во время генерации")
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

		// Пауза 1 секунда между задачами, чтобы не перегружать ПК
		time.Sleep(1 * time.Second)
	}

	resultMsg := fmt.Sprintf("Успешно: сгенерировано %d, сохранено %d новых задач", generatedCount, savedCount)
	log.Printf("[Worker] Batch finished. %s", resultMsg)
	w.updateStatus(true, false, resultMsg)
}
