package database

import (
	"context"
	"fmt"
	"sync"

	"saas-chatbot-platform/internal/auth"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type TenantDBManager struct {
	client    *mongo.Client
	databases map[string]*mongo.Database
	mu        sync.RWMutex
}

func NewTenantDBManager(mongoURI string) (*TenantDBManager, error) {
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}

	return &TenantDBManager{
		client:    client,
		databases: make(map[string]*mongo.Database),
	}, nil
}

// GetTenantDB returns isolated database for tenant
func (m *TenantDBManager) GetTenantDB(clientID string) (*mongo.Database, error) {
	m.mu.RLock()
	if db, exists := m.databases[clientID]; exists {
		m.mu.RUnlock()
		return db, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if db, exists := m.databases[clientID]; exists {
		return db, nil
	}

	// Create tenant-specific database
	dbName := fmt.Sprintf("tenant_%s", clientID)
	db := m.client.Database(dbName)

	// Create indexes for new tenant database
	if err := m.createTenantIndexes(db); err != nil {
		return nil, err
	}

	m.databases[clientID] = db
	return db, nil
}

func (m *TenantDBManager) createTenantIndexes(db *mongo.Database) error {
	ctx := context.Background()

	// PDFs collection indexes
	pdfsCol := db.Collection("pdfs")
	_, err := pdfsCol.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "filename", Value: 1}}},
		{Keys: bson.D{{Key: "uploaded_at", Value: -1}}},
	})
	if err != nil {
		return err
	}

	// Messages collection indexes
	messagesCol := db.Collection("messages")
	_, err = messagesCol.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "conversation_id", Value: 1}, {Key: "timestamp", Value: 1}}},
		{Keys: bson.D{{Key: "from_user_id", Value: 1}}},
	})

	return err
}

// Middleware to inject tenant database
func TenantDBMiddleware(dbManager *TenantDBManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		claimsValue, exists := c.Get("claims")
		if !exists {
			c.Next()
			return
		}

		tokenClaims, ok := claimsValue.(*auth.Claims)
		if !ok || tokenClaims.ClientID == "" {
			c.Next()
			return
		}

		clientID := tokenClaims.ClientID
		tenantDB, err := dbManager.GetTenantDB(clientID)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{"error": "database error"})
			return
		}

		c.Set("tenantDB", tenantDB)
		c.Next()
	}
}

// Migration utilities
func (m *TenantDBManager) MigrateToTenantDatabases(sharedDB *mongo.Database) error {
	ctx := context.Background()

	// Get all unique client_ids from shared collection
	clientsCollection := sharedDB.Collection("clients")
	cursor, err := clientsCollection.Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var clients []struct {
		ID string `bson:"_id"`
	}

	for cursor.Next(ctx) {
		var client struct {
			ID primitive.ObjectID `bson:"_id"`
		}
		if err := cursor.Decode(&client); err != nil {
			continue
		}
		clients = append(clients, struct {
			ID string `bson:"_id"`
		}{ID: client.ID.Hex()})
	}

	for _, client := range clients {
		tenantDB, err := m.GetTenantDB(client.ID)
		if err != nil {
			return fmt.Errorf("failed to create tenant DB for %s: %v", client.ID, err)
		}

		// Copy data to tenant-specific database
		if err := m.copyPDFs(sharedDB, tenantDB, client.ID); err != nil {
			return fmt.Errorf("failed to copy PDFs for %s: %v", client.ID, err)
		}

		if err := m.copyMessages(sharedDB, tenantDB, client.ID); err != nil {
			return fmt.Errorf("failed to copy messages for %s: %v", client.ID, err)
		}

		if err := m.copyUsers(sharedDB, tenantDB, client.ID); err != nil {
			return fmt.Errorf("failed to copy users for %s: %v", client.ID, err)
		}

		// Mark tenant as migrated (feature flag)
		if err := m.markTenantMigrated(sharedDB, client.ID); err != nil {
			return fmt.Errorf("failed to mark tenant migrated for %s: %v", client.ID, err)
		}
	}

	return nil
}

func (m *TenantDBManager) copyPDFs(sharedDB, tenantDB *mongo.Database, clientID string) error {
	ctx := context.Background()
	sharedCol := sharedDB.Collection("pdfs")
	tenantCol := tenantDB.Collection("pdfs")

	cursor, err := sharedCol.Find(ctx, bson.M{"client_id": clientID})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var pdfs []bson.M
	if err := cursor.All(ctx, &pdfs); err != nil {
		return err
	}

	if len(pdfs) > 0 {
		// Convert []bson.M to []interface{}
		docs := make([]interface{}, len(pdfs))
		for i, doc := range pdfs {
			docs[i] = doc
		}
		_, err = tenantCol.InsertMany(ctx, docs)
		return err
	}

	return nil
}

func (m *TenantDBManager) copyMessages(sharedDB, tenantDB *mongo.Database, clientID string) error {
	ctx := context.Background()
	sharedCol := sharedDB.Collection("messages")
	tenantCol := tenantDB.Collection("messages")

	cursor, err := sharedCol.Find(ctx, bson.M{"client_id": clientID})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var messages []bson.M
	if err := cursor.All(ctx, &messages); err != nil {
		return err
	}

	if len(messages) > 0 {
		// Convert []bson.M to []interface{}
		docs := make([]interface{}, len(messages))
		for i, doc := range messages {
			docs[i] = doc
		}
		_, err = tenantCol.InsertMany(ctx, docs)
		return err
	}

	return nil
}

func (m *TenantDBManager) copyUsers(sharedDB, tenantDB *mongo.Database, clientID string) error {
	ctx := context.Background()
	sharedCol := sharedDB.Collection("users")
	tenantCol := tenantDB.Collection("users")

	cursor, err := sharedCol.Find(ctx, bson.M{"client_id": clientID})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var users []bson.M
	if err := cursor.All(ctx, &users); err != nil {
		return err
	}

	if len(users) > 0 {
		// Convert []bson.M to []interface{}
		docs := make([]interface{}, len(users))
		for i, doc := range users {
			docs[i] = doc
		}
		_, err = tenantCol.InsertMany(ctx, docs)
		return err
	}

	return nil
}

func (m *TenantDBManager) markTenantMigrated(sharedDB *mongo.Database, clientID string) error {
	ctx := context.Background()
	clientsCollection := sharedDB.Collection("clients")

	clientObjectID, err := primitive.ObjectIDFromHex(clientID)
	if err != nil {
		return err
	}

	_, err = clientsCollection.UpdateOne(
		ctx,
		bson.M{"_id": clientObjectID},
		bson.M{"$set": bson.M{"migrated_to_tenant_db": true}},
	)

	return err
}
