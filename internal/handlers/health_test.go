package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"torrent-search-go/internal/config"
	storagemodels "torrent-search-go/pkg/models"
)

type fakeDB struct {
	status *storagemodels.HealthStatus
	err    error
}

func (f *fakeDB) HealthCheck() (*storagemodels.HealthStatus, error) {
	return f.status, f.err
}

func TestHealthReturnsOK(t *testing.T) {
	cfg := &config.Config{Environment: "test"}
	h := NewHealthHandler(cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "healthy" {
		t.Fatalf("status = %v, want healthy", body["status"])
	}
}

func TestReadyWithoutGoogleCredentials(t *testing.T) {
	cfg := &config.Config{
		Environment:  "production",
		IsProduction: true,
		APIKeys: config.APIKeysConfig{
			AddonAPIToken: "test-token",
		},
		Database: config.DatabaseConfig{
			Mongo: config.MongoConfig{URI: "mongodb://localhost:27017"},
		},
	}
	h := NewHealthHandler(cfg, nil, &fakeDB{
		status: &storagemodels.HealthStatus{Status: "healthy", Type: "mongodb"},
	})

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	h.Ready(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestValidateEnvironmentAddonMode(t *testing.T) {
	cfg := &config.Config{
		IsProduction: true,
		APIKeys:      config.APIKeysConfig{AddonAPIToken: "secret"},
		Database: config.DatabaseConfig{
			Mongo: config.MongoConfig{URI: "mongodb://localhost:27017"},
		},
	}
	result := config.ValidateEnvironment(cfg)
	if !result.IsValid {
		t.Fatalf("expected valid env in addon mode, errors: %v", result.Errors)
	}
}
