package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	MongoURI            string
	DBName              string
	JWTSecret           string
	JWTExpiresIn        string
	GeminiAPIKey        string
	GeminiAPIURL        string
	Port                string
	GinMode             string
	CORSOrigins         []string
	MaxFileSize         int64
	AllowedTypes        []string
	DefaultTokenLimit   int
	TokenRefillRate     int
	BcryptCost          int
	RateLimitReqs       int
	RateLimitWindow     int
	MaxChunkSize        int
	ChunkOverlap        int
	FileStorageDir      string
	SyncProcessingLimit int64

	// Redis Configuration
	RedisURL      string
	RedisPassword string
	RedisDB       int

	// JWT Token Secrets
	AccessSecret  string
	RefreshSecret string

	// Token Alert Configuration
	TokenWarnPercent      int    `default:"80"`
	TokenCriticalPercent  int    `default:"95"`
	TokenExhaustedPercent int    `default:"100"`
	TokenAlertCron        string `default:"*/15 * * * *"`

	// SMTP Configuration
	SMTPHost    string
	SMTPPort    string `default:"587"`
	SMTPUser    string
	SMTPPass    string
	SMTPFrom    string
	AdminEmails []string

	// OCR Service Configuration (Deprecated - DeepSeek-OCR removed)
	// Kept for backward compatibility but not actively used
	OCRServiceURL          string
	OCRServiceEnabled      bool
	OCRTimeout             int
	OCRConfidenceThreshold float64

	// MongoDB Search/Vector Search
	AtlasTextSearchEnabled bool
	VectorSearchEnabled    bool
	SearchIndexName        string
	VectorIndexName        string
	VectorDimensions       int

	// Embeddings configuration
	EmbeddingsProvider    string // "google" (default), "openai"
	GoogleEmbeddingsModel string // e.g., "text-embedding-004"
	OpenAIAPIKey          string
	OpenAIEmbeddingsModel string

	// CSRF Protection
	CSRFSecret string
}

func LoadConfig() (*Config, error) {
	// Load .env file if exists
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			return nil, fmt.Errorf("error loading .env file: %v", err)
		}
	}

	cfg := &Config{
		MongoURI:            getEnv("MONGO_URI", "mongodb://localhost:27017/saas_chatbot"),
		DBName:              getEnv("DB_NAME", "saas_chatbot"),
		JWTSecret:           getEnv("JWT_SECRET", ""),
		JWTExpiresIn:        getEnv("JWT_EXPIRES_IN", "24h"),
		GeminiAPIKey:        getEnv("GEMINI_API_KEY", ""),
		GeminiAPIURL:        getEnv("GEMINI_API_URL", "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent"),
		Port:                getEnv("PORT", "8080"),
		GinMode:             getEnv("GIN_MODE", "debug"),
		CORSOrigins:         strings.Split(getEnv("CORS_ORIGINS", "http://localhost:3000,http://localhost:8080"), ","),
		MaxFileSize:         getEnvInt64("MAX_FILE_SIZE", 104857600), // 100MB maximum for robust large file support
		AllowedTypes:        strings.Split(getEnv("ALLOWED_FILE_TYPES", "application/pdf"), ","),
		DefaultTokenLimit:   getEnvInt("DEFAULT_TOKEN_LIMIT", 10000),
		TokenRefillRate:     getEnvInt("TOKEN_REFILL_RATE", 1000),
		BcryptCost:          getEnvInt("BCRYPT_COST", 12),
		RateLimitReqs:       getEnvInt("RATE_LIMIT_REQUESTS", 100),
		RateLimitWindow:     getEnvInt("RATE_LIMIT_WINDOW", 60),
		MaxChunkSize:        getEnvInt("MAX_CHUNK_SIZE", 1000),
		ChunkOverlap:        getEnvInt("CHUNK_OVERLAP", 200),
		FileStorageDir:      getEnv("FILE_STORAGE_DIR", "./storage"),
		SyncProcessingLimit: getEnvInt64("SYNC_PROCESSING_LIMIT", 20971520), // 20MB sync processing limit

		// Redis Configuration
		RedisURL:      getEnv("REDIS_URL", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		// JWT Token Secrets
		AccessSecret:  getEnv("ACCESS_SECRET", ""),
		RefreshSecret: getEnv("REFRESH_SECRET", ""),

		// Token Alert Configuration
		TokenWarnPercent:      getEnvInt("TOKEN_WARN_PERCENT", 80),
		TokenCriticalPercent:  getEnvInt("TOKEN_CRITICAL_PERCENT", 95),
		TokenExhaustedPercent: getEnvInt("TOKEN_EXHAUSTED_PERCENT", 100),
		TokenAlertCron:        getEnv("TOKEN_ALERT_CRON", "*/15 * * * *"),

		// SMTP Configuration
		SMTPHost:    getEnv("SMTP_HOST", ""),
		SMTPPort:    getEnv("SMTP_PORT", "587"),
		SMTPUser:    getEnv("SMTP_USER", ""),
		SMTPPass:    getEnv("SMTP_PASS", ""),
		SMTPFrom:    getEnv("SMTP_FROM", ""),
		AdminEmails: strings.Split(getEnv("ADMIN_EMAILS", ""), ","),

		// OCR Service Configuration
		OCRServiceURL:          getEnv("OCR_SERVICE_URL", "http://localhost:8001"),
		OCRServiceEnabled:      getEnvBool("OCR_SERVICE_ENABLED", true),
		OCRTimeout:             getEnvInt("OCR_TIMEOUT", 300), // 5 minutes
		OCRConfidenceThreshold: getEnvFloat64("OCR_CONFIDENCE_THRESHOLD", 0.7),

		// MongoDB Search/Vector Search
		AtlasTextSearchEnabled: getEnvBool("MONGODB_SEARCH_ENABLED", false),
		VectorSearchEnabled:    getEnvBool("MONGODB_VECTOR_ENABLED", false),
		SearchIndexName:        getEnv("MONGODB_SEARCH_INDEX", "pdf_chunks_text"),
		VectorIndexName:        getEnv("MONGODB_VECTOR_INDEX", "pdf_chunks_vector"),
		VectorDimensions:       getEnvInt("VECTOR_DIM", 768),

		// Embeddings
		EmbeddingsProvider:    getEnv("EMBEDDINGS_PROVIDER", "google"),
		GoogleEmbeddingsModel: getEnv("GOOGLE_EMBEDDINGS_MODEL", "text-embedding-004"),
		OpenAIAPIKey:          getEnv("OPENAI_API_KEY", ""),
		OpenAIEmbeddingsModel: getEnv("OPENAI_EMBEDDINGS_MODEL", "text-embedding-3-small"),

		// CSRF Protection
		CSRFSecret: getEnv("CSRF_SECRET", ""),
	}

	// Validate required fields
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required - set it in .env file")
	}

	if cfg.AccessSecret == "" {
		return nil, fmt.Errorf("ACCESS_SECRET is required - set it in .env file")
	}

	if cfg.RefreshSecret == "" {
		return nil, fmt.Errorf("REFRESH_SECRET is required - set it in .env file")
	}

	if cfg.GeminiAPIKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required - set it in .env file")
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

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvFloat64(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
			return floatValue
		}
	}
	return defaultValue
}
