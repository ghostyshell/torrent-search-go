package jobs

import (
	"context"
	"log"
	"time"
)

type refreshResult struct {
	processed int
	refreshed int
	failed    int
}

type cleanupResult struct {
	total   int
	deleted int
	failed  int
}

// runCoverStorageMaintenance refreshes presigned URLs for stored covers and deletes
// expired temp cover objects plus their DB rows.
func runCoverStorageMaintenance(ctx context.Context, r *Runner) (map[string]interface{}, error) {
	if r.objectStorage == nil || !r.objectStorage.IsEnabled() {
		return map[string]interface{}{
			"success": true,
			"skipped": true,
			"reason":  "S3 not enabled",
		}, nil
	}

	refresh, err := r.refreshPresignedUrls(ctx)
	if err != nil {
		return nil, err
	}

	cleanup, err := r.cleanupExpiredTemp(ctx)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"success":       true,
		"refreshed":     refresh.refreshed,
		"processed":     refresh.processed,
		"failedRefresh": refresh.failed,
		"deletedTemp":   cleanup.deleted,
		"failedDelete":  cleanup.failed,
		"totalTemp":     cleanup.total,
	}
	return result, nil
}

func (r *Runner) refreshPresignedUrls(ctx context.Context) (refreshResult, error) {
	const page = 200
	offset := 0
	res := refreshResult{}

	for {
		rows, err := r.storage.GetObjectStorageCovers(ctx, page, offset)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			url, err := r.objectStorage.GetPresignedURL(ctx, row.StorageKey)
			if err != nil {
				res.failed++
				res.processed++
				continue
			}
			if _, err := r.storage.UpdateCoverPresignedURL(ctx, row.TorrentKey, url); err != nil {
				res.failed++
				res.processed++
				continue
			}
			res.refreshed++
			res.processed++
		}

		offset += len(rows)
		if len(rows) < page {
			break
		}
	}

	log.Printf("[CoverRefresh] refreshed %d/%d presigned URLs (failed %d)", res.refreshed, res.processed, res.failed)
	return res, nil
}

func (r *Runner) cleanupExpiredTemp(ctx context.Context) (cleanupResult, error) {
	res := cleanupResult{}
	objects, err := r.objectStorage.ListObjects(ctx, r.objectStorage.TempPrefix())
	if err != nil {
		return res, err
	}
	res.total = len(objects)

	cutoff := time.Now().Add(-time.Duration(r.cfg.S3.TempExpireDays) * 24 * time.Hour)
	for _, obj := range objects {
		if obj.LastModified.IsZero() || obj.LastModified.After(cutoff) {
			continue
		}
		if err := r.objectStorage.DeleteObject(ctx, obj.Key); err != nil {
			res.failed++
			continue
		}
		if _, err := r.storage.DeleteCoverByStorageKey(ctx, obj.Key); err != nil {
			res.failed++
			continue
		}
		res.deleted++
	}

	log.Printf("[CoverCleanup] removed %d expired temp covers older than %dd (of %d temp objects, failed %d)", res.deleted, r.cfg.S3.TempExpireDays, res.total, res.failed)
	return res, nil
}
