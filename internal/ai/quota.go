package ai

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TenantGeminiQuota represents per-tenant Gemini API quotas
type TenantGeminiQuota struct {
	ClientID        string    `bson:"client_id"`
	DailyTokenLimit int       `bson:"daily_token_limit"`
	TokensUsedToday int       `bson:"tokens_used_today"`
	LastResetDate   time.Time `bson:"last_reset_date"`
	RequestsToday   int       `bson:"requests_today"`
	CreatedAt       time.Time `bson:"created_at"`
	UpdatedAt       time.Time `bson:"updated_at"`
}

// CheckTenantQuota checks if tenant can consume estimated tokens
func CheckTenantQuota(clientID string, estimatedTokens int, db *mongo.Database) error {
	ctx := context.Background()
	col := db.Collection("gemini_quotas")

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// Reset if new day
	filter := bson.M{
		"client_id":      clientID,
		"last_reset_date": bson.M{"$lt": today},
	}

	// Reset if new day
	_, err := col.UpdateOne(ctx, filter, bson.M{
		"$set": bson.M{
			"tokens_used_today": 0,
			"requests_today":    0,
			"last_reset_date":   today,
			"updated_at":        now,
		},
	})

	// Check if under limit
	var quota TenantGeminiQuota
	err = col.FindOne(ctx, bson.M{"client_id": clientID}).Decode(&quota)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// Create new quota for this tenant
			quota = TenantGeminiQuota{
				ClientID:        clientID,
				DailyTokenLimit: 10000, // Default limit
				TokensUsedToday: 0,
				RequestsToday:   0,
				LastResetDate:   today,
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			_, err = col.InsertOne(ctx, quota)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	if quota.TokensUsedToday+estimatedTokens > quota.DailyTokenLimit {
		return errors.New("daily quota exceeded")
	}

	// Increment atomically
	_, err = col.UpdateOne(
		ctx,
		bson.M{"client_id": clientID},
		bson.M{
			"$inc": bson.M{
				"tokens_used_today": estimatedTokens,
				"requests_today":    1,
			},
			"$set": bson.M{
				"updated_at": now,
			},
		},
	)

	return err
}

// GetTenantQuotaStatus returns current quota status for a tenant
func GetTenantQuotaStatus(clientID string, db *mongo.Database) (*TenantGeminiQuota, error) {
	ctx := context.Background()
	col := db.Collection("gemini_quotas")

	var quota TenantGeminiQuota
	err := col.FindOne(ctx, bson.M{"client_id": clientID}).Decode(&quota)
	if err != nil {
		return nil, err
	}

	return &quota, nil
}

// SetTenantQuotaLimit sets daily token limit for a tenant
func SetTenantQuotaLimit(clientID string, dailyLimit int, db *mongo.Database) error {
	ctx := context.Background()
	col := db.Collection("gemini_quotas")

	now := time.Now()
	_, err := col.UpdateOne(
		ctx,
		bson.M{"client_id": clientID},
		bson.M{
			"$set": bson.M{
				"daily_token_limit": dailyLimit,
				"updated_at":        now,
			},
		},
		options.Update().SetUpsert(true),
	)

	return err
}

// ResetTenantQuota resets daily usage for a tenant
func ResetTenantQuota(clientID string, db *mongo.Database) error {
	ctx := context.Background()
	col := db.Collection("gemini_quotas")

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	_, err := col.UpdateOne(
		ctx,
		bson.M{"client_id": clientID},
		bson.M{
			"$set": bson.M{
				"tokens_used_today": 0,
				"requests_today":    0,
				"last_reset_date":   today,
				"updated_at":        now,
			},
		},
	)

	return err
}
