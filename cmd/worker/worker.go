package main

import (
	"context"
	"log"

	"saas-chatbot-platform/internal/ai"
	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/internal/database"
	"saas-chatbot-platform/internal/queue"

	"github.com/hibiken/asynq"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Connect to MongoDB
	mongoClient, err := mongo.Connect(nil, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatal("Failed to connect to MongoDB:", err)
	}
	defer mongoClient.Disconnect(nil)

	// Initialize database manager
	dbManager, err := database.NewTenantDBManager(cfg.MongoURI)
	if err != nil {
		log.Fatal("Failed to create tenant manager:", err)
	}

	// Initialize Gemini client
	geminiClient, err := ai.NewGeminiClient(cfg.GeminiAPIKey, "free")
	if err != nil {
		log.Fatal("Failed to initialize Gemini client:", err)
	}
	defer geminiClient.Close()

	// Redis options for Asynq
	redisOpt := asynq.RedisClientOpt{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	}

	// Create Asynq server
	server := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: 20, // Process 20 tasks concurrently
			Queues: map[string]int{
				"critical": 6, // 60% of workers
				"default":  3, // 30% of workers
				"low":      1, // 10% of workers
			},
			StrictPriority: true,
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				log.Printf("Task failed: %s, error: %v", task.Type(), err)
				// Send to error tracking service
			}),
		},
	)

	// Create task processor
	processor := queue.NewTaskProcessor(dbManager, geminiClient, mongoClient)

	// Create mux and register handlers
	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TaskProcessPDF, processor.ProcessPDF)
	mux.HandleFunc(queue.TaskGenerateAIResp, processor.GenerateAIResponse)

	log.Println("🚀 Starting Asynq worker...")
	log.Printf("   Concurrency: 20")
	log.Printf("   Queues: critical(6), default(3), low(1)")
	log.Printf("   Redis: %s", redisOpt.Addr)

	// Start the server
	if err := server.Run(mux); err != nil {
		log.Fatal("Failed to start worker:", err)
	}
}
