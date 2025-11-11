package services

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/bson/primitive"
    "go.mongodb.org/mongo-driver/mongo"
    
    "saas-chatbot-platform/internal/config"
    "saas-chatbot-platform/models"
)

type AlertEvaluator struct {
    config       config.Config
    emailSender  EmailSender
    clientsCol   *mongo.Collection
}

func NewAlertEvaluator(cfg config.Config, emailSender EmailSender, clientsCol *mongo.Collection) *AlertEvaluator {
    return &AlertEvaluator{
        config:      cfg,
        emailSender: emailSender,
        clientsCol:  clientsCol,
    }
}

func (a *AlertEvaluator) EvaluateAndNotify(ctx context.Context, clientID primitive.ObjectID) error {
    // Fetch current client data
    var client models.Client
    err := a.clientsCol.FindOne(ctx, bson.M{"_id": clientID}).Decode(&client)
    if err != nil {
        return fmt.Errorf("failed to fetch client: %w", err)
    }
    
    // Calculate usage percentage
    if client.TokenLimit == 0 {
        return nil // Skip if no token limit set
    }
    
    percentUsed := float64(client.TokenUsed) / float64(client.TokenLimit) * 100
    
    // Determine alert level needed
    var alertLevel string
    if percentUsed >= float64(a.config.TokenExhaustedPercent) {
        alertLevel = "exhausted"
    } else if percentUsed >= float64(a.config.TokenCriticalPercent) {
        alertLevel = "critical"
    } else if percentUsed >= float64(a.config.TokenWarnPercent) {
        alertLevel = "warn"
    } else {
        return nil // No alert needed
    }
    
    // Check if we've already sent this alert level in current period
    if a.shouldSkipAlert(client, alertLevel) {
        return nil
    }
    
    // Prepare alert data
    tokenData := TokenAlertData{
        TenantName:      client.Name,
        ClientEmail:     client.ContactEmail,
        AdminEmails:     a.config.AdminEmails,
        UsedTokens:      client.TokenUsed,
        TotalTokens:     client.TokenLimit,
        RemainingTokens: client.TokenLimit - client.TokenUsed,
        PercentUsed:     percentUsed,
    }
    
    // Send email notification
    if err := a.emailSender.SendTokenAlert(client, alertLevel, tokenData); err != nil {
        log.Printf("Failed to send %s alert for client %s: %v", alertLevel, client.Name, err)
        return err
    }
    
    // Update client alert status atomically
    return a.updateAlertStatus(ctx, clientID, alertLevel)
}

func (a *AlertEvaluator) shouldSkipAlert(client models.Client, alertLevel string) bool {
    // If no alert has been sent yet, don't skip
    if client.AlertLevelSent == "" || client.AlertLevelSent == "none" {
        return false
    }
    
    // Define alert hierarchy
    alertHierarchy := map[string]int{
        "warn":      1,
        "critical":  2,
        "exhausted": 3,
    }
    
    currentLevel := alertHierarchy[client.AlertLevelSent]
    newLevel := alertHierarchy[alertLevel]
    
    // Skip if we've already sent a higher or equal level alert
    if currentLevel >= newLevel {
        return true
    }
    
    return false
}

func (a *AlertEvaluator) updateAlertStatus(ctx context.Context, clientID primitive.ObjectID, alertLevel string) error {
    update := bson.M{
        "$set": bson.M{
            "alert_level_sent":   alertLevel,
            "alert_last_sent_at": time.Now(),
            "updated_at":         time.Now(),
        },
    }
    
    _, err := a.clientsCol.UpdateOne(ctx, bson.M{"_id": clientID}, update)
    return err
}

// Reset alert status for a client (called on token top-up or quota reset)
func (a *AlertEvaluator) ResetAlertStatus(ctx context.Context, clientID primitive.ObjectID) error {
    update := bson.M{
        "$set": bson.M{
            "alert_level_sent":   "none",
            "alert_last_sent_at": time.Time{},
            "updated_at":         time.Now(),
        },
    }
    
    _, err := a.clientsCol.UpdateOne(ctx, bson.M{"_id": clientID}, update)
    return err
}

// Batch scanner for periodic checking
func (a *AlertEvaluator) ScanAllClients(ctx context.Context) error {
    cursor, err := a.clientsCol.Find(ctx, bson.M{"status": bson.M{"$ne": "inactive"}})
    if err != nil {
        return err
    }
    defer cursor.Close(ctx)
    
    for cursor.Next(ctx) {
        var client models.Client
        if err := cursor.Decode(&client); err != nil {
            log.Printf("Failed to decode client: %v", err)
            continue
        }
        
        if err := a.EvaluateAndNotify(ctx, client.ID); err != nil {
            log.Printf("Failed to evaluate alerts for client %s: %v", client.Name, err)
        }
    }
    
    return cursor.Err()
}
