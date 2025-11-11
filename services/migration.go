package services

import (
    "context"
    "time"
    
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
)

func MigrateClientAlertFields(ctx context.Context, clientsCol *mongo.Collection) error {
    // Add alert fields to existing clients that don't have them
    filter := bson.M{
        "$or": []bson.M{
            {"alert_level_sent": bson.M{"$exists": false}},
            {"alert_last_sent_at": bson.M{"$exists": false}},
        },
    }
    
    update := bson.M{
        "$set": bson.M{
            "alert_level_sent":   "none",
            "alert_last_sent_at": time.Time{},
            "quota_period_start": time.Time{},
            "quota_period_end":   time.Time{},
            "updated_at":         time.Now(),
        },
    }
    
    _, err := clientsCol.UpdateMany(ctx, filter, update)
    return err
}
