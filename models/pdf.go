package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type PDF struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID      primitive.ObjectID `bson:"client_id" json:"client_id"`
	Filename      string             `bson:"filename" json:"filename"`
	ContentChunks []ContentChunk     `bson:"content_chunks" json:"content_chunks"`
	UploadedAt    time.Time          `bson:"uploaded_at" json:"uploaded_at"`
	Metadata      PDFMetadata        `bson:"metadata" json:"metadata"`
}

type ContentChunk struct {
	ChunkID string `bson:"chunk_id" json:"chunk_id"`
	Text    string `bson:"text" json:"text"`
	Order   int    `bson:"order" json:"order"`
}

type PDFMetadata struct {
	Size           int64         `bson:"size" json:"size"`
	Pages          int           `bson:"pages" json:"pages"`
	ProcessingTime time.Duration `bson:"processing_time" json:"processing_time"`
}

type UploadResponse struct {
	ID         string      `json:"id"`
	Filename   string      `json:"filename"`
	ChunkCount int         `json:"chunk_count"`
	Metadata   PDFMetadata `json:"metadata"`
}

type ChunkingConfig struct {
	MaxChunkSize int `json:"max_chunk_size"`
	Overlap      int `json:"overlap"`
}