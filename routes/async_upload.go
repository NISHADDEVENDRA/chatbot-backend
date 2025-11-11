package routes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/internal/queue"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// HandleAsyncPDFUpload processes PDF file uploads asynchronously
func HandleAsyncPDFUpload(cfg *config.Config, pdfsCollection *mongo.Collection, queueClient *asynq.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" && !middleware.IsAdmin(c) {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required for upload",
			})
			return
		}

		if err := c.Request.ParseMultipartForm(cfg.MaxFileSize); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "file_too_large",
				"message":    "File size exceeds maximum limit",
			})
			return
		}

		file, header, err := c.Request.FormFile("pdf")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "no_file",
				"message":    "No PDF file provided",
			})
			return
		}
		defer file.Close()

		// Validate file type
		ct := header.Header.Get("Content-Type")
		if !strings.Contains(ct, "pdf") && !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_file_type",
				"message":    "Only PDF files are allowed",
			})
			return
		}

		if header.Size > cfg.MaxFileSize {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "file_too_large",
				"message":    "File size exceeds maximum limit",
			})
			return
		}

		// Basic PDF header validation without loading whole file
		headerBuf := make([]byte, 5)
		if _, err := io.ReadFull(file, headerBuf); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_file",
				"message":    "Cannot read file header",
			})
			return
		}
		if string(headerBuf[:4]) != "%PDF" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_pdf",
				"message":    "File does not appear to be a valid PDF",
			})
			return
		}
		// Reset reader to start for streaming copy
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "file_seek_error",
				"message":    "Failed to reset file for saving",
			})
			return
		}

		// Generate unique file ID
		fileID := uuid.NewString()

		// Create upload directory if it doesn't exist
		uploadDir := filepath.Join(cfg.FileStorageDir, "pdfs", userClientID)
		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "directory_error",
				"message":    "Failed to create upload directory",
			})
			return
		}

		// Save file to disk
		filePath := filepath.Join(uploadDir, fmt.Sprintf("%s.pdf", fileID))
		dst, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "file_open_error",
				"message":    "Failed to open destination",
			})
			return
		}
		defer dst.Close()
		if _, err := io.Copy(dst, io.LimitReader(file, cfg.MaxFileSize)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "file_save_error",
				"message":    "Failed to save file",
			})
			return
		}

		// Create database record with "pending" status
		ctx := context.Background()
		pdfDoc := models.PDFDocument{
			ID:        fileID,
			ClientID:  userClientID,
			Filename:  header.Filename,
			Size:      header.Size,
			Status:    "pending",
			Progress:  0,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = pdfsCollection.InsertOne(ctx, pdfDoc)
		if err != nil {
			// Clean up file if database insert fails
			os.Remove(filePath)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to create database record",
			})
			return
		}

		// Enqueue processing task
		task, err := queue.NewPDFProcessTask(userClientID, fileID, filePath)
		if err != nil {
			// Clean up file and database record
			os.Remove(filePath)
			pdfsCollection.DeleteOne(ctx, bson.M{"_id": fileID})
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "queue_error",
				"message":    "Failed to create processing task",
			})
			return
		}

		info, err := queueClient.Enqueue(task)
		if err != nil {
			// Clean up file and database record
			os.Remove(filePath)
			pdfsCollection.DeleteOne(ctx, bson.M{"_id": fileID})
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "queue_error",
				"message":    "Failed to enqueue processing task",
			})
			return
		}

		// Return immediately with task info
		c.JSON(http.StatusAccepted, gin.H{
			"message":    "PDF upload accepted for processing",
			"file_id":    fileID,
			"task_id":    info.ID,
			"status":     "pending",
			"filename":   header.Filename,
			"size":       header.Size,
			"created_at": pdfDoc.CreatedAt,
		})
	}
}

// CheckPDFStatus checks the processing status of a PDF
func CheckPDFStatus(pdfsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		fileID := c.Param("fileID")
		userClientID := middleware.GetClientID(c)

		ctx := context.Background()
		var pdf models.PDFDocument
		err := pdfsCollection.FindOne(
			ctx,
			bson.M{
				"_id":       fileID,
				"client_id": userClientID, // Ensure user can only check their own files
			},
		).Decode(&pdf)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "file_not_found",
					"message":    "File not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve file status",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"file_id":    pdf.ID,
			"filename":   pdf.Filename,
			"status":     pdf.Status, // pending, processing, completed, failed
			"progress":   pdf.Progress,
			"size":       pdf.Size,
			"created_at": pdf.CreatedAt,
			"updated_at": pdf.UpdatedAt,
		})
	}
}

// ListPDFsWithStatus lists all PDFs for a client with their status
func ListPDFsWithStatus(pdfsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		page := c.DefaultQuery("page", "1")
		limit := c.DefaultQuery("limit", "10")

		// Parse pagination
		pageInt := 1
		limitInt := 10
		if p, err := strconv.Atoi(page); err == nil && p > 0 {
			pageInt = p
		}
		if l, err := strconv.Atoi(limit); err == nil && l > 0 && l <= 100 {
			limitInt = l
		}

		ctx := context.Background()
		skip := (pageInt - 1) * limitInt

		// Find PDFs for this client
		cursor, err := pdfsCollection.Find(
			ctx,
			bson.M{"client_id": userClientID},
			options.Find().
				SetSort(bson.M{"created_at": -1}).
				SetSkip(int64(skip)).
				SetLimit(int64(limitInt)),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve PDFs",
			})
			return
		}
		defer cursor.Close(ctx)

		var pdfs []models.PDFDocument
		if err := cursor.All(ctx, &pdfs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode PDFs",
			})
			return
		}

		// Get total count
		total, err := pdfsCollection.CountDocuments(ctx, bson.M{"client_id": userClientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to count PDFs",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"pdfs": pdfs,
			"pagination": gin.H{
				"page":        pageInt,
				"limit":       limitInt,
				"total":       total,
				"total_pages": (total + int64(limitInt) - 1) / int64(limitInt),
			},
		})
	}
}
