package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
)

func encryptionKey() ([]byte, error) {
	secret := os.Getenv("REAL_DEBRID_ENCRYPTION_KEY")
	if secret == "" {
		secret = os.Getenv("SESSION_SECRET")
	}
	if secret == "" {
		return nil, errors.New("SESSION_SECRET is required for secret encryption")
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:], nil
}

// EncryptSecret encrypts plaintext using AES-256-GCM (Node-compatible v1 format).
func EncryptSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, err := encryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	tagSize := gcm.Overhead()
	data := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]

	return "v1:" +
		base64.StdEncoding.EncodeToString(nonce) + ":" +
		base64.StdEncoding.EncodeToString(tag) + ":" +
		base64.StdEncoding.EncodeToString(data), nil
}

// DecryptSecret decrypts a v1 ciphertext or returns legacy plaintext as-is.
func DecryptSecret(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !strings.HasPrefix(ciphertext, "v1:") {
		return ciphertext, nil
	}

	parts := strings.Split(ciphertext, ":")
	if len(parts) != 4 {
		return "", errors.New("invalid encrypted secret format")
	}

	iv, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	tag, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return "", err
	}

	key, err := encryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	combined := append(data, tag...)
	plain, err := gcm.Open(nil, iv, combined, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
