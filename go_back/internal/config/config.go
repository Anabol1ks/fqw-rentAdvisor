package config

import (
	"os"
	"strconv"

	"go.uber.org/zap"
)

type Config struct {
	Port         string
	MLServiceURL string
	MLTimeoutSec int
	DSN_URL      string
}

func Load(log *zap.Logger) *Config {
	ml_timeoutStr := getEnv("ML_TIMEOUT_SEC", "100", log)
	ml_timeout, err := strconv.Atoi(ml_timeoutStr)
	if err != nil {
		log.Error("Invalid ML_TIMEOUT_SEC value, must be an integer",
			zap.String("value", ml_timeoutStr),
		)
		panic("invalid ML_TIMEOUT_SEC value")
	}
	return &Config{
		Port:         getEnv("PORT", "8080", log),
		MLServiceURL: getEnv("ML_SERVICE_URL", "http://localhost:8000", log),
		MLTimeoutSec: ml_timeout,
		DSN_URL:      getEnv("POSTGRES_DSN_URL", "", log),
	}
}

func getEnv(key, defaultVal string, log *zap.Logger) string {
	if val, exists := os.LookupEnv(key); exists {
		return val
	}
	if defaultVal != "" {
		log.Warn("Переменная окружения не установлена, используем значение по умолчанию",
			zap.String("key", key),
			zap.String("default", defaultVal),
		)
		return defaultVal
	}

	log.Error("Обязательная переменная окружения не установлена и значение по умолчанию не задано",
		zap.String("key", key),
	)
	panic("missing required environment variable: " + key)
}
