package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CrawlJob represents a web crawling job
type CrawlJob struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID     primitive.ObjectID `bson:"client_id" json:"client_id"`
	URL          string             `bson:"url" json:"url"`
	Status       string             `bson:"status" json:"status"` // pending, crawling, completed, failed
	Progress     int                `bson:"progress" json:"progress"`
	Title        string             `bson:"title,omitempty" json:"title,omitempty"`
	Content      string             `bson:"content,omitempty" json:"content,omitempty"`
	PagesFound   int                `bson:"pages_found" json:"pages_found"`
	PagesCrawled int                `bson:"pages_crawled" json:"pages_crawled"`
	Error        string             `bson:"error,omitempty" json:"error,omitempty"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
	CompletedAt  *time.Time         `bson:"completed_at,omitempty" json:"completed_at,omitempty"`

	// Crawling configuration
	MaxPages       int      `bson:"max_pages,omitempty" json:"max_pages,omitempty"`
	AllowedDomains []string `bson:"allowed_domains,omitempty" json:"allowed_domains,omitempty"`
	AllowedPaths   []string `bson:"allowed_paths,omitempty" json:"allowed_paths,omitempty"`
	FollowLinks    bool     `bson:"follow_links" json:"follow_links"`
	IncludeImages  bool     `bson:"include_images" json:"include_images"`
	RespectRobots  bool     `bson:"respect_robots" json:"respect_robots"`

	// Extracted data
	CrawledPages  []CrawledPage  `bson:"crawled_pages,omitempty" json:"crawled_pages,omitempty"`
	Products      []Product      `bson:"products,omitempty" json:"products,omitempty"`
	ContentChunks []ContentChunk `bson:"content_chunks,omitempty" json:"content_chunks,omitempty"`

	// Processing metadata
	TotalTokens    int           `bson:"total_tokens,omitempty" json:"total_tokens,omitempty"`
	ProcessingTime time.Duration `bson:"processing_time,omitempty" json:"processing_time,omitempty"`

	// Additional metadata for tracking
	StartTime  *time.Time `bson:"start_time,omitempty" json:"start_time,omitempty"`
	EndTime    *time.Time `bson:"end_time,omitempty" json:"end_time,omitempty"`
	RetryCount int        `bson:"retry_count,omitempty" json:"retry_count,omitempty"`
}

// CrawledPage represents a single crawled page
type CrawledPage struct {
	URL        string    `bson:"url" json:"url"`
	Title      string    `bson:"title" json:"title"`
	Content    string    `bson:"content" json:"content"`
	CrawledAt  time.Time `bson:"crawled_at" json:"crawled_at"`
	StatusCode int       `bson:"status_code" json:"status_code"`
	Size       int64     `bson:"size" json:"size"`
	WordCount  int       `bson:"word_count,omitempty" json:"word_count,omitempty"`
}

// Product represents extracted product data from eCommerce sites
type Product struct {
	Name        string                 `bson:"name" json:"name"`
	Price       string                 `bson:"price,omitempty" json:"price,omitempty"`
	SKU         string                 `bson:"sku,omitempty" json:"sku,omitempty"`
	Description string                 `bson:"description,omitempty" json:"description,omitempty"`
	ImageURL    string                 `bson:"image_url,omitempty" json:"image_url,omitempty"`
	URL         string                 `bson:"url" json:"url"`
	Category    string                 `bson:"category,omitempty" json:"category,omitempty"`
	InStock     bool                   `bson:"in_stock,omitempty" json:"in_stock,omitempty"`
	Rating      float64                `bson:"rating,omitempty" json:"rating,omitempty"`
	Attributes  map[string]interface{} `bson:"attributes,omitempty" json:"attributes,omitempty"`
	ExtractedAt time.Time              `bson:"extracted_at" json:"extracted_at"`
}

// CrawlStatus constants
const (
	CrawlStatusPending   = "pending"
	CrawlStatusCrawling  = "crawling"
	CrawlStatusCompleted = "completed"
	CrawlStatusFailed    = "failed"
	CrawlStatusCancelled = "cancelled"
)
