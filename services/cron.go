package services

import (
    "context"
    "log"
    "time"
    
    "go.mongodb.org/mongo-driver/mongo"
    
    "saas-chatbot-platform/internal/config"
)

type CronService struct {
    alertEvaluator *AlertEvaluator
    stopChan       chan struct{}
}

func NewCronService(cfg config.Config, emailSender EmailSender, clientsCol *mongo.Collection) *CronService {
    alertEvaluator := NewAlertEvaluator(cfg, emailSender, clientsCol)
    
    return &CronService{
        alertEvaluator: alertEvaluator,
        stopChan:       make(chan struct{}),
    }
}

func (c *CronService) Start() {
    // Simple cron implementation - runs every 15 minutes
    ticker := time.NewTicker(15 * time.Minute)
    defer ticker.Stop()
    
    log.Println("Starting token alert cron service...")
    
    for {
        select {
        case <-ticker.C:
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
            if err := c.alertEvaluator.ScanAllClients(ctx); err != nil {
                log.Printf("Cron scan failed: %v", err)
            }
            cancel()
            
        case <-c.stopChan:
            log.Println("Stopping token alert cron service...")
            return
        }
    }
}

func (c *CronService) Stop() {
    close(c.stopChan)
}
