package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type User struct {
	ID           primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	Username     string              `bson:"username" json:"username" binding:"required,min=3,max=50"`
	Name         string              `bson:"name" json:"name" binding:"required,min=2,max=100"`
	Email        string              `bson:"email,omitempty" json:"email,omitempty"`
	Phone        string              `bson:"phone,omitempty" json:"phone,omitempty"`
	PasswordHash string              `bson:"password_hash" json:"-"`
	Role         string              `bson:"role" json:"role" binding:"required,oneof=admin client visitor"`
	ClientID     *primitive.ObjectID `bson:"client_id,omitempty" json:"client_id,omitempty"`
	TokenUsage   int                 `bson:"token_usage" json:"token_usage"`
	CreatedAt    time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time           `bson:"updated_at" json:"updated_at"`
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Name     string `json:"name" binding:"required,min=2,max=100"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Role     string `json:"role" binding:"required,oneof=admin client visitor"`
	ClientID string `json:"client_id,omitempty"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      UserInfo  `json:"user"`
}

type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Role     string `json:"role"`
	ClientID string `json:"client_id,omitempty"`
}


