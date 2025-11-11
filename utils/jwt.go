package utils

import (
    "fmt"
    "strings"
    "time"
    "github.com/golang-jwt/jwt/v5"
)

type Claims struct {
    UserID   string `json:"user_id"`
    Role     string `json:"role"`
    ClientID string `json:"client_id,omitempty"`
    jwt.RegisteredClaims
}

func GenerateJWT(userID, role, clientID, jwtSecret string, expiresIn time.Duration) (string, error) {
    claims := Claims{
        UserID:   userID,
        Role:     role,
        ClientID: clientID,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
            NotBefore: jwt.NewNumericDate(time.Now()),
            Issuer:    "saas-chatbot-platform",
        },
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(jwtSecret))
}

func ValidateJWT(tokenString, jwtSecret string) (*Claims, error) {
    // Remove Bearer prefix if present
    tokenString = strings.TrimPrefix(tokenString, "Bearer ")

    token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        return []byte(jwtSecret), nil
    })

    if err != nil {
        return nil, fmt.Errorf("failed to parse token: %v", err)
    }

    claims, ok := token.Claims.(*Claims)
    if !ok || !token.Valid {
        return nil, fmt.Errorf("invalid token claims")
    }

    // Check if token is expired
    if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
        return nil, fmt.Errorf("token is expired")
    }

    return claims, nil
}

func RefreshJWT(tokenString, jwtSecret string, expiresIn time.Duration) (string, error) {
    claims, err := ValidateJWT(tokenString, jwtSecret)
    if err != nil {
        return "", err
    }

    // Generate new token with same claims but updated expiration
    return GenerateJWT(claims.UserID, claims.Role, claims.ClientID, jwtSecret, expiresIn)
}

func ExtractTokenFromHeader(authHeader string) string {
    if authHeader == "" {
        return ""
    }

    parts := strings.Split(authHeader, " ")
    if len(parts) != 2 || parts[0] != "Bearer" {
        return ""
    }

    return parts[1]
}
