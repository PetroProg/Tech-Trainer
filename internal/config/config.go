package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime configuration values for the application.
type Config struct {
	Port               string
	DBPath             string
	AIProvider         string
	GeminiAPIKey       string
	GeminiModel        string
	GeminiBaseURL      string
	OllamaURL          string
	OllamaModel        string
	CheckIntervalHours time.Duration
	MinCategoryBuffer  int
	BatchSize          int
}

// getEnv retrieves an environment variable or returns a fallback value.
func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

// getEnvInt retrieves an environment variable as an integer or returns a fallback value.
func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

// loadDotEnv loads variables from a .env file into the environment if not already set.
func loadDotEnv(filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
				v = v[1 : len(v)-1]
			}
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
	}
}

// Load populates the Config struct from environment variables.
func Load() *Config {
	loadDotEnv(".env")

	hours := getEnvInt("CHECK_INTERVAL_HOURS", 4)
	return &Config{
		Port:               getEnv("PORT", "8080"),
		DBPath:             getEnv("DB_PATH", "data/trainer.db"),
		AIProvider:         strings.ToLower(getEnv("AI_PROVIDER", "gemini")),
		GeminiAPIKey:       getEnv("GEMINI_API_KEY", ""),
		GeminiModel:        getEnv("GEMINI_MODEL", "gemini-3.5-flash-lite"),
		GeminiBaseURL:      getEnv("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
		OllamaURL:          getEnv("OLLAMA_URL", "http://100.114.251.81:11434"),
		OllamaModel:        getEnv("OLLAMA_MODEL", "qwen2.5:7b"),
		CheckIntervalHours: time.Duration(hours) * time.Hour,
		MinCategoryBuffer:  getEnvInt("MIN_CATEGORY_BUFFER", 15),
		BatchSize:          getEnvInt("BATCH_SIZE", 50),
	}
}
