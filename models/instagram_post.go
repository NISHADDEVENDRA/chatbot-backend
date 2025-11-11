package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// InstagramPost represents an Instagram post entry for the chatbot
type InstagramPost struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID  primitive.ObjectID `bson:"client_id" json:"client_id"`
	PostURL   string             `bson:"post_url" json:"post_url" binding:"required"`
	Title     string             `bson:"title,omitempty" json:"title,omitempty"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

