package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	AccessExp    time.Time `json:"access_exp"`
	RefreshExp   time.Time `json:"refresh_exp"`
}

type Claims struct {
	UserID   string `json:"user_id"`
	ClientID string `json:"client_id"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

var (
	loadSecretsOnce sync.Once
	accessSecret    []byte
	refreshSecret   []byte
	loadSecretsErr  error
)

func ensureSecrets() error {
	loadSecretsOnce.Do(func() {
		access := os.Getenv("ACCESS_SECRET")
		refresh := os.Getenv("REFRESH_SECRET")

		if len(access) < 32 || len(refresh) < 32 {
			loadSecretsErr = fmt.Errorf("ACCESS_SECRET and REFRESH_SECRET must be configured and at least 32 characters")
			return
		}

		accessSecret = []byte(access)
		refreshSecret = []byte(refresh)
	})

	return loadSecretsErr
}

func IssueTokenPair(userID, clientID, role string, rdb *redis.Client) (*TokenPair, error) {
	if err := ensureSecrets(); err != nil {
		return nil, err
	}

	now := time.Now()
	accessJTI := uuid.NewString()
	refreshJTI := uuid.NewString()

	// Short-lived access token: 1 hour
	accessExp := now.Add(1 * time.Hour)
	accessClaims := Claims{
		UserID:   userID,
		ClientID: clientID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        accessJTI,
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(accessExp),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "saas-chatbot-platform",
		},
	}

	// Long-lived refresh token: 7 days
	refreshExp := now.Add(7 * 24 * time.Hour)
	refreshClaims := Claims{
		UserID:   userID,
		ClientID: clientID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        refreshJTI,
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(refreshExp),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "saas-chatbot-platform",
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)

	accessString, err := accessToken.SignedString(accessSecret)
	if err != nil {
		return nil, err
	}

	refreshString, err := refreshToken.SignedString(refreshSecret)
	if err != nil {
		return nil, err
	}

	// Store JTIs in Redis for revocation capability
	ctx := context.Background()
	pipe := rdb.Pipeline()
	pipe.Set(ctx, "access:"+accessJTI, userID, 1*time.Hour)
	pipe.Set(ctx, "refresh:"+refreshJTI, userID, 7*24*time.Hour)

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessString,
		RefreshToken: refreshString,
		AccessExp:    accessExp,
		RefreshExp:   refreshExp,
	}, nil
}

func ValidateAccessToken(tokenString string, rdb *redis.Client) (*Claims, error) {
	if err := ensureSecrets(); err != nil {
		return nil, err
	}
	return validateToken(tokenString, accessSecret, "access:", rdb)
}

func ValidateRefreshToken(tokenString string, rdb *redis.Client) (*Claims, error) {
	if err := ensureSecrets(); err != nil {
		return nil, err
	}
	return validateToken(tokenString, refreshSecret, "refresh:", rdb)
}

func validateToken(tokenString string, secret []byte, prefix string, rdb *redis.Client) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Prevent algorithm confusion attacks
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})

	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Check if token is revoked
	ctx := context.Background()
	exists, err := rdb.Exists(ctx, prefix+claims.ID).Result()
	if err != nil || exists != 1 {
		return nil, errors.New("token revoked or expired")
	}

	return claims, nil
}

func RevokeToken(jti string, isRefresh bool, rdb *redis.Client) error {
	ctx := context.Background()
	prefix := "access:"
	if isRefresh {
		prefix = "refresh:"
	}
	return rdb.Del(ctx, prefix+jti).Err()
}

func RevokeAllUserTokens(userID string, rdb *redis.Client) error {
	ctx := context.Background()

	// Scan and delete all tokens for this user
	iter := rdb.Scan(ctx, 0, "access:*", 0).Iterator()
	pipe := rdb.Pipeline()

	for iter.Next(ctx) {
		key := iter.Val()
		val, _ := rdb.Get(ctx, key).Result()
		if val == userID {
			pipe.Del(ctx, key)
		}
	}

	iter = rdb.Scan(ctx, 0, "refresh:*", 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		val, _ := rdb.Get(ctx, key).Result()
		if val == userID {
			pipe.Del(ctx, key)
		}
	}

	_, err := pipe.Exec(ctx)
	return err
}

// IssueVisitorToken for embedded widgets with limited permissions
func IssueVisitorToken(clientID, origin string, rdb *redis.Client) (string, error) {
	if err := ensureSecrets(); err != nil {
		return "", err
	}

	now := time.Now()
	visitorJTI := uuid.NewString()

	// Very short-lived visitor token: 1 hour
	visitorExp := now.Add(1 * time.Hour)
	visitorClaims := Claims{
		UserID:   "visitor",
		ClientID: clientID,
		Role:     "visitor",
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        visitorJTI,
			Subject:   "visitor",
			ExpiresAt: jwt.NewNumericDate(visitorExp),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "saas-chatbot-platform",
		},
	}

	visitorToken := jwt.NewWithClaims(jwt.SigningMethodHS256, visitorClaims)
	visitorString, err := visitorToken.SignedString(accessSecret)
	if err != nil {
		return "", err
	}

	// Store visitor token in Redis
	ctx := context.Background()
	if err := rdb.Set(ctx, "visitor:"+visitorJTI, clientID, 1*time.Hour).Err(); err != nil {
		return "", err
	}

	return visitorString, nil
}
