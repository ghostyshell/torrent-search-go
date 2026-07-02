package config

import "testing"

func TestValidateEnvironmentRequiresMongo(t *testing.T) {
	result := ValidateEnvironment(&Config{})
	if result.IsValid {
		t.Fatal("expected invalid without mongo URI")
	}
}

func TestValidateEnvironmentGoogleOptional(t *testing.T) {
	cfg := &Config{
		Database: DatabaseConfig{
			Mongo: MongoConfig{URI: "mongodb://localhost:27017"},
		},
	}
	result := ValidateEnvironment(cfg)
	if !result.IsValid {
		t.Fatalf("expected valid without google, errors: %v", result.Errors)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected warnings for missing google config")
	}
}
