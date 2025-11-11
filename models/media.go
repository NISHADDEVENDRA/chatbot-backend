package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Media represents an uploaded media file (image, video, SVG)
type Media struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID     primitive.ObjectID `bson:"client_id" json:"client_id"`
	Filename     string             `bson:"filename" json:"filename"`
	OriginalName string             `bson:"original_name" json:"original_name"`
	FilePath     string             `bson:"file_path" json:"file_path"`
	FileHash     string             `bson:"file_hash" json:"file_hash"`
	FileSize     int64              `bson:"file_size" json:"file_size"`
	MimeType     string             `bson:"mime_type" json:"mime_type"`
	MediaType    string             `bson:"media_type" json:"media_type"` // image, video, svg
	Purpose      string             `bson:"purpose" json:"purpose"`       // launcher, avatar, etc.
	URL          string             `bson:"url" json:"url"`
	Status       string             `bson:"status" json:"status"` // active, deleted
	UploadedAt   time.Time          `bson:"uploaded_at" json:"uploaded_at"`
	Metadata     MediaMetadata      `bson:"metadata" json:"metadata"`
}

// MediaMetadata contains additional metadata about the media file
type MediaMetadata struct {
	Width       int    `bson:"width,omitempty" json:"width,omitempty"`
	Height      int    `bson:"height,omitempty" json:"height,omitempty"`
	Duration    int    `bson:"duration,omitempty" json:"duration,omitempty"`       // for videos, in seconds
	IsAnimated  bool   `bson:"is_animated,omitempty" json:"is_animated,omitempty"` // for SVGs
	ColorDepth  int    `bson:"color_depth,omitempty" json:"color_depth,omitempty"`
	Compression string `bson:"compression,omitempty" json:"compression,omitempty"`
}

// MediaUploadRequest represents a request to upload media
type MediaUploadRequest struct {
	MediaType string `form:"type" binding:"required,oneof=image video svg"`
	Purpose   string `form:"purpose" binding:"required,oneof=launcher avatar"`
}

// MediaUploadResponse represents the response after successful upload
type MediaUploadResponse struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	Type     string `json:"type"`
}


