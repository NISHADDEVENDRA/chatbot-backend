package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Client struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name           string             `bson:"name" json:"name" binding:"required,min=2,max=100"`
	Branding       Branding           `bson:"branding" json:"branding"`
	TokenLimit     int                `bson:"token_limit" json:"token_limit"`
	TokenUsed      int                `bson:"token_used" json:"token_used"`
	EmbedSecret    string             `bson:"embed_secret" json:"embed_secret"`
	AllowedOrigins []string           `bson:"allowed_origins" json:"allowed_origins"`                 // NEW: Whitelist of allowed origins
	Status         string             `bson:"status,omitempty" json:"status,omitempty"`               // optional, default "active"
	ContactEmail   string             `bson:"contact_email,omitempty" json:"contact_email,omitempty"` // optional
	ContactPhone   string             `bson:"contact_phone,omitempty" json:"contact_phone,omitempty"` // optional
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`

	// Migration flag
	MigratedToTenantDB bool `bson:"migrated_to_tenant_db,omitempty" json:"migrated_to_tenant_db,omitempty"`

	// Token Alert Fields
	AlertLevelSent   string    `bson:"alert_level_sent,omitempty" json:"alert_level_sent,omitempty"` // "none"|"warn"|"critical"|"exhausted"
	AlertLastSentAt  time.Time `bson:"alert_last_sent_at,omitempty" json:"alert_last_sent_at,omitempty"`
	QuotaPeriodStart time.Time `bson:"quota_period_start,omitempty" json:"quota_period_start,omitempty"`
	QuotaPeriodEnd   time.Time `bson:"quota_period_end,omitempty" json:"quota_period_end,omitempty"`

	// ✅ NEW: Domain Management Fields
	DomainWhitelist   []string `bson:"domain_whitelist,omitempty" json:"domain_whitelist,omitempty"`       // Allowed domains for embedding
	DomainBlacklist   []string `bson:"domain_blacklist,omitempty" json:"domain_blacklist,omitempty"`       // Blocked domains
	DomainMode        string   `bson:"domain_mode,omitempty" json:"domain_mode,omitempty"`                 // "whitelist" or "blacklist"
	RequireDomainAuth bool     `bson:"require_domain_auth,omitempty" json:"require_domain_auth,omitempty"` // Whether to enforce domain restrictions

	// AI Persona fields
	AIPersona *AIPersonaData `bson:"ai_persona,omitempty" json:"ai_persona,omitempty"` // PDF/DOC file info for AI persona

	// Calendly integration fields
	CalendlyURL     string `bson:"calendly_url,omitempty" json:"calendly_url,omitempty"`         // Calendly scheduling page URL
	CalendlyEnabled bool   `bson:"calendly_enabled,omitempty" json:"calendly_enabled,omitempty"` // Whether Calendly is enabled

	// QR Code integration fields
	QRCodeImageURL string `bson:"qr_code_image_url,omitempty" json:"qr_code_image_url,omitempty"` // QR code image URL for "Connect on Call"
	QRCodeEnabled  bool   `bson:"qr_code_enabled,omitempty" json:"qr_code_enabled,omitempty"`     // Whether QR code feature is enabled

	// WhatsApp QR Code integration fields
	WhatsAppQRCodeImageURL string `bson:"whatsapp_qr_code_image_url,omitempty" json:"whatsapp_qr_code_image_url,omitempty"` // WhatsApp QR code image URL for "Chat on WhatsApp"
	WhatsAppQRCodeEnabled  bool   `bson:"whatsapp_qr_code_enabled,omitempty" json:"whatsapp_qr_code_enabled,omitempty"`     // Whether WhatsApp QR code feature is enabled

	// Telegram QR Code integration fields
	TelegramQRCodeImageURL string `bson:"telegram_qr_code_image_url,omitempty" json:"telegram_qr_code_image_url,omitempty"` // Telegram QR code image URL for "Chat on Telegram"
	TelegramQRCodeEnabled  bool   `bson:"telegram_qr_code_enabled,omitempty" json:"telegram_qr_code_enabled,omitempty"`     // Whether Telegram QR code feature is enabled

	// Facebook Posts integration fields
	FacebookPostsEnabled bool `bson:"facebook_posts_enabled,omitempty" json:"facebook_posts_enabled,omitempty"` // Whether Facebook posts feature is enabled

	// Instagram Posts integration fields
	InstagramPostsEnabled bool `bson:"instagram_posts_enabled,omitempty" json:"instagram_posts_enabled,omitempty"` // Whether Instagram posts feature is enabled

	// Website Embed integration fields
	WebsiteEmbedURL     string `bson:"website_embed_url,omitempty" json:"website_embed_url,omitempty"`         // Website URL to embed
	WebsiteEmbedEnabled bool   `bson:"website_embed_enabled,omitempty" json:"website_embed_enabled,omitempty"` // Whether website embed feature is enabled

	// Client Permissions - Controls what client can see and access
	Permissions ClientPermissions `bson:"permissions,omitempty" json:"permissions,omitempty"`
}

// AIPersonaData represents uploaded persona file information
type AIPersonaData struct {
	Filename       string    `bson:"filename,omitempty" json:"filename,omitempty"`
	Size           int64     `bson:"size,omitempty" json:"size,omitempty"`
	UploadedAt     time.Time `bson:"uploaded_at,omitempty" json:"uploaded_at,omitempty"`
	Content        string    `bson:"content,omitempty" json:"content,omitempty"`
	Pages          int       `bson:"pages,omitempty" json:"pages,omitempty"`
	WordCount      int       `bson:"word_count,omitempty" json:"word_count,omitempty"`
	CharacterCount int       `bson:"character_count,omitempty" json:"character_count,omitempty"`
}

type Branding struct {
	LogoURL        string   `bson:"logo_url" json:"logo_url"`
	ThemeColor     string   `bson:"theme_color" json:"theme_color"`
	WelcomeMessage string   `bson:"welcome_message" json:"welcome_message"`
	PreQuestions   []string `bson:"pre_questions" json:"pre_questions" binding:"max=5"` // ← allow up to 5
	AllowEmbedding bool     `bson:"allow_embedding" json:"allow_embedding"`
	ShowPoweredBy  bool     `bson:"show_powered_by" json:"show_powered_by"`
	WidgetPosition string   `bson:"widget_position,omitempty" json:"widget_position,omitempty"`
	EmbedMode      string   `bson:"embed_mode,omitempty" json:"embed_mode,omitempty"` // "widget" or "fullscreen"

	// Launcher configuration
	LauncherColor     string `bson:"launcher_color,omitempty" json:"launcher_color,omitempty"`
	LauncherText      string `bson:"launcher_text,omitempty" json:"launcher_text,omitempty"`
	LauncherIcon      string `bson:"launcher_icon,omitempty" json:"launcher_icon,omitempty"`
	LauncherImageURL  string `bson:"launcher_image_url,omitempty" json:"launcher_image_url,omitempty"`
	LauncherVideoURL  string `bson:"launcher_video_url,omitempty" json:"launcher_video_url,omitempty"`
	LauncherSVGURL    string `bson:"launcher_svg_url,omitempty" json:"launcher_svg_url,omitempty"`
	LauncherIconColor string `bson:"launcher_icon_color,omitempty" json:"launcher_icon_color,omitempty"`

	// Cancel icon configuration
	CancelIcon      string `bson:"cancel_icon,omitempty" json:"cancel_icon,omitempty"`
	CancelImageURL  string `bson:"cancel_image_url,omitempty" json:"cancel_image_url,omitempty"`
	CancelIconColor string `bson:"cancel_icon_color,omitempty" json:"cancel_icon_color,omitempty"`

	// AI Avatar configuration
	AIAvatarType      string `bson:"ai_avatar_type,omitempty" json:"ai_avatar_type,omitempty"`
	ShowWelcomeAvatar bool   `bson:"show_welcome_avatar,omitempty" json:"show_welcome_avatar,omitempty"`
	ShowChatAvatar    bool   `bson:"show_chat_avatar,omitempty" json:"show_chat_avatar,omitempty"`
	ShowTypingAvatar  bool   `bson:"show_typing_avatar,omitempty" json:"show_typing_avatar,omitempty"`
}

type CreateClientRequest struct {
	Name         string   `json:"name" binding:"required,min=2,max=100"`
	TokenLimit   int      `json:"token_limit" binding:"required,min=1000"`
	Branding     Branding `json:"branding"`
	Status       string   `json:"status,omitempty"`
	ContactEmail string   `json:"contact_email,omitempty"`
	ContactPhone string   `json:"contact_phone,omitempty"`

	// Optional: create the first login user for this client
	InitialUser *InitialUser `json:"initial_user,omitempty"`
}

type InitialUser struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Name     string `json:"name" binding:"required,min=2,max=100"`
	Email    string `json:"email" binding:"required,email"`
	Phone    string `json:"phone" binding:"required,min=10,max=20"`
	Role     string `json:"role,omitempty"`
}

type UpdateClientRequest struct {
	Name         *string   `json:"name,omitempty" binding:"omitempty,min=2,max=100"`
	TokenLimit   *int      `json:"token_limit,omitempty" binding:"omitempty,min=1000"`
	Branding     *Branding `json:"branding,omitempty"`
	Status       *string   `json:"status,omitempty"`
	ContactEmail *string   `json:"contact_email,omitempty"`
	ContactPhone *string   `json:"contact_phone,omitempty"`
}

// ✅ NEW: Domain Management Types
type DomainManagementRequest struct {
	ClientID          string   `json:"client_id" binding:"required"`
	DomainWhitelist   []string `json:"domain_whitelist,omitempty"`
	DomainBlacklist   []string `json:"domain_blacklist,omitempty"`
	DomainMode        string   `json:"domain_mode,omitempty" binding:"omitempty,oneof=whitelist blacklist"`
	RequireDomainAuth *bool    `json:"require_domain_auth,omitempty"`
}

type DomainManagementResponse struct {
	ClientID          string    `json:"client_id"`
	DomainWhitelist   []string  `json:"domain_whitelist"`
	DomainBlacklist   []string  `json:"domain_blacklist"`
	DomainMode        string    `json:"domain_mode"`
	RequireDomainAuth bool      `json:"require_domain_auth"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SuspiciousActivityAlert struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID   primitive.ObjectID `bson:"client_id" json:"client_id"`
	Domain     string             `bson:"domain" json:"domain"`
	IPAddress  string             `bson:"ip_address" json:"ip_address"`
	UserAgent  string             `bson:"user_agent" json:"user_agent"`
	Referrer   string             `bson:"referrer" json:"referrer"`
	AlertType  string             `bson:"alert_type" json:"alert_type"` // "unauthorized_domain", "suspicious_activity"
	Severity   string             `bson:"severity" json:"severity"`     // "low", "medium", "high", "critical"
	Message    string             `bson:"message" json:"message"`
	Resolved   bool               `bson:"resolved" json:"resolved"`
	ResolvedAt *time.Time         `bson:"resolved_at,omitempty" json:"resolved_at,omitempty"`
	ResolvedBy string             `bson:"resolved_by,omitempty" json:"resolved_by,omitempty"`
	CreatedAt  time.Time          `bson:"created_at" json:"created_at"`
}

type ClientUsageStats struct {
	Client          Client    `json:"client"`
	UsagePercentage float64   `json:"usage_percentage"`
	LastActivity    time.Time `json:"last_activity"`
	TotalMessages   int       `json:"total_messages"`
	ActiveUsers     int       `json:"active_users"`

	// ✅ ADD THESE TWO NEW FIELDS
	ClientUsers  int `bson:"client_users" json:"client_users"`   // New field
	VisitorUsers int `bson:"visitor_users" json:"visitor_users"` // New field
}

type DailyUsageData struct {
	Date                string `json:"date"`
	Tokens              int    `json:"tokens"`
	Messages            int    `json:"messages"`
	ActiveUsers         int    `json:"active_users"`
	TotalConversations int    `json:"total_conversations"`
}

type HourlyUsageData struct {
	Hour                string `json:"hour"`
	Label               string `json:"label"`
	Tokens              int    `json:"tokens"`
	Messages            int    `json:"messages"`
	ActiveUsers         int    `json:"active_users"`
	TotalConversations int    `json:"total_conversations"`
}

type UsageAnalytics struct {
	TotalClients        int                `json:"total_clients"`
	TotalTokensUsed     int                `json:"total_tokens_used"`
	TotalMessages       int                `json:"total_messages"`
	ActiveClients       int                `json:"active_clients"`
	ActiveUsers         int                `json:"active_users"`
	ClientStats         []ClientUsageStats `json:"client_stats"`
	DailyUsage          []DailyUsageData  `json:"daily_usage"`
	HourlyUsage         []HourlyUsageData `json:"hourly_usage"`
	SystemUptime        float64            `json:"system_uptime"`        // Percentage
	AverageResponseTime float64            `json:"average_response_time"` // Milliseconds
	ErrorRate           float64            `json:"error_rate"`            // Percentage
	PeriodStart         time.Time          `json:"period_start"`
	PeriodEnd           time.Time          `json:"period_end"`
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

// ClientPermissions controls what navigation items and features a client can access
type ClientPermissions struct {
	// AllowedNavigationItems - Navigation items client can see in sidebar
	// If empty, all items are allowed (backward compatible)
	AllowedNavigationItems []string `bson:"allowed_navigation_items,omitempty" json:"allowed_navigation_items,omitempty"`
	
	// EnabledFeatures - Features client can access
	// Auto-populated based on AllowedNavigationItems
	// If empty, all features are enabled (backward compatible)
	EnabledFeatures []string `bson:"enabled_features,omitempty" json:"enabled_features,omitempty"`
}

// UpdateClientPermissionsRequest - Request to update client permissions
type UpdateClientPermissionsRequest struct {
	AllowedNavigationItems []string `json:"allowed_navigation_items,omitempty"`
}
