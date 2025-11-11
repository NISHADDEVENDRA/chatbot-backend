// cmd/main.go
package main

import (
	"context"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/internal/database"
	"saas-chatbot-platform/internal/logger"
	"saas-chatbot-platform/internal/telemetry"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/routes"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
)

// Define custom template functions
func defaultFunc(def, val interface{}) interface{} {
	if val == nil {
		return def
	}
	if str, ok := val.(string); ok {
		if str == "" {
			return def
		}
	}
	return val
}

// JSON marshal function for templates
func toJSON(v interface{}) template.JS {
	bytes, err := json.Marshal(v)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(bytes)
}

// JavaScript escape function for safe template rendering
func jsEscape(s string) template.JSStr {
	return template.JSStr(s)
}

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Connect to MongoDB
	mongoClient, err := config.ConnectMongoDB(cfg)
	if err != nil {
		log.Fatal("Failed to connect to MongoDB:", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mongoClient.Disconnect(ctx)
	}()

	// Connect to Redis
	rdb, err := config.NewRedisClient(cfg)
	if err != nil {
		log.Fatal("Failed to connect to Redis:", err)
	}
	defer rdb.Close()

	// Initialize Asynq client for async processing
	redisOpt := asynq.RedisClientOpt{
		Addr:     cfg.RedisURL,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}
	queueClient := asynq.NewClient(redisOpt)
	defer queueClient.Close()

	// Initialize tenant database manager
	tenantManager, err := database.NewTenantDBManager(cfg.MongoURI)
	if err != nil {
		log.Fatal("Failed to create tenant manager:", err)
	}

	// Initialize OpenTelemetry tracing
	shutdownTracer, err := telemetry.InitTracer("saas-chatbot-platform")
	if err != nil {
		log.Printf("⚠️  Failed to initialize tracing: %v", err)
	} else {
		defer shutdownTracer()
		log.Println("✅ OpenTelemetry tracing initialized")
	}

	// Initialize metrics
	metrics, err := telemetry.InitMetrics()
	if err != nil {
		log.Printf("⚠️  Failed to initialize metrics: %v", err)
	} else {
		log.Println("✅ Metrics collection initialized")
	}

	// Initialize structured logger
	logger.InitLogger(cfg)
	logger.Info("Application starting", "gin_mode", cfg.GinMode, "port", cfg.Port)

	// Initialize audit logger
	db := mongoClient.Database(cfg.DBName)
	auditLogger := models.NewAuditLogger(db)
	logger.Info("Audit logging initialized")

	// Initialize Gin router
	if cfg.GinMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	
	// Use structured logging instead of default gin logger
	router.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		logger.Error("Panic recovered", "error", recovered, "path", c.Request.URL.Path)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error_code": "internal_error",
			"message":    "An unexpected error occurred",
		})
		c.Abort()
	}))
	
	// Set global multipart memory limit
	router.MaxMultipartMemory = 100 << 20 // 100 MB

	// Add observability middleware
	router.Use(middleware.TracingMiddleware())
	router.Use(middleware.EnrichTrace())
	router.Use(middleware.ManualTracing())

	if metrics != nil {
		router.Use(middleware.MetricsMiddleware(metrics))
	}

	// Add audit middleware to all routes
	router.Use(middleware.AuditMiddleware(auditLogger))

	// Add request ID middleware (first, so all requests have IDs)
	router.Use(middleware.RequestIDMiddleware())

	// Add request size limit middleware (before CORS)
	router.Use(middleware.RequestSizeLimit(10 << 20)) // 10 MB for JSON requests

	// Add rate limiting middleware (after CORS, before routes)
	router.Use(middleware.RateLimitMiddleware(rdb, cfg))

	// CORS configuration - Production-ready with config
	corsConfig := cors.Config{
		AllowOrigins:     cfg.CORSOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With", "Cookie", "X-Client-ID", "X-Embed-Secret", "X-Refresh-Token"},
		AllowCredentials: true,                   // CRITICAL: Allow cookies
		AllowAllOrigins:  false,                  // CRITICAL: Must be false when credentials=true
		ExposeHeaders:    []string{"Set-Cookie"}, // Allow Set-Cookie header
		MaxAge:           12 * time.Hour,
	}
	router.Use(cors.New(corsConfig))

	// Register custom template functions BEFORE loading templates
	router.SetFuncMap(template.FuncMap{
		"default": defaultFunc,
		"toJSON":  toJSON,
		"js":      jsEscape,
	})

	// Load HTML templates and static assets
	router.LoadHTMLGlob("templates/**/*.html")
	router.Static("/assets", "./assets")
	router.Static("/uploads", "./uploads")

	// Add favicon route to fix 404 error
	router.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// Health check endpoint - Enhanced with service checks
	router.GET("/health", func(c *gin.Context) {
		health := gin.H{
			"status":    "healthy",
			"timestamp": time.Now(),
		}
		
		// Check MongoDB
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := mongoClient.Ping(ctx, nil); err != nil {
			health["status"] = "unhealthy"
			health["mongodb"] = "unhealthy"
			health["mongodb_error"] = err.Error()
			c.JSON(http.StatusServiceUnavailable, health)
			return
		}
		health["mongodb"] = "healthy"
		
		// Check Redis
		if err := rdb.Ping(ctx).Err(); err != nil {
			health["status"] = "unhealthy"
			health["redis"] = "unhealthy"
			health["redis_error"] = err.Error()
			c.JSON(http.StatusServiceUnavailable, health)
			return
		}
		health["redis"] = "healthy"
		
		c.JSON(http.StatusOK, health)
	})

	// Readiness endpoint for Kubernetes
	router.GET("/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		
		if err := mongoClient.Ping(ctx, nil); err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		
		if err := rdb.Ping(ctx).Err(); err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		
		c.Status(http.StatusOK)
	})

	// Debug routes - Only available in development mode
	if cfg.GinMode == "debug" {
		// Debug route for testing authentication
		router.GET("/debug/auth", func(c *gin.Context) {
			authToken, err := c.Cookie("auth_token")
			allCookies := c.Request.Cookies()
			cookieMap := make(map[string]string)

			for _, cookie := range allCookies {
				cookieMap[cookie.Name] = cookie.Value
			}

			c.JSON(http.StatusOK, gin.H{
				"auth_token":   authToken,
				"cookie_error": err != nil,
				"error_message": func() string {
					if err != nil {
						return err.Error()
					} else {
						return "none"
					}
				}(),
				"all_cookies":     cookieMap,
				"request_headers": c.Request.Header,
				"user_agent":      c.GetHeader("User-Agent"),
				"referer":         c.GetHeader("Referer"),
			})
		})

		// Debug route for chat history stats
		router.GET("/debug/chat-stats", func(c *gin.Context) {
			db := mongoClient.Database(cfg.DBName)
			conversationsCollection := db.Collection("conversations")
			messagesCollection := db.Collection("messages")

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			conversationCount, _ := conversationsCollection.CountDocuments(ctx, map[string]interface{}{})
			messageCount, _ := messagesCollection.CountDocuments(ctx, map[string]interface{}{})

			c.JSON(http.StatusOK, gin.H{
				"conversations": conversationCount,
				"messages":      messageCount,
				"status":        "Chat history is working",
				"timestamp":     time.Now(),
			})
		})
	} else {
		// In production, return 404 for debug endpoints
		router.GET("/debug/*path", func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "not_found",
				"message":    "Debug endpoints not available in production",
			})
		})
	}

	// Initialize middleware
	authMiddleware := middleware.NewAuthMiddleware(cfg, rdb)
	roleMiddleware := middleware.NewRoleMiddleware()

	// Setup routes with new security features
	routes.SetupAuthRoutes(router, cfg, mongoClient, rdb)
	routes.SetupAdminRoutes(router, cfg, mongoClient, authMiddleware, roleMiddleware)
	routes.SetupClientRoutes(router, cfg, mongoClient, authMiddleware, roleMiddleware)
	routes.SetupChatRoutes(router, cfg, mongoClient, authMiddleware)
	routes.SetupEmbedRoutes(router, cfg, mongoClient, authMiddleware)

	// Setup async processing routes
	pdfsCollection := db.Collection("pdfs")

	// Async PDF upload routes
	asyncGroup := router.Group("/api/async")
	asyncGroup.Use(authMiddleware.RequireAuth())
	{
		asyncGroup.POST("/upload", routes.HandleAsyncPDFUpload(cfg, pdfsCollection, queueClient))
		asyncGroup.GET("/pdf/:fileID/status", routes.CheckPDFStatus(pdfsCollection))
		asyncGroup.GET("/pdfs", routes.ListPDFsWithStatus(pdfsCollection))
	}

	// Setup audit routes (admin only)
	auditGroup := router.Group("/api/admin/audit")
	auditGroup.Use(authMiddleware.RequireAuth())
	auditGroup.Use(roleMiddleware.RequireRole("admin"))
	{
		auditGroup.GET("/logs", routes.QueryAuditLogs(auditLogger))
		auditGroup.GET("/summary/:clientID", routes.GetAuditSummary(auditLogger))
		auditGroup.GET("/verify/:clientID", routes.VerifyAuditChain(auditLogger))
		auditGroup.GET("/stats", routes.GetAuditStats(auditLogger))
		auditGroup.GET("/export", routes.ExportAuditLogs(auditLogger))
	}

	// Add tenant database middleware to protected routes
	router.Use(database.TenantDBMiddleware(tenantManager))

	// Add CORS and embed security middleware
	router.Use(middleware.CORSMiddleware())

	// Add embed-specific routes with security validation
	embedGroup := router.Group("/embed")
	embedGroup.Use(middleware.EmbedCORSValidator(mongoClient.Database(cfg.DBName), rdb))
	{
		// Add embed-specific routes here
		embedGroup.GET("/widget", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "Embed widget endpoint"})
		})
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("🚀 Server starting on port %s", cfg.Port)
		log.Printf("📁 Templates loaded from: templates/**/*.html")
		log.Printf("📁 Static assets served from: ./assets")
		log.Printf("🌐 CORS enabled for origins: %v", corsConfig.AllowOrigins)
		log.Printf("🍪 Cookies enabled with credentials: %v", corsConfig.AllowCredentials)
		log.Printf("🔧 Debug endpoints available:")
		log.Printf("   - GET /debug/auth (check authentication)")
		log.Printf("   - GET /debug/chat-stats (check chat history)")
		log.Printf("📝 Chat routes enabled:")
		log.Printf("   - POST /chat/send (send message with history)")
		log.Printf("   - GET /chat/conversations (get all conversations)")
		log.Printf("   - GET /chat/conversations/:id (get specific conversation)")
		log.Printf("🔍 Observability features enabled:")
		log.Printf("   - OpenTelemetry tracing")
		log.Printf("   - Metrics collection")
		log.Printf("   - Immutable audit logging")
		log.Printf("📊 Admin audit endpoints:")
		log.Printf("   - GET /api/admin/audit/logs (query audit logs)")
		log.Printf("   - GET /api/admin/audit/summary/:clientID (audit summary)")
		log.Printf("   - GET /api/admin/audit/verify/:clientID (verify chain)")
		log.Printf("   - GET /api/admin/audit/stats (audit statistics)")
		log.Printf("   - GET /api/admin/audit/export (export logs)")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}
