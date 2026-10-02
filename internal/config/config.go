package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port               string
	DBPath             string
	OllamaURL          string
	OllamaModel        string
	CheckIntervalHours time.Duration
	MinCategoryBuffer  int
	BatchSize          int
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func Load() *Config {
	hours := getEnvInt("CHECK_INTERVAL_HOURS", 4)
	return &Config{
		Port:               getEnv("PORT", "8080"),
		DBPath:             getEnv("DB_PATH", "data/trainer.db"),
		OllamaURL:          getEnv("OLLAMA_URL", "http://100.114.251.81:11434"),
		OllamaModel:        getEnv("OLLAMA_MODEL", "qwen2.5:7b"),
		CheckIntervalHours: time.Duration(hours) * time.Hour,
		MinCategoryBuffer:  getEnvInt("MIN_CATEGORY_BUFFER", 15),
		BatchSize:          getEnvInt("BATCH_SIZE", 50),
	}
}
