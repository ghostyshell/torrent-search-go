package crypto

import (
	"strings"
	"testing"
)

func setKey(t *testing.T, key string) {
	t.Helper()
	t.Setenv("REAL_DEBRID_ENCRYPTION_KEY", "")
	t.Setenv("SESSION_SECRET", key)
}

// encryptionKey

func TestEncryptionKey_NoEnv(t *testing.T) {
	t.Setenv("REAL_DEBRID_ENCRYPTION_KEY", "")
	t.Setenv("SESSION_SECRET", "")
	_, err := encryptionKey()
	if err == nil {
		t.Fatal("expected error when no env set")
	}
}

func TestEncryptionKey_RealDebridKey(t *testing.T) {
	t.Setenv("REAL_DEBRID_ENCRYPTION_KEY", "mykey")
	t.Setenv("SESSION_SECRET", "")
	k, err := encryptionKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 32 {
		t.Fatalf("want 32 bytes, got %d", len(k))
	}
}

func TestEncryptionKey_SessionSecretFallback(t *testing.T) {
	t.Setenv("REAL_DEBRID_ENCRYPTION_KEY", "")
	t.Setenv("SESSION_SECRET", "fallback")
	k, err := encryptionKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 32 {
		t.Fatalf("want 32 bytes, got %d", len(k))
	}
}

// EncryptSecret

func TestEncryptSecret_Empty(t *testing.T) {
	ct, err := EncryptSecret("")
	if err != nil || ct != "" {
		t.Fatalf("want (\"\", nil), got (%q, %v)", ct, err)
	}
}

func TestEncryptSecret_NoKey(t *testing.T) {
	t.Setenv("REAL_DEBRID_ENCRYPTION_KEY", "")
	t.Setenv("SESSION_SECRET", "")
	_, err := EncryptSecret("hello")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncryptSecret_ProducesV1Format(t *testing.T) {
	setKey(t, "testkey")
	ct, err := EncryptSecret("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ct, "v1:") {
		t.Fatalf("want v1: prefix, got %q", ct)
	}
	if parts := strings.Split(ct, ":"); len(parts) != 4 {
		t.Fatalf("want 4 colon-separated parts, got %d", len(parts))
	}
}

// DecryptSecret

func TestDecryptSecret_Empty(t *testing.T) {
	pt, err := DecryptSecret("")
	if err != nil || pt != "" {
		t.Fatalf("want (\"\", nil), got (%q, %v)", pt, err)
	}
}

func TestDecryptSecret_LegacyPlaintext(t *testing.T) {
	pt, err := DecryptSecret("plaintoken")
	if err != nil || pt != "plaintoken" {
		t.Fatalf("want (plaintoken, nil), got (%q, %v)", pt, err)
	}
}

func TestDecryptSecret_InvalidPartCount(t *testing.T) {
	_, err := DecryptSecret("v1:only:three")
	if err == nil {
		t.Fatal("expected error for wrong number of parts")
	}
}

func TestDecryptSecret_InvalidBase64IV(t *testing.T) {
	_, err := DecryptSecret("v1:!!bad!!:dGFn:ZGF0YQ==")
	if err == nil {
		t.Fatal("expected base64 decode error for iv")
	}
}

func TestDecryptSecret_InvalidBase64Tag(t *testing.T) {
	_, err := DecryptSecret("v1:aXY=:!!bad!!:ZGF0YQ==")
	if err == nil {
		t.Fatal("expected base64 decode error for tag")
	}
}

func TestDecryptSecret_InvalidBase64Data(t *testing.T) {
	_, err := DecryptSecret("v1:aXY=:dGFn:!!bad!!")
	if err == nil {
		t.Fatal("expected base64 decode error for data")
	}
}

func TestDecryptSecret_NoKey(t *testing.T) {
	t.Setenv("REAL_DEBRID_ENCRYPTION_KEY", "")
	t.Setenv("SESSION_SECRET", "")
	_, err := DecryptSecret("v1:aXY=:dGFn:ZGF0YQ==")
	if err == nil {
		t.Fatal("expected error when no encryption key")
	}
}

func TestDecryptSecret_TamperedCiphertext(t *testing.T) {
	setKey(t, "testkey")
	ct, err := EncryptSecret("secret")
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt the data part (last segment)
	parts := strings.Split(ct, ":")
	parts[3] = "AAAAAAAAAAAAAAAA"
	_, err = DecryptSecret(strings.Join(parts, ":"))
	if err == nil {
		t.Fatal("expected decryption error for tampered ciphertext")
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	setKey(t, "roundtrip-key")
	for _, plain := range []string{"hello", "a", "unicode: 日本語", strings.Repeat("x", 1000)} {
		ct, err := EncryptSecret(plain)
		if err != nil {
			t.Fatalf("encrypt(%q): %v", plain, err)
		}
		got, err := DecryptSecret(ct)
		if err != nil {
			t.Fatalf("decrypt(%q): %v", ct, err)
		}
		if got != plain {
			t.Fatalf("roundtrip: want %q, got %q", plain, got)
		}
	}
}
