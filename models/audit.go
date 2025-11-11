package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuditEvent represents an immutable audit log entry
type AuditEvent struct {
	ID           string                 `bson:"_id,omitempty"`
	Timestamp    time.Time              `bson:"timestamp"`
	ClientID     string                 `bson:"client_id"`
	UserID       string                 `bson:"user_id"`
	Action       string                 `bson:"action"`   // CREATE, READ, UPDATE, DELETE
	Resource     string                 `bson:"resource"` // pdf, message, client, user
	ResourceID   string                 `bson:"resource_id"`
	IPAddress    string                 `bson:"ip_address"`
	UserAgent    string                 `bson:"user_agent"`
	RequestID    string                 `bson:"request_id"`
	Success      bool                   `bson:"success"`
	ErrorMessage string                 `bson:"error_message,omitempty"`
	Changes      map[string]interface{} `bson:"changes,omitempty"` // Before/after values
	PreviousHash string                 `bson:"previous_hash"`     // Hash of previous audit entry
	CurrentHash  string                 `bson:"current_hash"`      // Hash of this entry
	CreatedAt    time.Time              `bson:"created_at"`
}

// ComputeHash computes the hash of this audit event
func (e *AuditEvent) ComputeHash() string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%t|%s",
		e.Timestamp.Format(time.RFC3339Nano),
		e.ClientID,
		e.UserID,
		e.Action,
		e.Resource,
		e.ResourceID,
		e.Success,
		e.PreviousHash,
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// AuditLogger handles immutable audit logging
type AuditLogger struct {
	col        *mongo.Collection
	lastHashMu sync.RWMutex
	lastHashes map[string]string // clientID -> last hash
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(db *mongo.Database) *AuditLogger {
	col := db.Collection("audit_logs")

	// Create indexes for efficient querying
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "client_id", Value: 1},
				{Key: "timestamp", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "resource", Value: 1},
				{Key: "resource_id", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "action", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "timestamp", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "request_id", Value: 1},
			},
		},
	}

	// Create indexes
	col.Indexes().CreateMany(context.Background(), indexes)

	return &AuditLogger{
		col:        col,
		lastHashes: make(map[string]string),
	}
}

// Log logs an audit event
func (al *AuditLogger) Log(event *AuditEvent) error {
	al.lastHashMu.Lock()
	defer al.lastHashMu.Unlock()

	// Get last hash for this client to create chain
	event.PreviousHash = al.lastHashes[event.ClientID]
	event.Timestamp = time.Now().UTC()
	event.CreatedAt = time.Now().UTC()

	// Generate unique ID
	event.ID = fmt.Sprintf("%d_%s", time.Now().UnixNano(), event.ClientID)

	// Compute hash of this event
	event.CurrentHash = event.ComputeHash()

	// Store audit event (insert-only, never update)
	ctx := context.Background()
	_, err := al.col.InsertOne(ctx, event)
	if err != nil {
		log.Printf("❌ Failed to log audit event: %v", err)
		return err
	}

	// Update last hash
	al.lastHashes[event.ClientID] = event.CurrentHash

	log.Printf("✅ Audit event logged: %s %s %s", event.Action, event.Resource, event.ResourceID)
	return nil
}

// LogAsync logs an audit event asynchronously
func (al *AuditLogger) LogAsync(event *AuditEvent) {
	go func() {
		if err := al.Log(event); err != nil {
			log.Printf("❌ Async audit logging failed: %v", err)
		}
	}()
}

// VerifyChain verifies the integrity of the audit chain for a client
func (al *AuditLogger) VerifyChain(clientID string) (bool, error) {
	ctx := context.Background()
	cursor, err := al.col.Find(ctx,
		bson.M{"client_id": clientID},
		options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}}),
	)
	if err != nil {
		return false, err
	}
	defer cursor.Close(ctx)

	var previousHash string
	eventCount := 0

	for cursor.Next(ctx) {
		var event AuditEvent
		if err := cursor.Decode(&event); err != nil {
			return false, err
		}

		eventCount++

		// Verify previous hash matches (except for first event)
		if eventCount > 1 && event.PreviousHash != previousHash {
			log.Printf("❌ Audit chain broken at event %s - previous hash mismatch", event.ID)
			return false, nil
		}

		// Verify current hash is correct
		expectedHash := event.ComputeHash()
		if event.CurrentHash != expectedHash {
			log.Printf("❌ Audit event hash mismatch at %s", event.ID)
			return false, nil
		}

		previousHash = event.CurrentHash
	}

	log.Printf("✅ Audit chain verified for client %s: %d events", clientID, eventCount)
	return true, nil
}

// QueryAuditLogs queries audit logs with filters
func (al *AuditLogger) QueryAuditLogs(filter bson.M, page, pageSize int) ([]AuditEvent, int64, error) {
	ctx := context.Background()

	// Get total count
	total, err := al.col.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Execute query with pagination
	skip := (page - 1) * pageSize
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetSkip(int64(skip)).
		SetLimit(int64(pageSize))

	cursor, err := al.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var events []AuditEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// GetAuditSummary returns audit summary for a client
func (al *AuditLogger) GetAuditSummary(clientID string, days int) (map[string]interface{}, error) {
	ctx := context.Background()

	startTime := time.Now().AddDate(0, 0, -days)
	filter := bson.M{
		"client_id": clientID,
		"timestamp": bson.M{"$gte": startTime},
	}

	// Aggregate audit events
	pipeline := []bson.M{
		{"$match": filter},
		{
			"$group": bson.M{
				"_id":   "$action",
				"count": bson.M{"$sum": 1},
				"success_count": bson.M{
					"$sum": bson.M{
						"$cond": bson.M{
							"if":   "$success",
							"then": 1,
							"else": 0,
						},
					},
				},
			},
		},
	}

	cursor, err := al.col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	summary := map[string]interface{}{
		"client_id":    clientID,
		"period_days":  days,
		"actions":      results,
		"total_events": len(results),
	}

	return summary, nil
}

// Collection returns the audit collection for direct access
func (al *AuditLogger) Collection() *mongo.Collection {
	return al.col
}
