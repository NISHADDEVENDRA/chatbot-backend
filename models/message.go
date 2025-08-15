package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Message struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	FromUserID     primitive.ObjectID `bson:"from_user_id" json:"from_user_id"`
	FromName       string             `bson:"from_name" json:"from_name"`
	Message        string             `bson:"message" json:"message"`
	Reply          string             `bson:"reply" json:"reply"`
	Timestamp      time.Time          `bson:"timestamp" json:"timestamp"`
	ClientID       primitive.ObjectID `bson:"client_id" json:"client_id"`
	ConversationID string             `bson:"conversation_id" json:"conversation_id"`
	TokenCost      int                `bson:"token_cost" json:"token_cost"`
	 UserName       string             `bson:"user_name,omitempty" json:"user_name,omitempty"`  // Store user name
    UserEmail      string             `bson:"user_email,omitempty" json:"user_email,omitempty"` // Store user email
}

type ChatRequest struct {
	Message        string `json:"message" binding:"required,min=1,max=2000"`
	ConversationID string `json:"conversation_id,omitempty"`
}

type ChatResponse struct {
	Reply          string    `json:"reply"`
	TokensUsed     int       `json:"tokens_used"`
	RemainingTokens int      `json:"remaining_tokens"`
	ConversationID string    `json:"conversation_id"`
	Timestamp      time.Time `json:"timestamp"`
}

type ConversationHistory struct {
	ConversationID string    `json:"conversation_id"`
	Messages       []Message `json:"messages"`
	TotalTokens    int       `json:"total_tokens"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}