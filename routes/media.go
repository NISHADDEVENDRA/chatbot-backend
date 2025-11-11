package routes

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"saas-chatbot-platform/models"
	"saas-chatbot-platform/services"
)

// HandleMediaUpload handles media file uploads
func HandleMediaUpload(mediaService *services.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Debug: Log all headers
		fmt.Printf("Headers: %+v\n", c.Request.Header)

		// Get client ID from context (set by auth middleware)
		clientIDStr, exists := c.Get("client_id")
		if !exists {
			fmt.Printf("Client ID not found in context. Available keys: %+v\n", c.Keys)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Client ID not found"})
			return
		}

		clientID, err := primitive.ObjectIDFromHex(clientIDStr.(string))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}

		// Parse form data
		mediaType := c.PostForm("type")
		purpose := c.PostForm("purpose")

		if mediaType == "" {
			mediaType = "image" // default
		}
		if purpose == "" {
			purpose = "launcher" // default
		}

		// Get uploaded file
		file, err := c.FormFile("media")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
			return
		}

		// Upload media
		media, err := mediaService.UploadMedia(c.Request.Context(), clientID, file, mediaType, purpose)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return response
		response := models.MediaUploadResponse{
			ID:       media.ID.Hex(),
			Filename: media.Filename,
			URL:      media.URL,
			Size:     media.FileSize,
			Type:     media.MediaType,
		}

		c.JSON(http.StatusOK, response)
	}
}

// HandleLauncherMediaUpload handles launcher-specific media uploads
func HandleLauncherMediaUpload(mediaService *services.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Debug: Log all headers
		fmt.Printf("Launcher Media Upload - Headers: %+v\n", c.Request.Header)

		// Get client ID from context
		clientIDStr, exists := c.Get("client_id")
		if !exists {
			fmt.Printf("Launcher Media Upload - Client ID not found in context. Available keys: %+v\n", c.Keys)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Client ID not found"})
			return
		}

		clientID, err := primitive.ObjectIDFromHex(clientIDStr.(string))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}

		// Parse form data
		mediaType := c.PostForm("type")
		if mediaType == "" {
			mediaType = "image" // default
		}

		// Get uploaded file
		file, err := c.FormFile("media")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
			return
		}

		// Upload media with launcher purpose
		media, err := mediaService.UploadMedia(c.Request.Context(), clientID, file, mediaType, "launcher")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return response
		response := models.MediaUploadResponse{
			ID:       media.ID.Hex(),
			Filename: media.Filename,
			URL:      media.URL,
			Size:     media.FileSize,
			Type:     media.MediaType,
		}

		c.JSON(http.StatusOK, response)
	}
}

// HandleGetMediaList retrieves media list for a client
func HandleGetMediaList(mediaService *services.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get client ID from context
		clientIDStr, exists := c.Get("client_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Client ID not found"})
			return
		}

		clientID, err := primitive.ObjectIDFromHex(clientIDStr.(string))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}

		// Get query parameters
		purpose := c.Query("purpose")
		mediaType := c.Query("type")
		limitStr := c.DefaultQuery("limit", "50")
		offsetStr := c.DefaultQuery("offset", "0")

		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			limit = 50
		}
		if limit > 100 {
			limit = 100
		}

		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			offset = 0
		}

		// Get media list
		media, err := mediaService.GetMediaByClient(c.Request.Context(), clientID, purpose)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Filter by type if specified
		if mediaType != "" {
			var filtered []*models.Media
			for _, m := range media {
				if m.MediaType == mediaType {
					filtered = append(filtered, m)
				}
			}
			media = filtered
		}

		// Apply pagination
		total := len(media)
		start := offset
		end := offset + limit
		if start > total {
			start = total
		}
		if end > total {
			end = total
		}

		if start < len(media) {
			media = media[start:end]
		} else {
			media = []*models.Media{}
		}

		c.JSON(http.StatusOK, gin.H{
			"media":  media,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// HandleGetMedia retrieves a specific media file
func HandleGetMedia(mediaService *services.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		mediaIDStr := c.Param("id")
		mediaID, err := primitive.ObjectIDFromHex(mediaIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid media ID"})
			return
		}

		media, err := mediaService.GetMediaByID(c.Request.Context(), mediaID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{"error": "Media not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, media)
	}
}

// HandleDeleteMedia deletes a media file
func HandleDeleteMedia(mediaService *services.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get client ID from context
		clientIDStr, exists := c.Get("client_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Client ID not found"})
			return
		}

		mediaIDStr := c.Param("id")
		mediaID, err := primitive.ObjectIDFromHex(mediaIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid media ID"})
			return
		}

		// Verify ownership
		media, err := mediaService.GetMediaByID(c.Request.Context(), mediaID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{"error": "Media not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		clientID, err := primitive.ObjectIDFromHex(clientIDStr.(string))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}

		if media.ClientID != clientID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}

		// Delete media
		err = mediaService.DeleteMedia(c.Request.Context(), mediaID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Media deleted successfully"})
	}
}

// HandleServeMedia serves media files
func HandleServeMedia(mediaService *services.MediaService) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDStr := c.Param("clientId")
		filename := c.Param("filename")

		// Validate client ID
		clientID, err := primitive.ObjectIDFromHex(clientIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}

		// For now, serve files directly from storage
		// In production, you might want to add additional security checks
		filePath := mediaService.GetFilePath(clientID, filename)

		// Check if file exists
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
			return
		}

		// Set appropriate headers based on file extension
		ext := filepath.Ext(filename)
		var mimeType string
		switch ext {
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".png":
			mimeType = "image/png"
		case ".gif":
			mimeType = "image/gif"
		case ".webp":
			mimeType = "image/webp"
		case ".svg":
			mimeType = "image/svg+xml"
		case ".mp4":
			mimeType = "video/mp4"
		case ".webm":
			mimeType = "video/webm"
		case ".ogg":
			mimeType = "video/ogg"
		default:
			mimeType = "application/octet-stream"
		}

		c.Header("Content-Type", mimeType)
		c.Header("Cache-Control", "public, max-age=31536000") // 1 year cache

		// Serve file
		c.File(filePath)
	}
}
