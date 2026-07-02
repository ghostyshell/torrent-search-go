package models

import "time"

// User represents a user account
type User struct {
	ID                 string `json:"id"`
	Email              string `json:"email"`
	Name               string `json:"name"`
	Picture            string `json:"picture"`
	GoogleID           string `json:"googleId"`
	GoogleAccessToken  string `json:"-"` // Never expose in JSON
	GoogleRefreshToken string `json:"-"` // Never expose in JSON
	GoogleTokenExpires *int64 `json:"-"` // Never expose in JSON
	RealDebridAPIKey   string `json:"-"` // Never expose in JSON
	CreatedAt          int64  `json:"createdAt"`
	UpdatedAt          int64  `json:"updatedAt"`
	LastLoginAt        *int64 `json:"lastLoginAt"`
	IsActive           bool   `json:"isActive"`
}

// UserSession represents an authenticated user session
type UserSession struct {
	ID             string `json:"id"`
	UserID         string `json:"userId"`
	SessionToken   string `json:"-"` // Never expose in JSON
	ExpiresAt      int64  `json:"expiresAt"`
	UserAgent      string `json:"userAgent"`
	IPAddress      string `json:"ipAddress"`
	LastAccessedAt *int64 `json:"lastAccessedAt"`
	CreatedAt      int64  `json:"createdAt"`
	// Joined from users table
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// UserToken represents a temporary authentication token
type UserToken struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Picture   string `json:"picture"`
	Timestamp int64  `json:"timestamp"`
}

// NewUserToken creates a new temporary token
func NewUserToken(user *User) *UserToken {
	now := time.Now().UnixMilli()
	return &UserToken{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		Picture:   user.Picture,
		Timestamp: now,
	}
}

// IsExpired checks if the token is expired (max 24 hours)
func (t *UserToken) IsExpired() bool {
	maxAge := int64(24 * 60 * 60 * 1000) // 24 hours in milliseconds
	return time.Now().UnixMilli()-t.Timestamp > maxAge
}

// ToBase64 encodes the token to base64 string
func (t *UserToken) ToBase64() (string, error) {
	// Implementation in auth service
	return "", nil
}
