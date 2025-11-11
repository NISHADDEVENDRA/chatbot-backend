package services

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"saas-chatbot-platform/models"
)

type MediaService struct {
	mediaCollection *mongo.Collection
	storagePath     string
}

func NewMediaService(db *mongo.Database, storagePath string) *MediaService {
	return &MediaService{
		mediaCollection: db.Collection("media"),
		storagePath:     storagePath,
	}
}

// UploadMedia handles media file uploads
func (s *MediaService) UploadMedia(ctx context.Context, clientID primitive.ObjectID, file *multipart.FileHeader, mediaType, purpose string) (*models.Media, error) {
	// Validate file
	if err := s.validateFile(file, mediaType); err != nil {
		return nil, err
	}

	// Open file
	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer src.Close()

	// Calculate file hash
	hash := md5.New()
	if _, err := io.Copy(hash, src); err != nil {
		return nil, fmt.Errorf("failed to calculate hash: %w", err)
	}
	fileHash := fmt.Sprintf("%x", hash.Sum(nil))

	// Check for duplicates
	existingMedia, err := s.findDuplicate(ctx, clientID, fileHash)
	if err != nil {
		return nil, fmt.Errorf("duplicate check failed: %w", err)
	}
	if existingMedia != nil {
		return existingMedia, nil
	}

	// Reset file pointer
	src.Seek(0, 0)

	// Create storage path
	storageDir := filepath.Join(s.storagePath, "media", clientID.Hex())
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Generate secure filename
	ext := filepath.Ext(file.Filename)
	secureFilename := fmt.Sprintf("%s_%d%s", fileHash[:8], time.Now().Unix(), ext)
	filePath := filepath.Join(storageDir, secureFilename)

	// Save file
	dst, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(filePath) // Clean up on error
		return nil, fmt.Errorf("failed to save file: %w", err)
	}

	// Create media document
	media := &models.Media{
		ID:           primitive.NewObjectID(),
		ClientID:     clientID,
		Filename:     secureFilename,
		OriginalName: file.Filename,
		FilePath:     filePath,
		FileHash:     fileHash,
		FileSize:     file.Size,
		MimeType:     file.Header.Get("Content-Type"),
		MediaType:    mediaType,
		Purpose:      purpose,
		URL:          fmt.Sprintf("/media/%s/%s", clientID.Hex(), secureFilename),
		Status:       "active",
		UploadedAt:   time.Now(),
		Metadata:     s.extractMetadata(file, mediaType),
	}

	// Save to database
	if _, err := s.mediaCollection.InsertOne(ctx, media); err != nil {
		os.Remove(filePath) // Clean up on error
		return nil, fmt.Errorf("failed to save media record: %w", err)
	}

	return media, nil
}

// validateFile validates the uploaded file
func (s *MediaService) validateFile(file *multipart.FileHeader, mediaType string) error {
	// Check file size (5MB max)
	const maxSize = 5 * 1024 * 1024
	if file.Size > maxSize {
		return fmt.Errorf("file size exceeds 5MB limit")
	}

	// Check file type
	mimeType := file.Header.Get("Content-Type")
	switch mediaType {
	case "image":
		allowedTypes := []string{"image/jpeg", "image/jpg", "image/png", "image/gif", "image/webp"}
		if !contains(allowedTypes, mimeType) {
			return fmt.Errorf("invalid image type: %s", mimeType)
		}
	case "video":
		allowedTypes := []string{"video/mp4", "video/webm", "video/ogg"}
		if !contains(allowedTypes, mimeType) {
			return fmt.Errorf("invalid video type: %s", mimeType)
		}
	case "svg":
		if mimeType != "image/svg+xml" {
			return fmt.Errorf("invalid SVG type: %s", mimeType)
		}
	default:
		return fmt.Errorf("unsupported media type: %s", mediaType)
	}

	return nil
}

// findDuplicate checks for existing media with the same hash
func (s *MediaService) findDuplicate(ctx context.Context, clientID primitive.ObjectID, fileHash string) (*models.Media, error) {
	var media models.Media
	err := s.mediaCollection.FindOne(ctx, map[string]interface{}{
		"client_id": clientID,
		"file_hash": fileHash,
		"status":    "active",
	}).Decode(&media)

	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &media, nil
}

// extractMetadata extracts metadata from the file
func (s *MediaService) extractMetadata(file *multipart.FileHeader, mediaType string) models.MediaMetadata {
	metadata := models.MediaMetadata{}

	// For now, we'll set basic metadata
	// In a production environment, you might want to use libraries like:
	// - github.com/disintegration/imaging for image metadata
	// - github.com/3d0c/gmf for video metadata
	// - github.com/srwiley/oksvg for SVG metadata

	if mediaType == "svg" {
		// Check if SVG contains animation elements
		// This is a simplified check - in production, you'd parse the SVG
		metadata.IsAnimated = strings.Contains(file.Filename, "animated") ||
			strings.Contains(file.Filename, "anim")
	}

	return metadata
}

// GetMediaByID retrieves media by ID
func (s *MediaService) GetMediaByID(ctx context.Context, mediaID primitive.ObjectID) (*models.Media, error) {
	var media models.Media
	err := s.mediaCollection.FindOne(ctx, map[string]interface{}{
		"_id":    mediaID,
		"status": "active",
	}).Decode(&media)

	if err != nil {
		return nil, err
	}

	return &media, nil
}

// GetMediaByClient retrieves all media for a client
func (s *MediaService) GetMediaByClient(ctx context.Context, clientID primitive.ObjectID, purpose string) ([]*models.Media, error) {
	filter := map[string]interface{}{
		"client_id": clientID,
		"status":    "active",
	}

	if purpose != "" {
		filter["purpose"] = purpose
	}

	cursor, err := s.mediaCollection.Find(ctx, filter, options.Find().SetSort(map[string]interface{}{
		"uploaded_at": -1,
	}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var media []*models.Media
	if err := cursor.All(ctx, &media); err != nil {
		return nil, err
	}

	return media, nil
}

// DeleteMedia soft deletes media
func (s *MediaService) DeleteMedia(ctx context.Context, mediaID primitive.ObjectID) error {
	_, err := s.mediaCollection.UpdateOne(ctx,
		map[string]interface{}{"_id": mediaID},
		map[string]interface{}{"$set": map[string]interface{}{
			"status": "deleted",
		}},
	)
	return err
}

// CleanupDeletedMedia removes files for deleted media records
func (s *MediaService) CleanupDeletedMedia(ctx context.Context) error {
	// Find all deleted media
	cursor, err := s.mediaCollection.Find(ctx, map[string]interface{}{
		"status": "deleted",
	})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var deletedMedia []*models.Media
	if err := cursor.All(ctx, &deletedMedia); err != nil {
		return err
	}

	// Remove files and delete records
	for _, media := range deletedMedia {
		if err := os.Remove(media.FilePath); err != nil && !os.IsNotExist(err) {
			// Log error but continue
			fmt.Printf("Failed to remove file %s: %v\n", media.FilePath, err)
		}

		// Delete the record
		s.mediaCollection.DeleteOne(ctx, map[string]interface{}{"_id": media.ID})
	}

	return nil
}

// GetFilePath returns the file path for a given client ID and filename
func (s *MediaService) GetFilePath(clientID primitive.ObjectID, filename string) string {
	return filepath.Join(s.storagePath, "media", clientID.Hex(), filename)
}

// Helper function to check if slice contains string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
