package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"torrent-search-go/internal/config"
)

// ObjectInfo holds metadata for a stored object returned by ListObjects.
type ObjectInfo struct {
	Key          string
	LastModified time.Time
}

// ObjectStorage wraps a lightweight S3-compatible client (MinIO SDK) for
// storing private cover images and generating presigned GET URLs.
type ObjectStorage struct {
	client     *minio.Client
	cfg        config.S3Config
	httpClient *http.Client
}

// NewObjectStorage creates an S3-compatible object storage client from config.
func NewObjectStorage(cfg config.S3Config) (*ObjectStorage, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("S3 endpoint, bucket, access key and secret access key are required")
	}

	secure := strings.HasPrefix(strings.ToLower(endpoint), "https://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")

	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Region:       cfg.Region,
		Secure:       secure,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	return &ObjectStorage{
		client: client,
		cfg:    cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// IsEnabled reports whether object storage is configured and ready.
func (s *ObjectStorage) IsEnabled() bool {
	return s != nil && s.client != nil && s.cfg.Enabled
}

// KeyPrefix returns the configured key prefix (sanitized).
func (s *ObjectStorage) KeyPrefix() string {
	prefix := strings.Trim(strings.TrimSpace(s.cfg.KeyPrefix), "/")
	if prefix == "" {
		return "covers"
	}
	return prefix
}

// TempPrefix returns the object key prefix used for temporary (non-favorite) covers.
func (s *ObjectStorage) TempPrefix() string {
	return s.KeyPrefix() + "/temp/"
}

// CoverKey builds the full object key for a cover image.
// Keys use the configured prefix plus a hash-based filename so they are
// deterministic from the image content and clearly split into keep/temp folders.
func (s *ObjectStorage) CoverKey(base string, imageData []byte, isTemp bool) string {
	folder := "keep"
	if isTemp {
		folder = "temp"
	}

	h := sha256.New()
	if len(imageData) > 0 {
		h.Write(imageData)
	} else {
		h.Write([]byte(base))
	}
	hash := hex.EncodeToString(h.Sum(nil))

	return fmt.Sprintf("%s/%s/%s.jpg", s.KeyPrefix(), folder, hash)
}

// UploadCover stores imageData in the bucket and returns a presigned GET URL.
func (s *ObjectStorage) UploadCover(ctx context.Context, key string, imageData []byte, isTemp bool) (string, error) {
	objectKey := s.CoverKey(key, imageData, isTemp)
	contentType := http.DetectContentType(imageData)
	if contentType == "" {
		contentType = "image/jpeg"
	}

	_, err := s.client.PutObject(ctx, s.cfg.Bucket, objectKey, bytes.NewReader(imageData), int64(len(imageData)), minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: "public, max-age=31536000, immutable",
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload cover to S3: %w", err)
	}

	return s.GetPresignedURL(ctx, objectKey)
}

// UploadCoverFromURL fetches an image from imageURL and uploads it to object storage.
// It returns the full object key and a presigned GET URL.
func (s *ObjectStorage) UploadCoverFromURL(ctx context.Context, baseKey, imageURL string, isTemp bool) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("source fetch failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("source fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("source fetch HTTP %d", resp.StatusCode)
	}

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("source fetch read failed: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(imageData)
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}

	objectKey := s.CoverKey(baseKey, imageData, isTemp)

	_, err = s.client.PutObject(ctx, s.cfg.Bucket, objectKey, bytes.NewReader(imageData), int64(len(imageData)), minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: "public, max-age=31536000, immutable",
	})
	if err != nil {
		return "", "", fmt.Errorf("failed to upload cover to S3: %w", err)
	}

	presignedURL, err := s.GetPresignedURL(ctx, objectKey)
	if err != nil {
		return objectKey, "", err
	}

	return objectKey, presignedURL, nil
}

// GetPresignedURL generates a fresh presigned GET URL for the given object key.
func (s *ObjectStorage) GetPresignedURL(ctx context.Context, key string) (string, error) {
	expires := time.Duration(s.cfg.PresignDays) * 24 * time.Hour
	maxExpires := 7 * 24 * time.Hour
	if expires > maxExpires {
		expires = maxExpires
	}
	if expires <= 0 {
		expires = maxExpires
	}

	u, err := s.client.PresignedGetObject(ctx, s.cfg.Bucket, key, expires, nil)
	if err != nil {
		return "", fmt.Errorf("failed to presign S3 URL: %w", err)
	}
	return u.String(), nil
}

// DeleteObject removes an object by key.
func (s *ObjectStorage) DeleteObject(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.cfg.Bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete S3 object %s: %w", key, err)
	}
	return nil
}

// ListObjects returns metadata for all objects under the given prefix.
func (s *ObjectStorage) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := s.client.ListObjects(ctx, s.cfg.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var out []ObjectInfo
	for obj := range ch {
		if obj.Err != nil {
			return out, obj.Err
		}
		out = append(out, ObjectInfo{
			Key:          obj.Key,
			LastModified: obj.LastModified,
		})
	}
	return out, nil
}
