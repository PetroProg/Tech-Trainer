package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/PetroProg/tech-trainer/internal/ai"
	"github.com/PetroProg/tech-trainer/internal/api"
	"github.com/PetroProg/tech-trainer/internal/config"
	"github.com/PetroProg/tech-trainer/internal/db"
	"github.com/PetroProg/tech-trainer/internal/gemini"
	"github.com/PetroProg/tech-trainer/internal/ollama"
	"github.com/PetroProg/tech-trainer/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	log.Println("[Main] Starting Tech Trainer service...")
	cfg := config.Load()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("[Main] Failed to open database: %v", err)
	}
	defer database.Close()
	log.Printf("[Main] Database opened at %s", cfg.DBPath)

	var aiClient ai.AIClient
	if cfg.AIProvider == "ollama" {
		log.Printf("[Main] Initializing Ollama provider at %s (model: %s)", cfg.OllamaURL, cfg.OllamaModel)
		aiClient = ollama.NewClient(cfg.OllamaURL, cfg.OllamaModel)
	} else {
		log.Printf("[Main] Initializing Google Gemini provider (model: %s)", cfg.GeminiModel)
		aiClient = gemini.NewClient(cfg.GeminiBaseURL, cfg.GeminiAPIKey, cfg.GeminiModel)
	}

	bgWorker := worker.NewWorker(cfg, database, aiClient)

	// Context for graceful shutdown and background worker cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go bgWorker.Start(ctx)

	apiHandler := api.NewAPI(database, bgWorker)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// API Routes
	r.Route("/api", func(r chi.Router) {
		r.Get("/categories", apiHandler.GetCategories)
		r.Post("/categories", apiHandler.CreateCategory)
		r.Put("/categories/{id}", apiHandler.UpdateCategory)
		r.Get("/questions/random", apiHandler.GetRandomQuestion)
		r.Get("/questions/prefetch", apiHandler.GetPrefetchQuestions)
		r.Get("/questions/offline-pack", apiHandler.GetOfflinePack)
		r.Post("/questions/{id}/answer", apiHandler.SubmitAnswer)
		r.Post("/sync/answers", apiHandler.SyncAnswers)
		r.Delete("/questions/{id}", apiHandler.DeleteQuestion)
		r.Get("/stats", apiHandler.GetStats)
		r.Post("/stats/reset", apiHandler.ResetStats)
		r.Get("/worker/status", apiHandler.GetWorkerStatus)
		r.Post("/worker/trigger", apiHandler.TriggerWorker)
	})

	// Static assets serving with SPA Fallback
	staticDir := "frontend/dist"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		staticDir = "dist" // fallback path
	}

	workDir, _ := os.Getwd()
	filesDir := http.Dir(filepath.Join(workDir, staticDir))

	fileServer(r, "/", filesDir)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown listener
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("[Main] Shutting down gracefully...")

		cancel() // signals worker to stop

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("[Main] HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("[Main] Server listening on http://0.0.0.0:%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("[Main] Server error: %v", err)
	}

	log.Println("[Main] Server exited cleanly.")
}

// fileServer sets up a route for static file serving with Single Page Application (SPA) index fallback.
func fileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit any URL parameters.")
	}

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", 301).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		fsPath := strings.TrimPrefix(r.URL.Path, pathPrefix)

		f, err := root.Open(fsPath)
		if err != nil {
			// SPA fallback: return index.html
			indexFile, errIndex := root.Open("index.html")
			if errIndex != nil {
				http.NotFound(w, r)
				return
			}
			defer indexFile.Close()
			stat, _ := indexFile.Stat()
			http.ServeContent(w, r, "index.html", stat.ModTime(), indexFile)
			return
		}
		defer f.Close()

		stat, err := f.Stat()
		if err != nil || stat.IsDir() {
			indexFile, errIndex := root.Open("index.html")
			if errIndex != nil {
				http.NotFound(w, r)
				return
			}
			defer indexFile.Close()
			indexStat, _ := indexFile.Stat()
			http.ServeContent(w, r, "index.html", indexStat.ModTime(), indexFile)
			return
		}

		http.FileServer(root).ServeHTTP(w, r)
	})
}
