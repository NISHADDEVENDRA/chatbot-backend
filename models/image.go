package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Image represents an image entry for the chatbot gallery
type Image struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID  primitive.ObjectID `bson:"client_id" json:"client_id"`
	URL       string             `bson:"url" json:"url" binding:"required"`
	Title     string             `bson:"title" json:"title" binding:"required"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

