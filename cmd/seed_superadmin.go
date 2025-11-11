package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Connect to MongoDB
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(context.Background())

	db := client.Database(cfg.DBName)
	usersCollection := db.Collection("users")

	// Check if superadmin already exists
	var existingSuperAdmin models.User
	err = usersCollection.FindOne(context.Background(), bson.M{"username": "superadmin", "role": "superadmin"}).Decode(&existingSuperAdmin)
	if err == nil {
		fmt.Println("✅ SuperAdmin user already exists!")
		fmt.Printf("   Username: %s\n", existingSuperAdmin.Username)
		fmt.Printf("   Email: %s\n", existingSuperAdmin.Email)
		os.Exit(0)
	}

	// Get superadmin credentials from environment or use defaults
	superAdminUsername := os.Getenv("SUPERADMIN_USERNAME")
	if superAdminUsername == "" {
		superAdminUsername = "superadmin"
	}

	superAdminPassword := os.Getenv("SUPERADMIN_PASSWORD")
	if superAdminPassword == "" {
		superAdminPassword = "SuperAdmin123!" // ⚠️ Change this in production!
		fmt.Println("⚠️  WARNING: Using default password. Set SUPERADMIN_PASSWORD environment variable!")
	}

	superAdminEmail := os.Getenv("SUPERADMIN_EMAIL")
	if superAdminEmail == "" {
		superAdminEmail = "superadmin@example.com"
	}

	// Hash password
	hashedPassword, err := utils.HashPassword(superAdminPassword, cfg.BcryptCost)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	// Create superadmin user
	superAdminUser := models.User{
		Username:     superAdminUsername,
		Name:         "Super Administrator",
		Email:        superAdminEmail,
		Phone:        "",
		PasswordHash: hashedPassword,
		Role:         "superadmin", // ✅ SuperAdmin role
		ClientID:     nil,         // ✅ No client_id for superadmin
		TokenUsage:   0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	result, err := usersCollection.InsertOne(context.Background(), superAdminUser)
	if err != nil {
		log.Fatalf("Failed to create superadmin user: %v", err)
	}

	fmt.Printf("✅ SuperAdmin user created successfully!\n")
	fmt.Printf("   Username: %s\n", superAdminUsername)
	fmt.Printf("   Password: %s\n", superAdminPassword)
	fmt.Printf("   Email: %s\n", superAdminEmail)
	fmt.Printf("   User ID: %s\n", result.InsertedID.(primitive.ObjectID).Hex())
	fmt.Printf("\n⚠️  IMPORTANT: Change the password after first login!\n")
	fmt.Printf("\n📝 Next steps:\n")
	fmt.Printf("   1. Login at POST http://localhost:8080/auth/login\n")
	fmt.Printf("   2. Use the access_token to create more users\n")
}

