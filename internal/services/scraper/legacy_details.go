package scraper

import "torrent-search-go/internal/models"

func failedLegacyDetails(message string) *models.TorrentDetails {
	return &models.TorrentDetails{
		Description: "Failed to load description",
		Files:       []models.File{},
		Comments:    []models.TorrentComment{},
		Images:      []models.TorrentImageLink{},
		Error:       message,
	}
}
