package routes

import (
    "net/http"
    "saas-chatbot-platform/internal/config"

    "github.com/gin-gonic/gin"
)

func SetupAuthPagesRoutes(router *gin.Engine, cfg *config.Config) {
    // Login page
    router.GET("/auth/login", func(c *gin.Context) {
        clientID := c.Query("client_id")
        returnURL := c.Query("return_url")
        errorMsg := c.Query("error")

        c.HTML(http.StatusOK, "login.html", gin.H{
            "ClientID":  clientID,
            "ReturnURL": returnURL,
            "Error":     errorMsg,
        })
    })

    // Register page
    router.GET("/auth/register", func(c *gin.Context) {
        clientID := c.Query("client_id")
        returnURL := c.Query("return_url")
        errorMsg := c.Query("error")

        c.HTML(http.StatusOK, "register.html", gin.H{
            "ClientID":  clientID,
            "ReturnURL": returnURL,
            "Error":     errorMsg,
        })
    })

    // Set cookie endpoint - for frontend JavaScript cookie setting (backup)
    router.POST("/auth/set-cookie", func(c *gin.Context) {
        var req struct {
            Token     string `json:"token" binding:"required"`
            ExpiresAt string `json:"expires_at"`
        }

        if err := c.ShouldBindJSON(&req); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{
                "error_code": "invalid_input",
                "message":    "Invalid request data",
            })
            return
        }

        // Set cookie with relaxed settings for iframe compatibility
        c.SetSameSite(http.SameSiteLaxMode)
        c.SetCookie(
            "auth_token",      // name
            req.Token,         // value
            3600*24*7,         // maxAge (7 days)
            "/",               // path
            "",                // domain (empty for localhost)
            false,             // secure (false for development)
            false,             // httpOnly (false to allow JavaScript access)
        )

        c.JSON(http.StatusOK, gin.H{
            "message": "Cookie set successfully",
            "expires_at": req.ExpiresAt,
        })
    })

    // Clear auth cookie endpoint
    router.POST("/auth/clear-cookie", func(c *gin.Context) {
        c.SetCookie(
            "auth_token",
            "",
            -1,    // Expire immediately
            "/",
            "",
            false,
            false,
        )

        c.JSON(http.StatusOK, gin.H{
            "message": "Cookie cleared successfully",
        })
    })
}
