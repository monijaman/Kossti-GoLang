package product

import (
	"context"
	"encoding/json"
	"fmt"
	"kossti/internal/domain/entities"
	"kossti/internal/domain/repository"
	"net/http"
	"strconv"
	"strings"
)

// VideoItem represents a video entry
type VideoItem struct {
	ID         uint   `json:"id"`
	URL        string `json:"url"`
	YoutubeURL string `json:"youtubeUrl,omitempty"`
}

// GetProductVideosHandler handles GET /product-videos/{id}?locale=bn
// Fetches YouTube URLs from product_reviews.additional_details or product_review_translations.additional_details
func GetProductVideosHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/product-videos/")
	productIDStr := strings.Trim(path, "/")

	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid product ID"})
		return
	}

	// Get locale from query parameter (default to "en" for English content)
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		locale = "en"
	}

	// Debug logging
	fmt.Printf("[VideoHandler] ProductID: %d, Locale: %s\n", productID, locale)

	// Get reviews for this product
	reviews, err := reviewRepo.GetByProductID(context.Background(), uint(productID))
	if err != nil {
		fmt.Printf("[VideoHandler] Error fetching reviews: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to fetch reviews"})
		return
	}

	fmt.Printf("[VideoHandler] Found %d reviews for product %d\n", len(reviews), productID)

	// If locale is not English, fetch translations
	var translations map[uint]*entities.ProductReviewTranslation
	if locale != "en" && len(reviews) > 0 {
		// Collect review IDs
		reviewIDs := make([]uint, 0, len(reviews))
		for _, review := range reviews {
			reviewIDs = append(reviewIDs, review.ID)
		}

		fmt.Printf("[VideoHandler] Fetching translations for %d reviews, locale: %s\n", len(reviewIDs), locale)

		// Batch fetch translations for all reviews
		translations, err = reviewRepo.GetTranslationsByReviewIDsAndLocale(context.Background(), reviewIDs, locale)
		if err != nil {
			// Log error but continue with original reviews
			fmt.Printf("[VideoHandler] Error fetching translations: %v\n", err)
			translations = make(map[uint]*entities.ProductReviewTranslation)
		} else {
			fmt.Printf("[VideoHandler] Found %d translations\n", len(translations))
		}
	}

	// Extract video URLs from additional_details (either from translations or original reviews)
	var videos []VideoItem
	videoIDCounter := uint(1) // Counter for generating unique video IDs

	for _, review := range reviews {
		var additionalDetailsJSON json.RawMessage

		// Check if translation exists for this review
		if translation, hasTranslation := translations[review.ID]; hasTranslation && len(translation.AdditionalDetails) > 0 {
			// Use translation's additional_details
			additionalDetailsJSON = translation.AdditionalDetails
			fmt.Printf("[VideoHandler] Review %d: Using translation additional_details (length: %d)\n", review.ID, len(additionalDetailsJSON))
		} else if len(review.AdditionalDetails) > 0 {
			// Fallback to original review's additional_details
			additionalDetailsJSON = review.AdditionalDetails
			fmt.Printf("[VideoHandler] Review %d: Using original additional_details (length: %d)\n", review.ID, len(additionalDetailsJSON))
		} else {
			// No data available
			fmt.Printf("[VideoHandler] Review %d: No additional_details available\n", review.ID)
			continue
		}

		// Try to parse additional_details as JSON
		var additionalData interface{}
		if err := json.Unmarshal(additionalDetailsJSON, &additionalData); err != nil {
			// If it's not JSON, treat it as a plain string (might be a direct URL)
			url := strings.TrimSpace(string(additionalDetailsJSON))
			fmt.Printf("[VideoHandler] Review %d: Failed to parse as JSON, treating as string: %s\n", review.ID, url)
			if isYouTubeURL(url) {
				videos = append(videos, VideoItem{
					ID:         videoIDCounter,
					URL:        url,
					YoutubeURL: url,
				})
				videoIDCounter++
			}
			continue
		}

		fmt.Printf("[VideoHandler] Review %d: Parsed JSON type: %T\n", review.ID, additionalData)

		// Handle different JSON structures
		extractedVideos := extractVideosFromJSON(additionalData, &videoIDCounter)
		fmt.Printf("[VideoHandler] Review %d: Extracted %d videos\n", review.ID, len(extractedVideos))
		videos = append(videos, extractedVideos...)
	}

	fmt.Printf("[VideoHandler] Total videos extracted: %d\n", len(videos))

	// Return the videos
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data":   videos,
		"videos": videos, // Support both field names for compatibility
	})
}

// extractVideosFromJSON extracts video items from various JSON structures
func extractVideosFromJSON(data interface{}, videoIDCounter *uint) []VideoItem {
	var videos []VideoItem

	switch d := data.(type) {
	case string:
		// Simple string URL
		if isYouTubeURL(d) {
			videos = append(videos, VideoItem{
				ID:         *videoIDCounter,
				URL:        d,
				YoutubeURL: d,
			})
			*videoIDCounter++
		}

	case map[string]interface{}:
		// Object with url/youtubeUrl field
		videoURL := ""

		if url, ok := d["url"].(string); ok {
			videoURL = url
		} else if url, ok := d["youtubeUrl"].(string); ok {
			videoURL = url
		} else if url, ok := d["youtube_url"].(string); ok {
			videoURL = url
		}

		if isYouTubeURL(videoURL) {
			videos = append(videos, VideoItem{
				ID:         *videoIDCounter,
				URL:        videoURL,
				YoutubeURL: videoURL,
			})
			*videoIDCounter++
		}

	case []interface{}:
		// Array of video objects
		for _, item := range d {
			if videoObj, ok := item.(map[string]interface{}); ok {
				videoURL := ""

				if url, ok := videoObj["url"].(string); ok {
					videoURL = url
				} else if url, ok := videoObj["youtubeUrl"].(string); ok {
					videoURL = url
				} else if url, ok := videoObj["youtube_url"].(string); ok {
					videoURL = url
				}

				if isYouTubeURL(videoURL) {
					videos = append(videos, VideoItem{
						ID:         *videoIDCounter,
						URL:        videoURL,
						YoutubeURL: videoURL,
					})
					*videoIDCounter++
				}
			}
		}
	}

	return videos
}

// isYouTubeURL checks if a URL is a valid YouTube URL
func isYouTubeURL(url string) bool {
	if url == "" {
		return false
	}
	url = strings.ToLower(url)
	return strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be")
}
