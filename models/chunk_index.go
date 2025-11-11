package models

import "go.mongodb.org/mongo-driver/bson/primitive"

// PDFChunkIndex is a denormalized chunk for Atlas Search/VectorSearch.
// Keeping a separate collection enables efficient $search/$vectorSearch.
type PDFChunkIndex struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	ClientID primitive.ObjectID `bson:"client_id"`
	PDFID    primitive.ObjectID `bson:"pdf_id"`
	ChunkID  string             `bson:"chunk_id"`
	Order    int                `bson:"order"`
	Text     string             `bson:"text"`
	Keywords []string           `bson:"keywords,omitempty"`
	Vector   []float32          `bson:"vector,omitempty"`
	Language string             `bson:"language,omitempty"`
	Topic    string             `bson:"topic,omitempty"`
}
