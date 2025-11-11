package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/gin-gonic/gin"
)

// DomainAuthMiddleware handles domain authorization for chatframe embedding
type DomainAuthMiddleware struct {
	clientsCollection *mongo.Collection
	alertsCollection  *mongo.Collection
}

// NewDomainAuthMiddleware creates a new domain authorization middleware
func NewDomainAuthMiddleware(clientsCollection, alertsCollection *mongo.Collection) *DomainAuthMiddleware {
	return &DomainAuthMiddleware{
		clientsCollection: clientsCollection,
		alertsCollection:  alertsCollection,
	}
}

// CheckDomainAuthorization checks if the requesting domain is authorized for the client
func (m *DomainAuthMiddleware) CheckDomainAuthorization() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get client ID from URL parameter or request body
		// Try both camelCase and snake_case parameter names for compatibility
		clientID := c.Param("client_id")
		if clientID == "" {
			clientID = c.Param("clientId")
		}

		// If no client ID in URL, try to get it from request body (for /public/chat endpoint)
		if clientID == "" && c.Request.Method == "POST" {
			// Read the request body
			body, err := io.ReadAll(c.Request.Body)
			if err == nil {
				var requestBody struct {
					ClientID string `json:"client_id"`
				}
				if json.Unmarshal(body, &requestBody) == nil {
					clientID = requestBody.ClientID
				}
				// Restore the request body for the next handler
				c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
			}
		}

		if clientID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Client ID is required",
			})
			c.Abort()
			return
		}

		// Convert client ID to ObjectID
		clientObjID, err := primitive.ObjectIDFromHex(clientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid client ID format",
			})
			c.Abort()
			return
		}

		// Get client information
		var client struct {
			ID                primitive.ObjectID `bson:"_id"`
			Name              string             `bson:"name"`
			DomainWhitelist   []string           `bson:"domain_whitelist"`
			DomainBlacklist   []string           `bson:"domain_blacklist"`
			DomainMode        string             `bson:"domain_mode"`
			RequireDomainAuth bool               `bson:"require_domain_auth"`
		}

		err = m.clientsCollection.FindOne(context.Background(), bson.M{"_id": clientObjID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error": "Client not found",
				})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "Failed to fetch client information",
				})
			}
			c.Abort()
			return
		}

		// If domain authorization is not required, allow access
		if !client.RequireDomainAuth {
			c.Next()
			return
		}

		// Get the requesting domain
		requestDomain := m.getRequestDomain(c)
		if requestDomain == "" {
			m.logSuspiciousActivity(clientObjID, "", c, "no_domain", "No domain information available")
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "domain_auth_required",
				"message":     "Domain authorization required",
			})
			c.Abort()
			return
		}

		// Check domain authorization
		isAuthorized := m.checkDomainAccess(requestDomain, client.DomainWhitelist, client.DomainBlacklist, client.DomainMode)

		if !isAuthorized {
			// Log suspicious activity
			m.logSuspiciousActivity(clientObjID, requestDomain, c, "unauthorized_domain",
				fmt.Sprintf("Unauthorized domain '%s' attempted to access client '%s'", requestDomain, client.Name))

			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "domain_not_authorized",
				"message":    "Domain not authorized for this client",
				"details":    gin.H{"domain": requestDomain},
			})
			c.Abort()
			return
		}

		// Store client info in context for use in handlers
		c.Set("client", client)
		c.Next()
	}
}

// getRequestDomain extracts the domain from the request
func (m *DomainAuthMiddleware) getRequestDomain(c *gin.Context) string {
	// Try to get domain from referrer header first
	referrer := c.GetHeader("Referer")
	if referrer != "" {
		if domain := m.extractDomainFromURL(referrer); domain != "" {
			return domain
		}
	}

	// Try to get domain from origin header
	origin := c.GetHeader("Origin")
	if origin != "" {
		if domain := m.extractDomainFromURL(origin); domain != "" {
			return domain
		}
	}

	// Try to get domain from X-Forwarded-Host header (for reverse proxies)
	forwardedHost := c.GetHeader("X-Forwarded-Host")
	if forwardedHost != "" {
		return m.normalizeDomain(forwardedHost)
	}

	// Try to get domain from Host header
	host := c.GetHeader("Host")
	if host != "" {
		return m.normalizeDomain(host)
	}

	return ""
}

// extractDomainFromURL extracts domain from a URL
func (m *DomainAuthMiddleware) extractDomainFromURL(urlStr string) string {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	domain := parsedURL.Host
	if domain == "" {
		return ""
	}

	return m.normalizeDomain(domain)
}

// normalizeDomain normalizes domain for comparison
func (m *DomainAuthMiddleware) normalizeDomain(domain string) string {
	// Remove protocol and path
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")

	// Remove path
	if idx := strings.Index(domain, "/"); idx != -1 {
		domain = domain[:idx]
	}

	// Remove port if present
	if idx := strings.Index(domain, ":"); idx != -1 {
		domain = domain[:idx]
	}

	// Convert to lowercase
	domain = strings.ToLower(domain)

	// Remove www. prefix for comparison
	domain = strings.TrimPrefix(domain, "www.")

	// Handle localhost vs 127.0.0.1
	if domain == "127.0.0.1" {
		domain = "localhost"
	}

	return domain
}

// checkDomainAccess checks if domain is authorized based on whitelist/blacklist
func (m *DomainAuthMiddleware) checkDomainAccess(domain string, whitelist, blacklist []string, mode string) bool {
	normalizedDomain := m.normalizeDomain(domain)

	// Normalize whitelist and blacklist domains
	normalizedWhitelist := make([]string, len(whitelist))
	for i, d := range whitelist {
		normalizedWhitelist[i] = m.normalizeDomain(d)
	}

	normalizedBlacklist := make([]string, len(blacklist))
	for i, d := range blacklist {
		normalizedBlacklist[i] = m.normalizeDomain(d)
	}

	switch mode {
	case "whitelist":
		// In whitelist mode, domain must be in whitelist
		for _, allowedDomain := range normalizedWhitelist {
			match := normalizedDomain == allowedDomain || strings.HasSuffix(normalizedDomain, "."+allowedDomain)
			if match {
				return true
			}
		}
		return false

	case "blacklist":
		// In blacklist mode, domain must not be in blacklist
		for _, blockedDomain := range normalizedBlacklist {
			if normalizedDomain == blockedDomain || strings.HasSuffix(normalizedDomain, "."+blockedDomain) {
				return false
			}
		}
		return true

	default:
		// Default to whitelist mode if no mode specified
		return len(normalizedWhitelist) == 0 || m.checkDomainAccess(domain, whitelist, blacklist, "whitelist")
	}
}

// logSuspiciousActivity logs suspicious activity to the database
func (m *DomainAuthMiddleware) logSuspiciousActivity(clientID primitive.ObjectID, domain string, c *gin.Context, alertType, message string) {
	// Get additional request information
	userIP := c.ClientIP()
	userAgent := c.GetHeader("User-Agent")
	referrer := c.GetHeader("Referer")

	// Determine severity based on alert type
	severity := "medium"
	if alertType == "unauthorized_domain" {
		severity = "high"
	}

	alert := bson.M{
		"client_id":  clientID,
		"domain":     domain,
		"ip_address": userIP,
		"user_agent": userAgent,
		"referrer":   referrer,
		"alert_type": alertType,
		"severity":   severity,
		"message":    message,
		"resolved":   false,
		"created_at": time.Now(),
	}

	// Insert alert asynchronously
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := m.alertsCollection.InsertOne(ctx, alert)
		if err != nil {
			fmt.Printf("Failed to log suspicious activity: %v\n", err)
		}
	}()
}
