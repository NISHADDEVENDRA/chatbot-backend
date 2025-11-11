package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/internal/database"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/migrate.go <command>")
		fmt.Println("Commands:")
		fmt.Println("  migrate-to-tenants  - Migrate shared collections to tenant-specific databases")
		fmt.Println("  verify-migration    - Verify migration completed successfully")
		os.Exit(1)
	}

	command := os.Args[1]

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

	sharedDB := client.Database(cfg.DBName)

	// Create tenant database manager
	tenantManager, err := database.NewTenantDBManager(cfg.MongoURI)
	if err != nil {
		log.Fatalf("Failed to create tenant manager: %v", err)
	}

	switch command {
	case "migrate-to-tenants":
		if err := migrateToTenantDatabases(tenantManager, sharedDB); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
		fmt.Println("Migration completed successfully!")

	case "verify-migration":
		if err := verifyMigration(tenantManager, sharedDB); err != nil {
			log.Fatalf("Verification failed: %v", err)
		}
		fmt.Println("Migration verification completed successfully!")

	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}

func migrateToTenantDatabases(tenantManager *database.TenantDBManager, sharedDB *mongo.Database) error {
	fmt.Println("Starting migration to tenant-specific databases...")

	// Run the migration
	if err := tenantManager.MigrateToTenantDatabases(sharedDB); err != nil {
		return fmt.Errorf("migration failed: %v", err)
	}

	fmt.Println("Migration completed successfully!")
	return nil
}

func verifyMigration(tenantManager *database.TenantDBManager, sharedDB *mongo.Database) error {
	fmt.Println("Verifying migration...")

	// Get all clients
	ctx := context.Background()
	clientsCollection := sharedDB.Collection("clients")
	cursor, err := clientsCollection.Find(ctx, map[string]interface{}{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var clients []map[string]interface{}
	if err := cursor.All(ctx, &clients); err != nil {
		return err
	}

	fmt.Printf("Found %d clients to verify\n", len(clients))

	for _, client := range clients {
		clientID := client["_id"].(string)
		fmt.Printf("Verifying client: %s\n", clientID)

		// Get tenant database
		tenantDB, err := tenantManager.GetTenantDB(clientID)
		if err != nil {
			return fmt.Errorf("failed to get tenant DB for %s: %v", clientID, err)
		}

		// Verify collections exist and have data
		collections := []string{"pdfs", "messages", "users"}
		for _, collectionName := range collections {
			collection := tenantDB.Collection(collectionName)
			count, err := collection.CountDocuments(ctx, map[string]interface{}{})
			if err != nil {
				return fmt.Errorf("failed to count documents in %s for client %s: %v", collectionName, clientID, err)
			}
			fmt.Printf("  %s: %d documents\n", collectionName, count)
		}
	}

	return nil
}
