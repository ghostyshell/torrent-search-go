package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"torrent-search-go/internal/models"
)

// UserKey is the context key for user information
type UserKey struct{}

// AddonServiceKey marks requests authenticated via ADDON_API_TOKEN (no user).
type AddonServiceKey struct{}

// RealDebridKey is the context key for the decrypted Real-Debrid API key
type RealDebridKey struct{}

// User represents an authenticated user
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Picture   string `json:"picture"`
	SessionID string `json:"sessionId"`
	UserID    string `json:"userId"`
}

// AuthService interface for authentication operations
type AuthService interface {
	ValidateSession(token string) (*models.UserSession, error)
	GetUserByEmail(email string) (*models.User, error)
	FindOrCreateUser(userData *models.User) (*models.User, error)
	GetRealDebridApiKey(userID string) (string, error)
}

// AuthMiddleware handles authentication
type AuthMiddleware struct {
	authService   AuthService
	addonAPIToken string
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(authService AuthService, addonAPIToken string) *AuthMiddleware {
	return &AuthMiddleware{
		authService:   authService,
		addonAPIToken: addonAPIToken,
	}
}

// RequireAuth returns a middleware that requires authentication
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Addon-to-backend service token (ADDON_API_TOKEN). Does not identify a user.
		if MatchesAddonToken(r, m.addonAPIToken) {
			ctx := context.WithValue(r.Context(), AddonServiceKey{}, true)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		user, err := m.authenticate(r)
		if err != nil {
			writeAuthError(w, "Authentication required", "UNAUTHORIZED", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth returns a middleware that optionally authenticates
func (m *AuthMiddleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := m.authenticate(r)
		if user != nil {
			ctx := context.WithValue(r.Context(), UserKey{}, user)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// GetUserRealDebridKey returns a middleware that requires a Real-Debrid API key
func (m *AuthMiddleware) GetUserRealDebridKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil {
			writeAuthError(w, "Authentication required for Real Debrid operations", "UNAUTHORIZED", http.StatusUnauthorized)
			return
		}

		apiKey, err := m.authService.GetRealDebridApiKey(user.UserID)
		if err != nil {
			writeAuthError(w, "Error retrieving Real Debrid API key", "API_KEY_ERROR", http.StatusInternalServerError)
			return
		}
		if apiKey == "" {
			writeAuthError(w, "Real Debrid API key not configured. Please add it in your account settings.", "NO_API_KEY", http.StatusBadRequest)
			return
		}

		ctx := context.WithValue(r.Context(), RealDebridKey{}, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RestrictToOwner returns a middleware that restricts access to resource owner
func (m *AuthMiddleware) RestrictToOwner(getResourceUserID func(*http.Request) (string, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r)
			if user == nil {
				writeAuthError(w, "Authentication required", "UNAUTHORIZED", http.StatusUnauthorized)
				return
			}

			resourceUserID, err := getResourceUserID(r)
			if err != nil {
				writeAuthError(w, "Invalid resource ID", "INVALID_RESOURCE", http.StatusBadRequest)
				return
			}

			if resourceUserID != "" && resourceUserID != user.UserID {
				writeAuthError(w, "Access denied: You can only access your own data", "FORBIDDEN", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// authenticate extracts and validates the session token
func (m *AuthMiddleware) authenticate(r *http.Request) (*User, error) {
	token := extractToken(r)
	if token == "" {
		return nil, ErrNoToken
	}

	// Try database session validation first
	if m.authService != nil {
		session, err := m.authService.ValidateSession(token)
		if err == nil && session != nil {
			return &User{
				ID:        session.UserID,
				Email:     session.Email,
				Name:      session.Name,
				Picture:   session.Picture,
				SessionID: session.ID,
				UserID:    session.UserID,
			}, nil
		}
	}

	// Fallback: Try to validate as base64 temporary token
	user, err := m.validateTemporaryToken(token)
	if err == nil && user != nil {
		return user, nil
	}

	return nil, ErrInvalidToken
}

// validateTemporaryToken validates a base64-encoded temporary token
func (m *AuthMiddleware) validateTemporaryToken(token string) (*User, error) {
	// Validate base64 format
	if !isValidBase64(token) {
		return nil, ErrInvalidTokenFormat
	}

	// Decode token
	decodedBytes, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidTokenFormat
	}

	decodedString := string(decodedBytes)

	// Check if it's JSON
	if !strings.HasPrefix(decodedString, "{") || !strings.HasSuffix(decodedString, "}") {
		return nil, ErrInvalidTokenFormat
	}

	// Parse JSON
	var tokenData struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		Picture   string `json:"picture"`
		Timestamp int64  `json:"timestamp"`
	}

	if err := json.Unmarshal([]byte(decodedString), &tokenData); err != nil {
		return nil, ErrInvalidTokenFormat
	}

	// Validate required fields
	if tokenData.ID == "" || tokenData.Email == "" || tokenData.Timestamp == 0 {
		return nil, ErrInvalidTokenFormat
	}

	// Check token age (max 24 hours)
	tokenAge := time.Now().UnixMilli() - tokenData.Timestamp
	maxAge := int64(24 * 60 * 60 * 1000) // 24 hours in milliseconds
	if tokenAge > maxAge {
		return nil, ErrTokenExpired
	}

	// Create or get user
	var user *models.User

	if m.authService != nil {
		userData := &models.User{
			ID:    tokenData.ID,
			Email: tokenData.Email,
			Name:  tokenData.Name,
		}
		var createErr error
		user, createErr = m.authService.FindOrCreateUser(userData)
		if createErr != nil {
			return nil, ErrInvalidToken
		}
	}

	if user == nil {
		// Create temporary user
		user = &models.User{
			ID:    tokenData.ID,
			Email: tokenData.Email,
			Name:  tokenData.Name,
		}
	}

	return &User{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		Picture:   user.Picture,
		SessionID: "",
		UserID:    user.ID,
	}, nil
}

// extractToken extracts the token from request headers or cookies
func extractToken(r *http.Request) string {
	// Try Authorization header first
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != authHeader {
			return token
		}
	}

	// Try cookies
	cookie, err := r.Cookie("sessionToken")
	if err == nil && cookie != nil {
		return cookie.Value
	}

	return ""
}

// isValidBase64 checks if a string is valid base64
func isValidBase64(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Basic pattern check
	validChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
	for _, c := range s {
		if !strings.ContainsRune(validChars, c) {
			return false
		}
	}
	return true
}

// GetRealDebridKey retrieves the Real-Debrid API key from context
func GetRealDebridKey(r *http.Request) string {
	key, ok := r.Context().Value(RealDebridKey{}).(string)
	if !ok {
		return ""
	}
	return key
}

// MatchesAddonToken reports whether the request bears ADDON_API_TOKEN via
// Authorization: Bearer or X-Addon-Token.
func MatchesAddonToken(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		if strings.TrimPrefix(authHeader, "Bearer ") == token {
			return true
		}
	}
	return r.Header.Get("X-Addon-Token") == token
}

// IsAddonServiceRequest reports whether the request was authenticated with ADDON_API_TOKEN.
func IsAddonServiceRequest(r *http.Request) bool {
	v, ok := r.Context().Value(AddonServiceKey{}).(bool)
	return ok && v
}

// GetUser retrieves the user from context
func GetUser(r *http.Request) *User {
	user, ok := r.Context().Value(UserKey{}).(*User)
	if !ok {
		return nil
	}
	return user
}

// GetUserFromContext retrieves the user from context (alternative name)
func GetUserFromContext(ctx context.Context) *User {
	user, ok := ctx.Value(UserKey{}).(*User)
	if !ok {
		return nil
	}
	return user
}

// Auth errors
var (
	ErrNoToken            = &AuthError{Message: "No authentication token provided", Code: "NO_TOKEN"}
	ErrInvalidToken       = &AuthError{Message: "Invalid or expired session", Code: "INVALID_SESSION"}
	ErrInvalidTokenFormat = &AuthError{Message: "Invalid token format", Code: "INVALID_TOKEN_FORMAT"}
	ErrTokenExpired       = &AuthError{Message: "Token expired", Code: "TOKEN_EXPIRED"}
)

// AuthError represents an authentication error
type AuthError struct {
	Message string
	Code    string
}

func (e *AuthError) Error() string {
	return e.Message
}

// writeAuthError writes an authentication error response
func writeAuthError(w http.ResponseWriter, message, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
		"code":    code,
	})
}

// StaticTokenAuth returns middleware that accepts requests bearing the given
// static service token (Bearer or X-Addon-Token). If token is empty the
// middleware is a no-op, preserving backward-compatibility when the env var
// is not configured. Prefer RequireAuth for cache/storage routes - it accepts
// either a user session or the addon service token.
func StaticTokenAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !MatchesAddonToken(r, token) {
				writeAuthError(w, "Valid API token required", "UNAUTHORIZED", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
