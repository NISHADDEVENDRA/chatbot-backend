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
	AvatarURL    string              `bson:"avatar_url,omitempty" json:"avatar_url,omitempty"`
	PasswordHash string              `bson:"password_hash" json:"-"`
	Role         string              `bson:"role" json:"role" binding:"required,oneof=superadmin admin client visitor"`
	ClientID     *primitive.ObjectID `bson:"client_id,omitempty" json:"client_id,omitempty"`
	TokenUsage   int                 `bson:"token_usage" json:"token_usage"`
	CreatedAt    time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time           `bson:"updated_at" json:"updated_at"`
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50,alphanum"`
	Name     string `json:"name" binding:"required,min=2,max=100"`
	Email    string `json:"email" binding:"omitempty,email"`
	Phone    string `json:"phone" binding:"omitempty,e164"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	ClientID string `json:"client_id,omitempty" binding:"omitempty,hexadecimal,len=24"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Password string `json:"password" binding:"required,min=8"`
}

type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      UserInfo  `json:"user"`
}

type TokenPairResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	AccessExp    time.Time `json:"access_exp"`
	RefreshExp   time.Time `json:"refresh_exp"`
	User         UserInfo  `json:"user"`
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

type PDFDocument struct {
	ID        string    `bson:"_id" json:"id"`
	ClientID  string    `bson:"client_id" json:"client_id"`
	Filename  string    `bson:"filename" json:"filename"`
	Size      int64     `bson:"size" json:"size"`
	Status    string    `bson:"status" json:"status"` // pending, processing, completed, failed
	Progress  int       `bson:"progress" json:"progress"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

type PasswordReset struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"user_id" json:"user_id"`
	Token     string              `bson:"token" json:"token"`
	Email     string              `bson:"email" json:"email"`
	ExpiresAt time.Time           `bson:"expires_at" json:"expires_at"`
	Used      bool                 `bson:"used" json:"used"`
	CreatedAt time.Time            `bson:"created_at" json:"created_at"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type ResetPasswordRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}
