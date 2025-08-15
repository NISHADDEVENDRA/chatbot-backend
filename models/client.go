package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Client struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name          string             `bson:"name" json:"name" binding:"required,min=2,max=100"`
	Branding      Branding           `bson:"branding" json:"branding"`
	TokenLimit    int                `bson:"token_limit" json:"token_limit"`
	TokenUsed     int                `bson:"token_used" json:"token_used"`
	EmbedSecret   string             `bson:"embed_secret" json:"embed_secret"`
	Status        string             `bson:"status,omitempty" json:"status,omitempty"`                   // optional, default "active"
	ContactEmail  string             `bson:"contact_email,omitempty" json:"contact_email,omitempty"`     // optional
	ContactPhone  string             `bson:"contact_phone,omitempty" json:"contact_phone,omitempty"`     // optional
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}

type Branding struct {
	LogoURL        string   `bson:"logo_url" json:"logo_url"`
	ThemeColor     string   `bson:"theme_color" json:"theme_color"`
	WelcomeMessage string   `bson:"welcome_message" json:"welcome_message"`
	PreQuestions   []string `bson:"pre_questions" json:"pre_questions" binding:"max=5"` // ← allow up to 5
	AllowEmbedding bool     `bson:"allow_embedding" json:"allow_embedding"`
	ShowPoweredBy  bool     `bson:"show_powered_by,omitempty" json:"show_powered_by,omitempty"`
	WidgetPosition string   `bson:"widget_position,omitempty" json:"widget_position,omitempty"`
}

type CreateClientRequest struct {
	Name          string   `json:"name" binding:"required,min=2,max=100"`
	TokenLimit    int      `json:"token_limit" binding:"required,min=1000"`
	Branding      Branding `json:"branding"`
	Status        string   `json:"status,omitempty"`
	ContactEmail  string   `json:"contact_email,omitempty"`
	ContactPhone  string   `json:"contact_phone,omitempty"`

	// Optional: create the first login user for this client
	InitialUser *InitialUser `json:"initial_user,omitempty"`
}

type InitialUser struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Name     string `json:"name" binding:"required,min=2,max=100"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

type UpdateClientRequest struct {
	Name          *string   `json:"name,omitempty" binding:"omitempty,min=2,max=100"`
	TokenLimit    *int      `json:"token_limit,omitempty" binding:"omitempty,min=1000"`
	Branding      *Branding `json:"branding,omitempty"`
	Status        *string   `json:"status,omitempty"`
	ContactEmail  *string   `json:"contact_email,omitempty"`
	ContactPhone  *string   `json:"contact_phone,omitempty"`
}

type ClientUsageStats struct {
	Client          Client    `json:"client"`
	UsagePercentage float64   `json:"usage_percentage"`
	LastActivity    time.Time `json:"last_activity"`
	TotalMessages   int       `json:"total_messages"`
	ActiveUsers     int       `json:"active_users"`
}

type UsageAnalytics struct {
	TotalClients    int                `json:"total_clients"`
	TotalTokensUsed int                `json:"total_tokens_used"`
	TotalMessages   int                `json:"total_messages"`
	ActiveClients   int                `json:"active_clients"`
	ClientStats     []ClientUsageStats `json:"client_stats"`
	PeriodStart     time.Time          `json:"period_start"`
	PeriodEnd       time.Time          `json:"period_end"`
}

type TokenHistory struct {
    ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
    ClientID    primitive.ObjectID `bson:"client_id" json:"client_id"`
    OldLimit    int                `bson:"old_limit" json:"old_limit"`
    NewLimit    int                `bson:"new_limit" json:"new_limit"`
    Reason      string             `bson:"reason,omitempty" json:"reason,omitempty"`
    AdminUserID string             `bson:"admin_user_id,omitempty" json:"admin_user_id,omitempty"`
    Timestamp   time.Time          `bson:"timestamp" json:"timestamp"`
    Action      string             `bson:"action" json:"action"` // "reset", "increase", "decrease"
}