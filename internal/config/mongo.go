package config

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson" // Use bson for index keys
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ConnectMongoDB(cfg *Config) (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	// Test connection
	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %v", err)
	}

	// Create indexes
	err = createIndexes(client, cfg.DBName)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexes: %v", err)
	}

	return client, nil
}

func createIndexes(client *mongo.Client, dbName string) error {
	db := client.Database(dbName)

	// Users collection indexes
	usersCollection := db.Collection("users")
	userIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "client_id", Value: 1}},
		},
	}
	_, err := usersCollection.Indexes().CreateMany(context.Background(), userIndexes)
	if err != nil {
		return err
	}

	// Clients collection indexes
	clientsCollection := db.Collection("clients")
	clientIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "name", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "embed_secret", Value: 1}},
		},
	}
	_, err = clientsCollection.Indexes().CreateMany(context.Background(), clientIndexes)
	if err != nil {
		return err
	}

	// PDFs collection indexes
	pdfsCollection := db.Collection("pdfs")
	pdfIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "client_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "client_id", Value: 1}, {Key: "filename", Value: 1}},
		},
	}
	_, err = pdfsCollection.Indexes().CreateMany(context.Background(), pdfIndexes)
	if err != nil {
		return err
	}

	// Messages collection indexes
	messagesCollection := db.Collection("messages")
	messageIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "client_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "conversation_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "from_user_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "timestamp", Value: -1}},
		},
	}
	_, err = messagesCollection.Indexes().CreateMany(context.Background(), messageIndexes)
	if err != nil {
		return err
	}

	// PDF Chunks collection indexes for search/vector filters
	pdfChunksCollection := db.Collection("pdf_chunks")
	pdfChunkIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "client_id", Value: 1}}},
		{Keys: bson.D{{Key: "pdf_id", Value: 1}}},
		{Keys: bson.D{{Key: "chunk_id", Value: 1}}},
	}
	_, err = pdfChunksCollection.Indexes().CreateMany(context.Background(), pdfChunkIndexes)
	if err != nil {
		return err
	}

	return nil
}
