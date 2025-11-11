package routes

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"saas-chatbot-platform/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

// QueryAuditLogs queries audit logs with filters
func QueryAuditLogs(auditor *models.AuditLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Parse query parameters
		clientID := c.Query("client_id")
		userID := c.Query("user_id")
		action := c.Query("action")
		resource := c.Query("resource")
		startTimeStr := c.Query("start_time")
		endTimeStr := c.Query("end_time")
		pageStr := c.DefaultQuery("page", "1")
		pageSizeStr := c.DefaultQuery("page_size", "20")

		// Parse pagination
		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		pageSize, err := strconv.Atoi(pageSizeStr)
		if err != nil || pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}

		// Build filter
		filter := bson.M{}

		if clientID != "" {
			filter["client_id"] = clientID
		}
		if userID != "" {
			filter["user_id"] = userID
		}
		if action != "" {
			filter["action"] = action
		}
		if resource != "" {
			filter["resource"] = resource
		}

		// Parse time range
		if startTimeStr != "" || endTimeStr != "" {
			timeFilter := bson.M{}
			
			if startTimeStr != "" {
				if startTime, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
					timeFilter["$gte"] = startTime
				}
			}
			
			if endTimeStr != "" {
				if endTime, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
					timeFilter["$lte"] = endTime
				}
			}
			
			if len(timeFilter) > 0 {
				filter["timestamp"] = timeFilter
			}
		}

		// Execute query
		events, total, err := auditor.QueryAuditLogs(filter, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "query_failed",
				"message":    "Failed to query audit logs",
			})
			return
		}

		// Calculate pagination info
		totalPages := (total + int64(pageSize) - 1) / int64(pageSize)

		c.JSON(http.StatusOK, gin.H{
			"events": events,
			"pagination": gin.H{
				"page":        page,
				"page_size":   pageSize,
				"total":       total,
				"total_pages": totalPages,
			},
		})
	}
}

// GetAuditSummary returns audit summary for a client
func GetAuditSummary(auditor *models.AuditLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID := c.Param("clientID")
		daysStr := c.DefaultQuery("days", "30")

		days, err := strconv.Atoi(daysStr)
		if err != nil || days < 1 || days > 365 {
			days = 30
		}

		summary, err := auditor.GetAuditSummary(clientID, days)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "summary_failed",
				"message":    "Failed to generate audit summary",
			})
			return
		}

		c.JSON(http.StatusOK, summary)
	}
}

// VerifyAuditChain verifies the integrity of audit chain for a client
func VerifyAuditChain(auditor *models.AuditLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID := c.Param("clientID")

		isValid, err := auditor.VerifyChain(clientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "verification_failed",
				"message":    "Failed to verify audit chain",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"client_id": clientID,
			"is_valid":  isValid,
			"message":   "Audit chain verification completed",
		})
	}
}

// GetAuditStats returns audit statistics
func GetAuditStats(auditor *models.AuditLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		col := auditor.Collection()

		// Get total events count
		totalEvents, err := col.CountDocuments(ctx, bson.M{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "stats_failed",
				"message":    "Failed to get audit statistics",
			})
			return
		}

		// Get events by action
		actionPipeline := []bson.M{
			{
				"$group": bson.M{
					"_id":   "$action",
					"count": bson.M{"$sum": 1},
				},
			},
		}

		actionCursor, err := col.Aggregate(ctx, actionPipeline)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "stats_failed",
				"message":    "Failed to get action statistics",
			})
			return
		}
		defer actionCursor.Close(ctx)

		var actionStats []bson.M
		if err := actionCursor.All(ctx, &actionStats); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "stats_failed",
				"message":    "Failed to decode action statistics",
			})
			return
		}

		// Get events by resource
		resourcePipeline := []bson.M{
			{
				"$group": bson.M{
					"_id":   "$resource",
					"count": bson.M{"$sum": 1},
				},
			},
		}

		resourceCursor, err := col.Aggregate(ctx, resourcePipeline)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "stats_failed",
				"message":    "Failed to get resource statistics",
			})
			return
		}
		defer resourceCursor.Close(ctx)

		var resourceStats []bson.M
		if err := resourceCursor.All(ctx, &resourceStats); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "stats_failed",
				"message":    "Failed to decode resource statistics",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"total_events":    totalEvents,
			"action_stats":    actionStats,
			"resource_stats":  resourceStats,
			"generated_at":    time.Now(),
		})
	}
}

// ExportAuditLogs exports audit logs to JSON
func ExportAuditLogs(auditor *models.AuditLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Parse query parameters (same as QueryAuditLogs)
		clientID := c.Query("client_id")
		userID := c.Query("user_id")
		action := c.Query("action")
		resource := c.Query("resource")
		startTimeStr := c.Query("start_time")
		endTimeStr := c.Query("end_time")

		// Build filter (same as QueryAuditLogs)
		filter := bson.M{}

		if clientID != "" {
			filter["client_id"] = clientID
		}
		if userID != "" {
			filter["user_id"] = userID
		}
		if action != "" {
			filter["action"] = action
		}
		if resource != "" {
			filter["resource"] = resource
		}

		// Parse time range
		if startTimeStr != "" || endTimeStr != "" {
			timeFilter := bson.M{}
			
			if startTimeStr != "" {
				if startTime, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
					timeFilter["$gte"] = startTime
				}
			}
			
			if endTimeStr != "" {
				if endTime, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
					timeFilter["$lte"] = endTime
				}
			}
			
			if len(timeFilter) > 0 {
				filter["timestamp"] = timeFilter
			}
		}

		// Get all matching events (no pagination for export)
		events, _, err := auditor.QueryAuditLogs(filter, 1, 10000) // Max 10k events
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "export_failed",
				"message":    "Failed to export audit logs",
			})
			return
		}

		// Set response headers for file download
		filename := "audit_logs_" + time.Now().Format("20060102_150405") + ".json"
		c.Header("Content-Disposition", "attachment; filename="+filename)
		c.Header("Content-Type", "application/json")

		c.JSON(http.StatusOK, gin.H{
			"export_info": gin.H{
				"filename":    filename,
				"total_events": len(events),
				"exported_at": time.Now(),
				"filters":     filter,
			},
			"events": events,
		})
	}
}
