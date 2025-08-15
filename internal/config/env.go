package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	MongoURI      string
	DBName        string
	JWTSecret     string
	JWTExpiresIn  string
	GeminiAPIKey  string
	GeminiAPIURL  string
	Port          string
	GinMode       string
	CORSOrigins   []string
	MaxFileSize   int64
	AllowedTypes  []string
	DefaultTokenLimit int
	TokenRefillRate   int
	BcryptCost       int
	RateLimitReqs    int
	RateLimitWindow  int
	MaxChunkSize     int
	ChunkOverlap     int
}

func LoadConfig() (*Config, error) {
	// Load .env file if it exists
	if _, err := os.Stat(".env"); err == nil {
		err := godotenv.Load()
		if err != nil {
			return nil, fmt.Errorf("error loading .env file: %v", err)
		}
	}

	cfg := &Config{
		MongoURI:      getEnv("MONGO_URI", "mongodb://localhost:27017/saas_chatbot"),
		DBName:        getEnv("DB_NAME", "saas_chatbot"),
		JWTSecret:     getEnv("JWT_SECRET", ""),
		JWTExpiresIn:  getEnv("JWT_EXPIRES_IN", "24h"),
		GeminiAPIKey:  getEnv("GEMINI_API_KEY", ""),
		GeminiAPIURL:  getEnv("GEMINI_API_URL", "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent"),
		Port:          getEnv("PORT", "8080"),
		GinMode:       getEnv("GIN_MODE", "debug"),
		CORSOrigins:   strings.Split(getEnv("CORS_ORIGINS", "http://localhost:3000"), ","),
		MaxFileSize:   getEnvInt64("MAX_FILE_SIZE", 10485760), // 10MB
		AllowedTypes:  strings.Split(getEnv("ALLOWED_FILE_TYPES", "application/pdf"), ","),
		DefaultTokenLimit: getEnvInt("DEFAULT_TOKEN_LIMIT", 10000),
		TokenRefillRate:   getEnvInt("TOKEN_REFILL_RATE", 1000),
		BcryptCost:       getEnvInt("BCRYPT_COST", 12),
		RateLimitReqs:    getEnvInt("RATE_LIMIT_REQUESTS", 100),
		RateLimitWindow:  getEnvInt("RATE_LIMIT_WINDOW", 60),
		MaxChunkSize:     getEnvInt("MAX_CHUNK_SIZE", 1000),
		ChunkOverlap:     getEnvInt("CHUNK_OVERLAP", 200),
	}

	// Validate required fields
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}

	if cfg.GeminiAPIKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}
	return defaultValue
}