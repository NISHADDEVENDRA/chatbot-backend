package middleware

import (
	"net/http"


	"github.com/gin-gonic/gin"
)

type RoleMiddleware struct{}

func NewRoleMiddleware() *RoleMiddleware {
	return &RoleMiddleware{}
}

func (r *RoleMiddleware) RequireRole(allowedRoles ...string) gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		role := GetRole(c)
		if role == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "User role not found",
			})
			c.Abort()
			return
		}

		// Check if user has one of the allowed roles
		hasRole := false
		for _, allowedRole := range allowedRoles {
			if role == allowedRole {
				hasRole = true
				break
			}
		}

		if !hasRole {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Insufficient permissions",
				"details": gin.H{
					"required_roles": allowedRoles,
					"user_role":      role,
				},
			})
			c.Abort()
			return
		}

		c.Next()
	})
}

func (r *RoleMiddleware) AdminGuard() gin.HandlerFunc {
    return func(c *gin.Context) {
        role := c.GetString("role")
        
        // Both admin and superadmin can access admin routes
        if role != "admin" && role != "superadmin" {
            c.JSON(http.StatusForbidden, gin.H{
                "error_code": "insufficient_permissions",
                "message":    "Admin access required. Your role: " + role,
            })
            c.Abort()
            return
        }
        
        c.Next()
    }
}

// SuperAdminGuard - Only superadmin can access
func (r *RoleMiddleware) SuperAdminGuard() gin.HandlerFunc {
    return func(c *gin.Context) {
        role := c.GetString("role")
        
        // Only superadmin can access superadmin routes
        if role != "superadmin" {
            c.JSON(http.StatusForbidden, gin.H{
                "error_code": "insufficient_permissions",
                "message":    "SuperAdmin access required. Your role: " + role,
            })
            c.Abort()
            return
        }
        
        c.Next()
    }
}

func (r *RoleMiddleware) ClientGuard() gin.HandlerFunc {
    return func(c *gin.Context) {
        role := c.GetString("role")
        
        // Only clients can access client routes
        if role != "client" {
            c.JSON(http.StatusForbidden, gin.H{
                "error_code": "insufficient_permissions",
                "message":    "Client access required. Your role: " + role,
            })
            c.Abort()
            return
        }
        
        c.Next()
    }
}

func (r *RoleMiddleware) VisitorGuard() gin.HandlerFunc {
    return func(c *gin.Context) {
        role := c.GetString("role")
        
        // Allow all roles to access (visitors, clients, admins can chat)
        if role == "" {
            c.JSON(http.StatusUnauthorized, gin.H{
                "error_code": "unauthorized",
                "message":    "Authentication required",
            })
            c.Abort()
            return
        }
        
        c.Next()
    }
}

func (r *RoleMiddleware) RequireClientAccess() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		role := GetRole(c)
		userClientID := GetClientID(c)
		
		// Admin and superadmin can access all clients
		if role == "admin" || role == "superadmin" {
			c.Next()
			return
		}

		// Client users must have a client_id
		if role == "client" && userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required for this operation",
			})
			c.Abort()
			return
		}

		// Check if the requested client matches the user's client
		requestedClientID := c.Param("id")
		if requestedClientID == "" {
			requestedClientID = c.Param("client_id")
		}

		if requestedClientID != "" && role == "client" && requestedClientID != userClientID {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Access denied to this client",
			})
			c.Abort()
			return
		}

		c.Next()
	})
}

func (r *RoleMiddleware) ValidateEmbedAccess() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// Extract origin for embed validation
		origin := c.GetHeader("Origin")
		referer := c.GetHeader("Referer")
		
		// For embed access, we need to validate the origin
		// This is a basic implementation - in production, you'd validate against allowed domains
		if origin == "" && referer == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Invalid embed access",
			})
			c.Abort()
			return
		}

		c.Next()
	})
}

// Helper function to check if user is admin
func IsAdmin(c *gin.Context) bool {
	role := GetRole(c)
	return role == "admin" || role == "superadmin"
}

// Helper function to check if user is superadmin
func IsSuperAdmin(c *gin.Context) bool {
	role := GetRole(c)
	return role == "superadmin"
}

// Helper function to check if user is client
func IsClient(c *gin.Context) bool {
	role := GetRole(c)
	return role == "client"
}

// Helper function to check if user is visitor
func IsVisitor(c *gin.Context) bool {
	role := GetRole(c)
	return role == "visitor"
}

// Helper function to check client ownership
func CanAccessClient(c *gin.Context, targetClientID string) bool {
	role := GetRole(c)
	// Admin and superadmin can access all clients
	if role == "admin" || role == "superadmin" {
		return true
	}
	
	userClientID := GetClientID(c)
	return userClientID == targetClientID
}