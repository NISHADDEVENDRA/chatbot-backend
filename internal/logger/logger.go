package logger

import (
	"log/slog"
	"os"
	"saas-chatbot-platform/internal/config"
)

var Logger *slog.Logger

// InitLogger initializes structured logging based on configuration
func InitLogger(cfg *config.Config) {
	level := slog.LevelInfo
	if cfg.GinMode == "debug" {
		level = slog.LevelDebug
	}
	
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.GinMode == "debug", // Only add source in debug mode
	}
	
	handler := slog.NewJSONHandler(os.Stdout, opts)
	Logger = slog.New(handler)
	
	if cfg.GinMode == "debug" {
		Logger.Debug("Structured logging initialized", "level", level.String())
	} else {
		Logger.Info("Structured logging initialized", "level", level.String())
	}
}

// Helper functions for common log operations
func Info(msg string, args ...any) {
	if Logger != nil {
		Logger.Info(msg, args...)
	}
}

func Error(msg string, args ...any) {
	if Logger != nil {
		Logger.Error(msg, args...)
	}
}

func Debug(msg string, args ...any) {
	if Logger != nil {
		Logger.Debug(msg, args...)
	}
}

func Warn(msg string, args ...any) {
	if Logger != nil {
		Logger.Warn(msg, args...)
	}
}

