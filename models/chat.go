// models/chat.go
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ✅ UPDATED: Your existing Message model with fixes
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
	UserName       string             `bson:"user_name,omitempty" json:"user_name,omitempty"`   // ✅ Fixed
	UserEmail      string             `bson:"user_email,omitempty" json:"user_email,omitempty"` // ✅ Fixed

	// Contact collection state
	ContactCollectionPhase string `bson:"contact_collection_phase,omitempty" json:"contact_collection_phase,omitempty"` // 'none', 'awaiting_name', 'awaiting_email', 'completed'
	ChatDisabled           bool   `bson:"chat_disabled,omitempty" json:"chat_disabled,omitempty"`                       // Whether chat is disabled after contact collection

	// ✅ NEW: IP tracking and user identification for embed users
	UserIP      string `bson:"user_ip,omitempty" json:"user_ip,omitempty"`
	UserAgent   string `bson:"user_agent,omitempty" json:"user_agent,omitempty"`
	Referrer    string `bson:"referrer,omitempty" json:"referrer,omitempty"`
	SessionID   string `bson:"session_id,omitempty" json:"session_id,omitempty"`
	IsEmbedUser bool   `bson:"is_embed_user" json:"is_embed_user"`

	// ✅ ENHANCED: Comprehensive geolocation data (95% accurate)
	Country      string  `bson:"country,omitempty" json:"country,omitempty"`           // Country name
	CountryCode  string  `bson:"country_code,omitempty" json:"country_code,omitempty"` // Country code (e.g., "US", "IN")
	Region       string  `bson:"region,omitempty" json:"region,omitempty"`             // State/Province code
	RegionName   string  `bson:"region_name,omitempty" json:"region_name,omitempty"`   // State/Province name
	City         string  `bson:"city,omitempty" json:"city,omitempty"`                 // City name (50-80% accuracy)
	Latitude     float64 `bson:"latitude,omitempty" json:"latitude,omitempty"`         // Latitude for maps/analytics
	Longitude    float64 `bson:"longitude,omitempty" json:"longitude,omitempty"`       // Longitude for maps/analytics
	Timezone     string  `bson:"timezone,omitempty" json:"timezone,omitempty"`         // User's timezone
	ISP          string  `bson:"isp,omitempty" json:"isp,omitempty"`                   // Internet Service Provider
	Organization string  `bson:"organization,omitempty" json:"organization,omitempty"` // Organization/Company
	IPType       string  `bson:"ip_type,omitempty" json:"ip_type,omitempty"`           // Residential/Datacenter/VPN/Proxy
}

// ✅ UPDATED: Your existing ChatRequest with fixes
type ChatRequest struct {
	Message        string `json:"message" binding:"required,min=1,max=2000"`
	ConversationID string `json:"conversation_id,omitempty"` // ✅ Fixed
}

// ✅ UPDATED: Your existing ChatResponse with fixes
type ChatResponse struct {
	Reply           string    `json:"reply"`
	TokensUsed      int       `json:"tokens_used"`      // ✅ Fixed
	RemainingTokens int       `json:"remaining_tokens"` // ✅ Fixed
	ConversationID  string    `json:"conversation_id"`  // ✅ Fixed
	Timestamp       time.Time `json:"timestamp"`
}

// ✅ UPDATED: Your existing ConversationHistory with fixes
type ConversationHistory struct {
	ConversationID string    `json:"conversation_id"` // ✅ Fixed
	Messages       []Message `json:"messages"`
	TotalTokens    int       `json:"total_tokens"` // ✅ Fixed
	CreatedAt      time.Time `json:"created_at"`   // ✅ Fixed
	UpdatedAt      time.Time `json:"updated_at"`   // ✅ Fixed
}

// ✅ ADDED: Conversation model to track chat sessions
type Conversation struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"user_id" json:"user_id"`
	UserName  string             `bson:"user_name" json:"user_name"`
	UserEmail string             `bson:"user_email,omitempty" json:"user_email,omitempty"`
	ClientID  primitive.ObjectID `bson:"client_id" json:"client_id"`
	Title     string             `bson:"title,omitempty" json:"title,omitempty"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// ✅ ADDED: Individual message model for better structure
type ChatMessage struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ConversationID string             `bson:"conversation_id" json:"conversation_id"`
	UserID         primitive.ObjectID `bson:"user_id" json:"user_id"`
	UserName       string             `bson:"user_name" json:"user_name"`
	UserEmail      string             `bson:"user_email,omitempty" json:"user_email,omitempty"`
	ClientID       primitive.ObjectID `bson:"client_id" json:"client_id"`
	Role           string             `bson:"role" json:"role"` // "user" or "assistant"
	Content        string             `bson:"content" json:"content"`
	TokenCost      int                `bson:"token_cost,omitempty" json:"token_cost,omitempty"`
	Timestamp      time.Time          `bson:"timestamp" json:"timestamp"`
}

// ✅ ADDED: User name storage by IP for cross-conversation name persistence
type UserNameByIP struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserIP    string             `bson:"user_ip" json:"user_ip"`
	UserName  string             `bson:"user_name" json:"user_name"`
	UserEmail string             `bson:"user_email,omitempty" json:"user_email,omitempty"`
	ClientID  primitive.ObjectID `bson:"client_id" json:"client_id"`
	FirstSeen time.Time          `bson:"first_seen" json:"first_seen"`
	LastSeen  time.Time          `bson:"last_seen" json:"last_seen"`
	Count     int                `bson:"count" json:"count"` // Number of conversations from this IP
}

// ✅ ADDED: Message feedback model for thumbs up/down
type MessageFeedback struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	MessageID        primitive.ObjectID `bson:"message_id" json:"message_id"`
	FeedbackType     string             `bson:"feedback_type" json:"feedback_type"` // "positive" or "negative"
	Comment          string             `bson:"comment,omitempty" json:"comment,omitempty"`
	Timestamp        time.Time          `bson:"timestamp" json:"timestamp"`
	UserIP           string             `bson:"user_ip,omitempty" json:"user_ip,omitempty"`
	SessionID        string             `bson:"session_id,omitempty" json:"session_id,omitempty"`
	ClientID         primitive.ObjectID `bson:"client_id" json:"client_id"`
	ConversationID   string             `bson:"conversation_id,omitempty" json:"conversation_id,omitempty"`
	ConversationContext string          `bson:"conversation_context,omitempty" json:"conversation_context,omitempty"` // Last few messages
	
	// ✅ ENHANCED: Detailed feedback fields
	IssueCategory    string             `bson:"issue_category,omitempty" json:"issue_category,omitempty"` // "wrong_answer", "unclear", "incomplete", "irrelevant", "too_generic", "repetitive", "technical_error"
	UserMessage      string             `bson:"user_message,omitempty" json:"user_message,omitempty"` // Original user message
	AIResponse       string             `bson:"ai_response,omitempty" json:"ai_response,omitempty"` // AI response that received feedback
	Analyzed         bool               `bson:"analyzed" json:"analyzed"` // Whether feedback has been analyzed
	AnalysisDate     time.Time          `bson:"analysis_date,omitempty" json:"analysis_date,omitempty"`
	QualityScore     float64            `bson:"quality_score,omitempty" json:"quality_score,omitempty"` // 0-1 quality score
	InsightCreated   bool               `bson:"insight_created,omitempty" json:"insight_created,omitempty"` // Whether this feedback has been used to create an insight
}

// ✅ ADDED: Performance metrics model for response time tracking
type PerformanceMetrics struct {
	ID                   primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Timestamp            time.Time          `bson:"timestamp" json:"timestamp"`
	ClientID             primitive.ObjectID `bson:"client_id" json:"client_id"`
	SessionID            string             `bson:"session_id,omitempty" json:"session_id,omitempty"`
	TotalTimeMs          int                `bson:"total_time_ms" json:"total_time_ms"`
	Phases               PhaseTimings        `bson:"phases" json:"phases"`
	TokenCount           int                `bson:"token_count" json:"token_count"`
	Model                string             `bson:"model,omitempty" json:"model,omitempty"`
	Status               string             `bson:"status" json:"status"` // "success" or "error"
	ErrorMessage         string             `bson:"error_message,omitempty" json:"error_message,omitempty"`
	MessageLength        int                `bson:"message_length,omitempty" json:"message_length,omitempty"`
	ResponseLength       int                `bson:"response_length,omitempty" json:"response_length,omitempty"`
}

// PhaseTimings represents timing breakdown for different phases
type PhaseTimings struct {
	ContextRetrievalMs int `bson:"context_retrieval_ms" json:"context_retrieval_ms"`
	HistoryLoadingMs   int `bson:"history_loading_ms" json:"history_loading_ms"`
	SummarizationMs    int `bson:"summarization_ms" json:"summarization_ms"`
	PromptBuildingMs   int `bson:"prompt_building_ms" json:"prompt_building_ms"`
	AIGenerationMs     int `bson:"ai_generation_ms" json:"ai_generation_ms"`
	ValidationMs       int `bson:"validation_ms" json:"validation_ms"`
}

// ✅ ADDED: Quality metrics model for tracking feedback quality
type QualityMetrics struct {
	ID                  primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID            primitive.ObjectID `bson:"client_id" json:"client_id"`
	Period              string             `bson:"period" json:"period"` // "daily", "weekly", "monthly"
	PeriodStart         time.Time          `bson:"period_start" json:"period_start"`
	PeriodEnd           time.Time          `bson:"period_end" json:"period_end"`
	TotalFeedback       int                `bson:"total_feedback" json:"total_feedback"`
	PositiveFeedback    int                `bson:"positive_feedback" json:"positive_feedback"`
	NegativeFeedback    int                `bson:"negative_feedback" json:"negative_feedback"`
	SatisfactionRate    float64            `bson:"satisfaction_rate" json:"satisfaction_rate"` // 0-1
	IssueDistribution   map[string]int     `bson:"issue_distribution" json:"issue_distribution"` // Map of issue category to count
	TopicDistribution   map[string]int     `bson:"topic_distribution" json:"topic_distribution"` // Map of topic to feedback count
	AverageQualityScore float64            `bson:"average_quality_score" json:"average_quality_score"`
	CreatedAt           time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt           time.Time          `bson:"updated_at" json:"updated_at"`
}

// ✅ ADDED: Feedback insights model for storing analyzed feedback patterns
type FeedbackInsight struct {
	ID                  primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID            primitive.ObjectID `bson:"client_id" json:"client_id"`
	InsightType         string             `bson:"insight_type" json:"insight_type"` // "common_issue", "topic_issue", "trend", "pattern"
	Title               string             `bson:"title" json:"title"`
	Description         string             `bson:"description" json:"description"`
	Severity            string             `bson:"severity" json:"severity"` // "low", "medium", "high", "critical"
	AffectedTopics      []string           `bson:"affected_topics" json:"affected_topics"`
	IssueCategory       string             `bson:"issue_category,omitempty" json:"issue_category,omitempty"`
	FeedbackCount       int                `bson:"feedback_count" json:"feedback_count"`
	Recommendation      string             `bson:"recommendation,omitempty" json:"recommendation,omitempty"`
	ExampleFeedbacks    []FeedbackExample  `bson:"example_feedbacks,omitempty" json:"example_feedbacks,omitempty"` // User questions and bot answers that received negative feedback
	CreatedAt           time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt           time.Time          `bson:"updated_at" json:"updated_at"`
	Resolved            bool               `bson:"resolved" json:"resolved"`
	ResolvedAt          time.Time          `bson:"resolved_at,omitempty" json:"resolved_at,omitempty"`
}

// FeedbackExample stores example user question and bot answer for an insight
type FeedbackExample struct {
	UserMessage string    `bson:"user_message" json:"user_message"`
	AIResponse  string    `bson:"ai_response" json:"ai_response"`
	Comment     string    `bson:"comment,omitempty" json:"comment,omitempty"`
	Timestamp   time.Time `bson:"timestamp" json:"timestamp"`
}