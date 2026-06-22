package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"torrent-search-go/internal/config"
	"torrent-search-go/internal/middleware"
	"torrent-search-go/internal/models"
	storagemodels "torrent-search-go/pkg/models"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	storage *StorageProvider
	config  *config.Config
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(storage *StorageProvider, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		storage: storage,
		config:  cfg,
	}
}

// GoogleLogin initiates Google OAuth login by redirecting to Google's consent screen
func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	params := url.Values{}
	params.Set("client_id", h.config.Google.OAuthClientID)
	params.Set("redirect_uri", h.resolveCallbackURL(r))
	params.Set("response_type", "code")
	params.Set("scope", "email profile openid")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")

	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+params.Encode(), http.StatusTemporaryRedirect)
}

// GoogleCallback handles the Google OAuth callback, exchanges code for tokens,
// finds or creates the user, creates a session, and redirects with the session token.
func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		redirectWithError(w, r, "callback_failed", h.config.FrontendURL)
		return
	}

	googleTokens, err := h.exchangeGoogleCode(r.Context(), code, h.resolveCallbackURL(r))
	if err != nil {
		redirectWithError(w, r, "token_exchange_failed", h.config.FrontendURL)
		return
	}

	googleUser, err := h.getGoogleUserInfo(r.Context(), googleTokens.AccessToken)
	if err != nil {
		redirectWithError(w, r, "user_info_failed", h.config.FrontendURL)
		return
	}

	if !h.isEmailAllowed(googleUser.Email) {
		http.Redirect(w, r, strings.TrimRight(h.config.FrontendURL, "/")+"/login?email_not_allowed=1", http.StatusTemporaryRedirect)
		return
	}

	ctx := r.Context()

	// Find or create user
	userID, err := h.findOrCreateGoogleUser(ctx, googleUser)
	if err != nil {
		redirectWithError(w, r, "db_error", h.config.FrontendURL)
		return
	}

	// Persist Google tokens and update last login
	var tokenExpiry int64
	if googleTokens.ExpiresIn > 0 {
		tokenExpiry = time.Now().Unix() + int64(googleTokens.ExpiresIn)
	}
	_ = h.storage.UpdateUserGoogleTokens(ctx, userID, googleTokens.AccessToken, googleTokens.RefreshToken, tokenExpiry)
	_ = h.storage.UpdateUserLastLogin(ctx, userID)

	// Create session
	sessionToken, err := generateSessionToken()
	if err != nil {
		redirectWithError(w, r, "session_create_failed", h.config.FrontendURL)
		return
	}
	sessionID := generateTokenID()
	expiresAt := time.Now().Add(30 * 24 * time.Hour).Unix()

	err = h.storage.CreateSession(ctx, sessionID, userID, sessionToken,
		r.Header.Get("User-Agent"), r.RemoteAddr, expiresAt)
	if err != nil {
		redirectWithError(w, r, "session_create_failed", h.config.FrontendURL)
		return
	}

	exchangeCode, err := h.storage.CreateExchangeCode(ctx, sessionToken)
	if err != nil {
		redirectWithError(w, r, "exchange_failed", h.config.FrontendURL)
		return
	}

	frontendURL := strings.TrimRight(h.config.FrontendURL, "/")
	http.Redirect(w, r,
		fmt.Sprintf("%s/?auth_exchange=%s", frontendURL, url.QueryEscape(exchangeCode)),
		http.StatusTemporaryRedirect)
}

// redirectWithError redirects to the frontend login page with an error type
func redirectWithError(w http.ResponseWriter, r *http.Request, errorType, frontendURL string) {
	loginURL := strings.TrimRight(frontendURL, "/") + "/login?error=" + errorType
	if frontendURL == "" {
		loginURL = "/login?error=" + errorType
	}
	http.Redirect(w, r, loginURL, http.StatusTemporaryRedirect)
}

// ExchangeAuthCode exchanges a one-time OAuth code for a session token.
func (h *AuthHandler) ExchangeAuthCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Exchange code is required",
			"code":    "MISSING_CODE",
		})
		return
	}

	ctx := r.Context()
	sessionToken, err := h.storage.ConsumeExchangeCode(ctx, req.Code)
	if err != nil || sessionToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Invalid or expired exchange code",
			"code":    "INVALID_CODE",
		})
		return
	}

	session, err := h.storage.ValidateSession(ctx, sessionToken)
	if err != nil || session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Session could not be established",
			"code":    "SESSION_ERROR",
		})
		return
	}

	isEmailAllowed := h.isEmailAllowed(session.Email)
	if !isEmailAllowed {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"success": false,
			"error":   "Email not authorized",
			"code":    "EMAIL_NOT_ALLOWED",
		})
		return
	}

	hasRD, _ := h.storage.HasRealDebridKey(r.Context(), session.UserID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"token":   sessionToken,
		"user":    buildAuthUserPayload(h.storage, r.Context(), session.UserID, session.Email, session.Name, derefStr(session.Picture), hasRD, true, isEmailAllowed),
	})
}

// Logout logs out the current user by invalidating the session
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := extractTokenFromRequest(r)
	if token != "" {
		_ = h.storage.DeleteSession(r.Context(), token)
	}
	// Clear the session cookie, matching Node behavior.
	http.SetCookie(w, &http.Cookie{
		Name:     "sessionToken",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Logged out successfully",
	})
}

// GetUser returns the current authenticated user
func (h *AuthHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Not authenticated",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"user":    userPayloadFromContext(r, h.storage),
	})
}

func userPayloadFromContext(r *http.Request, storage *StorageProvider) map[string]interface{} {
	user := getUserFromContext(r)
	if user == nil {
		return nil
	}
	hasRD, _ := storage.HasRealDebridKey(r.Context(), user.ID)
	return buildAuthUserPayload(storage, r.Context(), user.ID, user.Email, user.Name, user.Picture, hasRD, false, false)
}

func buildAuthUserPayload(
	storage *StorageProvider,
	ctx context.Context,
	userID, email, name, picture string,
	hasRD bool,
	includeEmailAllowed bool,
	isEmailAllowed bool,
) map[string]interface{} {
	payload := map[string]interface{}{
		"id":               userID,
		"email":            email,
		"name":             name,
		"picture":          picture,
		"hasRealDebridKey": hasRD,
	}
	if includeEmailAllowed {
		payload["isEmailAllowed"] = isEmailAllowed
	}
	if row, err := storage.GetUserByID(ctx, userID); err == nil && row != nil {
		payload["createdAt"] = row.CreatedAt
		if row.LastLoginAt != nil {
			payload["lastLoginAt"] = *row.LastLoginAt
		}
	}
	return payload
}

// GetRealDebridKey returns whether the user has a Real-Debrid API key configured.
func (h *AuthHandler) GetRealDebridKey(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Not authenticated"})
		return
	}
	hasKey, err := h.storage.HasRealDebridKey(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Error fetching API key status"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"hasApiKey": hasKey,
	})
}

// SetRealDebridKey saves the user's Real-Debrid API key
func (h *AuthHandler) SetRealDebridKey(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Not authenticated"})
		return
	}
	var req struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "API key is required"})
		return
	}
	if err := h.storage.SetRealDebridKey(r.Context(), user.ID, req.APIKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to save API key"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Real-Debrid API key saved"})
}

// DeleteRealDebridKey deletes the user's Real-Debrid API key
func (h *AuthHandler) DeleteRealDebridKey(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Not authenticated"})
		return
	}
	if err := h.storage.DeleteRealDebridKey(r.Context(), user.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to delete API key"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Real-Debrid API key deleted"})
}

// ValidateSession validates a session token and returns user info
func (h *AuthHandler) ValidateSession(w http.ResponseWriter, r *http.Request) {
	token := extractTokenFromRequest(r)
	if token == "" {
		var req struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		token = req.Token
	}
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Authentication required",
			"code":    "MISSING_TOKEN",
		})
		return
	}

	session, err := h.storage.ValidateSession(r.Context(), token)
	if err != nil || session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Invalid or expired session",
			"code":    "INVALID_TOKEN",
		})
		return
	}

	isEmailAllowed := h.isEmailAllowed(session.Email)
	hasRD, _ := h.storage.HasRealDebridKey(r.Context(), session.UserID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"user": buildAuthUserPayload(
			h.storage, r.Context(), session.UserID, session.Email, session.Name,
			derefStr(session.Picture), hasRD, true, isEmailAllowed,
		),
	})
}

// GetSessions returns the user's active sessions
func (h *AuthHandler) GetSessions(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "Not authenticated"})
		return
	}
	sessions, err := h.storage.GetSessionsByUserID(r.Context(), user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "Failed to get sessions"})
		return
	}
	if sessions == nil {
		sessions = []*storagemodels.SessionRow{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "sessions": sessions})
}

// isEmailAllowed checks if an email is in the allowlist
func (h *AuthHandler) isEmailAllowed(email string) bool {
	if len(h.config.Security.EmailAllowlist) == 0 {
		return true
	}
	email = strings.ToLower(email)
	for _, allowed := range h.config.Security.EmailAllowlist {
		if email == allowed {
			return true
		}
	}
	return false
}

// getUserFromContext extracts the user from the request context
func getUserFromContext(r *http.Request) *models.User {
	mwUser := middleware.GetUser(r)
	if mwUser == nil {
		return nil
	}
	return &models.User{
		ID:      mwUser.UserID,
		Email:   mwUser.Email,
		Name:    mwUser.Name,
		Picture: mwUser.Picture,
	}
}

// extractTokenFromRequest extracts the bearer token from headers or cookie
func extractTokenFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if cookie, err := r.Cookie("sessionToken"); err == nil {
		return cookie.Value
	}
	return ""
}

// ─── authService (implements middleware.AuthService) ────────────────────────

type authService struct {
	storage *StorageProvider
	config  *config.Config
}

// NewAuthService creates a new auth service
func NewAuthService(storage *StorageProvider, cfg *config.Config) *authService {
	return &authService{storage: storage, config: cfg}
}

// ValidateSession validates a session token and returns a UserSession
func (s *authService) ValidateSession(token string) (*models.UserSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	row, err := s.storage.ValidateSession(ctx, token)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	picture := derefStr(row.Picture)
	return &models.UserSession{
		ID:        row.ID,
		UserID:    row.UserID,
		ExpiresAt: row.ExpiresAt,
		CreatedAt: row.CreatedAt,
		Email:     row.Email,
		Name:      row.Name,
		Picture:   picture,
	}, nil
}

// GetRealDebridApiKey returns the decrypted Real-Debrid API key for a user.
func (s *authService) GetRealDebridApiKey(userID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.storage.GetRealDebridKey(ctx, userID)
}

// GetUserByEmail gets a user by email
func (s *authService) GetUserByEmail(email string) (*models.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	row, err := s.storage.GetUserByEmail(ctx, email)
	if err != nil || row == nil {
		return nil, err
	}
	return dbUserToModel(row), nil
}

// FindOrCreateUser finds or creates a user
func (s *authService) FindOrCreateUser(userData *models.User) (*models.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	existing, err := s.storage.GetUserByEmail(ctx, userData.Email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return dbUserToModel(existing), nil
	}

	id := generateTokenID()
	if err := s.storage.CreateUser(ctx, id, userData.Email, userData.Name, userData.Picture, userData.GoogleID); err != nil {
		return nil, err
	}
	return &models.User{
		ID:        id,
		Email:     userData.Email,
		Name:      userData.Name,
		Picture:   userData.Picture,
		GoogleID:  userData.GoogleID,
		IsActive:  true,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}, nil
}

// ─── Google OAuth helpers ────────────────────────────────────────────────────

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
}

type googleUserInfo struct {
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func (h *AuthHandler) exchangeGoogleCode(ctx context.Context, code, redirectURI string) (*googleTokenResponse, error) {
	data := url.Values{
		"code":          {code},
		"client_id":     {h.config.Google.OAuthClientID},
		"client_secret": {h.config.Google.OAuthClientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, body)
	}

	var tokens googleTokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}
	return &tokens, nil
}

func (h *AuthHandler) getGoogleUserInfo(ctx context.Context, accessToken string) (*googleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed (HTTP %d): %s", resp.StatusCode, body)
	}

	var user googleUserInfo
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("failed to parse userinfo: %w", err)
	}
	return &user, nil
}

func (h *AuthHandler) findOrCreateGoogleUser(ctx context.Context, gUser *googleUserInfo) (string, error) {
	// Try by Google ID first
	row, err := h.storage.GetUserByGoogleID(ctx, gUser.Sub)
	if err != nil {
		return "", err
	}
	if row != nil {
		return row.ID, nil
	}

	// Try by email
	row, err = h.storage.GetUserByEmail(ctx, gUser.Email)
	if err != nil {
		return "", err
	}
	if row != nil {
		return row.ID, nil
	}

	// Create new user
	id := generateTokenID()
	if err := h.storage.CreateUser(ctx, id, gUser.Email, gUser.Name, gUser.Picture, gUser.Sub); err != nil {
		return "", err
	}
	return id, nil
}

func (h *AuthHandler) resolveCallbackURL(r *http.Request) string {
	cb := h.config.Google.CallbackURL
	if strings.HasPrefix(cb, "http") {
		return cb
	}
	scheme := "https"
	if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto != "" {
		scheme = fwdProto
	} else if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	return scheme + "://" + host + cb
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func generateTokenID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dbUserToModel(u *storagemodels.UserRow) *models.User {
	picture := derefStr(u.Picture)
	googleID := derefStr(u.GoogleID)
	var lastLogin *int64
	if u.LastLoginAt != nil {
		v := *u.LastLoginAt
		lastLogin = &v
	}
	return &models.User{
		ID:          u.ID,
		Email:       u.Email,
		Name:        u.Name,
		Picture:     picture,
		GoogleID:    googleID,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		LastLoginAt: lastLogin,
		IsActive:    u.IsActive == 1,
	}
}
