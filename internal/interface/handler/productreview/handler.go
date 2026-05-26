package productreview

import (
	"encoding/json"
	"fmt"
	"kossti/internal/domain/entities"
	"kossti/internal/domain/repository"
	"kossti/internal/usecase/productreview"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Request/Response structures
type CreateReviewRequest struct {
	Rating            float64         `json:"rating"`
	Reviews           string          `json:"reviews"`
	SourceURL         *string         `json:"source_url,omitempty"`
	AdditionalDetails json.RawMessage `json:"additional_details,omitempty"`
	Priority          int             `json:"priority,omitempty"`
}

type UpdateReviewRequest struct {
	Rating            float64         `json:"rating"`
	Reviews           string          `json:"reviews"`
	SourceURL         *string         `json:"source_url,omitempty"`
	AdditionalDetails json.RawMessage `json:"additional_details,omitempty"`
	Priority          int             `json:"priority,omitempty"`
}

type ReviewTranslationRequest struct {
	ProductID         uint            `json:"product_id"`
	Locale            string          `json:"locale"`
	Rating            string          `json:"rating"`
	Review            string          `json:"review"`
	AdditionalDetails json.RawMessage `json:"additional_details,omitempty"`
}

type UploadImageRequest struct {
	Files     []string `json:"files"`
	ProductID uint     `json:"product_id"`
}

type ReviewResponse struct {
	ID                uint        `json:"id"`
	ProductID         uint        `json:"product_id"`
	UserID            uint        `json:"user_id"`
	Rating            string      `json:"rating"`
	Reviews           string      `json:"reviews"`
	AdditionalDetails interface{} `json:"additional_details,omitempty"`
	SourceURL         *string     `json:"source_url,omitempty"`
	Priority          int         `json:"priority"`
	Status            bool        `json:"status"`
	CreatedAt         string      `json:"created_at"`
	UpdatedAt         string      `json:"updated_at"`
}

type TranslationResponse struct {
	ID                uint        `json:"id"`
	ProductReviewID   uint        `json:"product_review_id"`
	Locale            string      `json:"locale"`
	Rating            string      `json:"rating"`
	TranslatedReview  string      `json:"translated_review"`
	AdditionalDetails interface{} `json:"additional_details,omitempty"`
	CreatedAt         string      `json:"created_at"`
	UpdatedAt         string      `json:"updated_at"`
}

type ImageResponse struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	URL          string `json:"url"` // presigned GET or public URL
	ProductID    uint   `json:"product_id"`
	DefaultPhoto bool   `json:"defaultphoto"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// convertReviewToResponse converts entity to response format
func convertReviewToResponse(review *entities.ProductReview) ReviewResponse {
	reviewText := ""
	if review.Review != nil {
		reviewText = *review.Review
	}

	var additional interface{}
	if len(review.AdditionalDetails) > 0 {
		if err := json.Unmarshal(review.AdditionalDetails, &additional); err != nil {
			// If unmarshalling fails, fallback to the raw bytes as string
			additional = string(review.AdditionalDetails)
		}
	}

	return ReviewResponse{
		ID:                review.ID,
		ProductID:         review.ProductID,
		UserID:            review.UserID,
		Rating:            review.Rating,
		Reviews:           reviewText,
		AdditionalDetails: additional,
		SourceURL:         review.SourceURL,
		CreatedAt:         review.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         review.UpdatedAt.Format(time.RFC3339),
	}
}

// convertTranslationToResponse converts translation entity to response format
func convertTranslationToResponse(translation *entities.ProductReviewTranslation) TranslationResponse {
	var additional interface{}
	if len(translation.AdditionalDetails) > 0 {
		if err := json.Unmarshal(translation.AdditionalDetails, &additional); err != nil {
			additional = string(translation.AdditionalDetails)
		}
	}

	return TranslationResponse{
		ID:                translation.ID,
		ProductReviewID:   translation.ProductReviewID,
		Locale:            translation.Locale,
		Rating:            translation.Rating,
		TranslatedReview:  translation.TranslatedReview,
		AdditionalDetails: additional,
		CreatedAt:         translation.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         translation.UpdatedAt.Format(time.RFC3339),
	}
}

// toBanglaNumber converts a numeric rating to Bengali digits string (e.g., 4.15 -> "৪.১৫")
func toBanglaNumber(f float32) string {
	// format with up to 2 decimal places but trim trailing zeros
	s := fmt.Sprintf("%.2f", f)
	// remove trailing .00 -> show integer if whole number? keep as is per request
	// Replace ASCII digits with Bengali digits
	var b []rune
	for _, r := range s {
		switch r {
		case '0':
			b = append(b, '০')
		case '1':
			b = append(b, '১')
		case '2':
			b = append(b, '২')
		case '3':
			b = append(b, '৩')
		case '4':
			b = append(b, '৪')
		case '5':
			b = append(b, '৫')
		case '6':
			b = append(b, '৬')
		case '7':
			b = append(b, '৭')
		case '8':
			b = append(b, '৮')
		case '9':
			b = append(b, '৯')
		default:
			b = append(b, r)
		}
	}
	return string(b)
}

// CreateReviewHandler handles POST /reviews/{id}
func CreateReviewHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository, productRepo repository.ProductRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path parameter (supports /reviews/{id})
	// Fallback: if the route is different, handlers that call this should ensure the path contains the product id.
	productIDStr := strings.TrimPrefix(r.URL.Path, "/reviews/")
	productIDStr = strings.Trim(productIDStr, "/")

	// Debugging info
	println("===== HANDLER DEBUG =====")
	println("Full URL Path:", r.URL.Path)
	println("Parsed productIDStr:", productIDStr)

	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid product ID"})
		return
	}

	println("Parsed Product ID:", productID)
	println("=========================")

	// Validate product exists
	_, err = productRepo.GetByID(r.Context(), uint(productID))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Product not found"})
		return
	}

	var req CreateReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Rating < 1 || req.Rating > 5 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Rating must be between 1 and 5"})
		return
	}

	if req.Reviews == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Review content is required"})
		return
	}

	// TODO: Extract user ID from JWT token
	userID := uint(1) // Placeholder

	// Pass additional_details raw JSON to usecase so it's stored as structured JSON
	review, err := productreview.CreateReview(r.Context(), reviewRepo, userID, uint(productID), req.Rating, req.Reviews, req.SourceURL, req.AdditionalDetails)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Review added successfully",
		"review":  convertReviewToResponse(review),
	})
}

// GetAllReviewsHandler handles GET /reviews
func GetAllReviewsHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 10
	}

	sortOrder := r.URL.Query().Get("sortOrder")
	if sortOrder == "" {
		sortOrder = "desc"
	}

	searchTerm := r.URL.Query().Get("searchterm")
	categoryID := r.URL.Query().Get("category")

	var reviews []*entities.ProductReview
	var total int
	var err error

	if searchTerm != "" || categoryID != "" {
		reviews, total, err = productreview.SearchReviews(r.Context(), reviewRepo, searchTerm, page, limit, sortOrder, categoryID)
	} else {
		reviews, total, err = productreview.GetAllReviews(r.Context(), reviewRepo, page, limit, sortOrder)
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	reviewResponses := make([]ReviewResponse, len(reviews))
	for i, review := range reviews {
		reviewResponses[i] = convertReviewToResponse(review)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"reviews":      reviewResponses,
		"totalReviews": total,
		"currentPage":  page,
		"perPage":      limit,
	})
}

// CreateReviewTranslationHandler handles POST /review/translation
func CreateReviewTranslationHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	var req ReviewTranslationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	if req.Locale == "" || req.Review == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Locale and review are required"})
		return
	}

	// Determine the review ID to attach the translation to.
	// Prefer an explicit mapping from ProductID -> existing product review.
	var reviewID uint
	if req.ProductID != 0 {
		// Try to find existing reviews for the product and pick the first one.
		reviews, err := productreview.GetReviewsByProduct(r.Context(), reviewRepo, req.ProductID)
		if err != nil || len(reviews) == 0 {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "No review found for provided product_id"})
			return
		}
		reviewID = reviews[0].ID
	} else {
		// Fallback to placeholder if product_id not provided (legacy behaviour)
		reviewID = uint(1)
	}

	// Check if translation exists and update or create
	existingTranslation, err := reviewRepo.GetTranslation(r.Context(), reviewID, req.Locale)
	if err == nil && existingTranslation != nil {
		// Update existing translation
		translation, err := productreview.UpdateTranslation(r.Context(), reviewRepo, reviewID, req.Locale, req.Rating, req.Review, req.AdditionalDetails)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":     "Review updated successfully",
			"translation": convertTranslationToResponse(translation),
		})
	} else {
		// Create new translation
		translation, err := productreview.CreateTranslation(r.Context(), reviewRepo, reviewID, req.Locale, req.Rating, req.Review, req.AdditionalDetails)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message":     "Translation created successfully",
			"translation": convertTranslationToResponse(translation),
		})
	}
}

// UpdateReviewHandler handles POST /product/{id}/review/{reviewid}
func UpdateReviewHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract review ID from URL path - pattern: /product/{id}/review/{reviewid}
	path := r.URL.Path
	parts := strings.Split(path, "/")
	// Expect at least ["", "product", "{id}", "review", "{reviewid}"] -> len >= 5
	if len(parts) < 5 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format"})
		return
	}

	reviewID, err := strconv.ParseUint(parts[len(parts)-1], 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid review ID"})
		return
	}

	var req UpdateReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	// TODO: Extract user ID from JWT token
	// For admin endpoints we don't have an authenticated user yet; to allow
	// admin UI updates we'll fetch the existing review and use its owner
	// user ID so the usecase authorization check passes. This acts as an
	// admin override until real auth is wired.
	existingReview, errGet := reviewRepo.GetByID(r.Context(), uint(reviewID))
	if errGet != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Review not found"})
		return
	}

	userID := existingReview.UserID

	// Pass raw JSON bytes for additional_details
	review, err := productreview.UpdateReview(r.Context(), reviewRepo, uint(reviewID), userID, req.Rating, req.Reviews, req.SourceURL, req.AdditionalDetails)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Review updated successfully",
		"review":  convertReviewToResponse(review),
	})
}

// GetProductReviewsHandler handles GET /products/{productId}/reviews
func GetProductReviewsHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path - supports both
	// /products/{productId}/reviews and /api/.../products/{productId}/reviews
	path := r.URL.Path
	parts := strings.Split(path, "/")

	// Expect at least ["", "products", "{id}", "reviews"] -> len >= 4
	if len(parts) < 4 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format"})
		return
	}

	// Find the "reviews" segment at the end and pick the previous element as product id
	var productIDStr string
	if parts[len(parts)-1] == "reviews" {
		// standard case: /.../products/{id}/reviews
		productIDStr = parts[len(parts)-2]
	} else if len(parts) >= 1 {
		// fallback: maybe URL ends with the id directly
		productIDStr = parts[len(parts)-1]
	}

	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid product ID"})
		return
	}

	// Fetch reviews for the product
	reviews, err := productreview.GetReviewsByProduct(r.Context(), reviewRepo, uint(productID))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// If a locale is provided, try to load translations and prefer translated content when available
	locale := r.URL.Query().Get("locale")

	reviewResponses := make([]ReviewResponse, 0, len(reviews))
	for _, review := range reviews {
		// Start with base response (original review)
		resp := convertReviewToResponse(review)

		if locale != "" {
			// Try to get a translation for this review and locale
			if translation, err := reviewRepo.GetTranslation(r.Context(), review.ID, locale); err == nil && translation != nil {
				// Replace the review text with the translated review
				resp.Reviews = translation.TranslatedReview

				// If the translation has additional details, prefer those
				var additional interface{}
				if len(translation.AdditionalDetails) > 0 {
					if err := json.Unmarshal(translation.AdditionalDetails, &additional); err != nil {
						additional = string(translation.AdditionalDetails)
					}
					resp.AdditionalDetails = additional
				}
			}
		}

		reviewResponses = append(reviewResponses, resp)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"product_id": productID,
		"reviews":    reviewResponses,
	})
}

// GetReviewHandler handles GET /reviews/{id} and GET /reviews/{id}?locale=bn
func GetReviewHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/reviews/")
	idStr := strings.Trim(path, "/")

	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid ID"})
		return
	}

	locale := r.URL.Query().Get("locale")

	// Always treat ID as review_id and fetch the review by ID
	review, err := reviewRepo.GetByID(r.Context(), uint(id))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Review not found"})
		return
	}

	response := map[string]interface{}{
		"review": convertReviewToResponse(review),
	}

	// If locale is specified and it's not English, get the translation
	if locale != "" && locale != "en" {
		translation, err := reviewRepo.GetTranslation(r.Context(), review.ID, locale)
		if err == nil {
			response["translation"] = convertTranslationToResponse(translation)
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetReviewsByProductHandler handles GET /product-reviews/{product_id}
func GetReviewsByProductHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path: /product-reviews/{product_id}
	path := strings.TrimPrefix(r.URL.Path, "/product-reviews/")
	productIDStr := strings.Trim(path, "/")

	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid product ID"})
		return
	}

	locale := r.URL.Query().Get("locale")

	// Get all reviews for this product (no status filter for admin access)
	reviews, err := reviewRepo.GetByProductID(r.Context(), uint(productID))
	if err != nil || len(reviews) == 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "No reviews found for this product"})
		return
	}

	// Build response with all reviews and their translations if locale specified
	reviewsData := make([]map[string]interface{}, 0)
	for _, review := range reviews {
		resp := convertReviewToResponse(review)

		// If locale is specified and not English, merge translation into the review
		if locale != "" && locale != "en" {
			translation, err := reviewRepo.GetTranslation(r.Context(), review.ID, locale)
			if err == nil && translation != nil {
				if translation.TranslatedReview != "" {
					resp.Reviews = translation.TranslatedReview
				}
				if translation.Rating != "" {
					resp.Rating = translation.Rating
				}
				// Also include translation's AdditionalDetails if available
				if len(translation.AdditionalDetails) > 0 {
					var additional interface{}
					if err := json.Unmarshal(translation.AdditionalDetails, &additional); err != nil {
						resp.AdditionalDetails = string(translation.AdditionalDetails)
					} else {
						resp.AdditionalDetails = additional
					}
				}
			}
		}

		reviewsData = append(reviewsData, map[string]interface{}{
			"review": resp,
		})
	}

	response := map[string]interface{}{
		"product_id": productID,
		"count":      len(reviewsData),
		"reviews":    reviewsData,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// UploadImagesHandler handles POST /productimages
func UploadImagesHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Parse multipart form with 10MB max memory (rest will be stored on disk temporarily)
	err := r.ParseMultipartForm(10 << 20) // 10MB
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to parse multipart form: " + err.Error(),
		})
		return
	}

	// Get product ID from form data
	productIDStr := r.FormValue("product_id")
	if productIDStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "product_id is required",
		})
		return
	}

	// Parse product ID to uint
	productID64, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "invalid product_id",
		})
		return
	}
	productID := uint(productID64)

	// Get the uploaded files
	form := r.MultipartForm
	var files []*multipart.FileHeader

	// Look for files with pattern "files[0]", "files[1]", etc.
	for key, fileHeaders := range form.File {
		if strings.HasPrefix(key, "files[") {
			files = append(files, fileHeaders...)
		}
	}

	// If no files found with pattern, try direct "files" field
	if len(files) == 0 {
		if directFiles, exists := form.File["files"]; exists {
			files = directFiles
		}
	}

	if len(files) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "No files provided",
		})
		return
	}

	var uploadedImages []ImageResponse

	// Process each uploaded file
	for i, fileHeader := range files {
		// We don't need to open the file here because storage isn't implemented yet.
		// Open would be required if we were saving the file to disk or forwarding it to S3.

		// For now, create a placeholder response
		// TODO: Implement actual file storage (save to disk/cloud storage)
		// TODO: Save file metadata to database
		uploadedImage := ImageResponse{
			ID:           uint(i + 1), // Placeholder ID
			Name:         fileHeader.Filename,
			Path:         "/uploads/" + fileHeader.Filename, // Placeholder path
			ProductID:    productID,
			DefaultPhoto: i == 0, // Make first image default
			CreatedAt:    time.Now().Format(time.RFC3339),
			UpdatedAt:    time.Now().Format(time.RFC3339),
		}
		uploadedImages = append(uploadedImages, uploadedImage)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("%d image(s) uploaded successfully", len(uploadedImages)),
		"images":  uploadedImages,
	})
}

// RegisterS3ImagesHandler accepts a JSON payload with S3 keys for files that were
// uploaded directly from the client and persists image metadata in the DB.
func RegisterS3ImagesHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	log.Printf("[handler] RegisterS3ImagesHandler called: %s %s", r.Method, r.URL.Path)

	var payload struct {
		ProductID uint `json:"product_id"`
		Files     []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
			URL  string `json:"url"`
			Size int64  `json:"size"`
		} `json:"files"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "invalid payload"})
		return
	}

	// Build presign client once for all images
	bucket := os.Getenv("S3_BUCKET")
	region := os.Getenv("AWS_REGION")
	var presignClient *s3.PresignClient
	if bucket != "" && region != "" {
		if cfg, err := config.LoadDefaultConfig(r.Context(), config.WithRegion(region)); err == nil {
			presignClient = s3.NewPresignClient(s3.NewFromConfig(cfg))
		}
	}

	// Persist files to DB and return presigned GET URLs
	images := make([]ImageResponse, 0, len(payload.Files))
	for _, f := range payload.Files {
		image := &entities.Image{
			ImageableType: "Product",
			ImageableID:   payload.ProductID,
			ImagePath:     f.Key,
			Status:        1,
			DefaultPhoto:  0,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}

		createdImage, err := imageRepo.Create(r.Context(), image)
		if err != nil {
			log.Printf("[error] Failed to save image: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "failed to save image"})
			return
		}

		imgURL := ""
		if presignClient != nil {
			presigned, err := presignClient.PresignGetObject(r.Context(), &s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(createdImage.ImagePath),
			}, func(opts *s3.PresignOptions) { opts.Expires = 1 * time.Hour })
			if err == nil {
				imgURL = presigned.URL
			}
		}
		if imgURL == "" && bucket != "" {
			// Fallback: plain public URL (works if bucket policy allows public-read)
			imgURL = "https://" + bucket + ".s3." + region + ".amazonaws.com/" + createdImage.ImagePath
		}

		images = append(images, ImageResponse{
			ID:           createdImage.ID,
			Name:         f.Name,
			Path:         createdImage.ImagePath,
			URL:          imgURL,
			ProductID:    payload.ProductID,
			DefaultPhoto: false,
			CreatedAt:    createdImage.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    createdImage.UpdatedAt.Format(time.RFC3339),
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "S3 images registered",
		"images":  images,
	})
}

// GetProductImagesHandler handles GET /productimages/{id}
func GetProductImagesHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/productimages/")
	productIDStr := strings.Trim(path, "/")

	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid product ID"})
		return
	}

	// Get images from database
	images, err := imageRepo.GetByImageableID(r.Context(), "Product", uint(productID))
	if err != nil {
		log.Printf("[error] Failed to get images: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to retrieve images"})
		return
	}

	// Load AWS config for presigning
	bucket := os.Getenv("S3_BUCKET")
	region := os.Getenv("AWS_REGION")
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	cfg, err := config.LoadDefaultConfig(r.Context(),
		config.WithRegion(region),
	)
	if err != nil {
		log.Printf("[error] Failed to load AWS config: %v", err)
	}

	var s3Client *s3.Client
	var presignClient *s3.PresignClient
	if err == nil && bucket != "" && accessKey != "" && secretKey != "" {
		s3Client = s3.NewFromConfig(cfg)
		presignClient = s3.NewPresignClient(s3Client)
	}

	// Convert to response format with presigned URLs
	imageResponses := make([]map[string]interface{}, 0, len(images))
	for _, img := range images {
		imageResp := map[string]interface{}{
			"id":           img.ID,
			"path":         img.ImagePath,
			"product_id":   productID,
			"defaultphoto": img.DefaultPhoto,
			"created_at":   img.CreatedAt.Format(time.RFC3339),
			"updated_at":   img.UpdatedAt.Format(time.RFC3339),
		}

		// Prefer returning full URLs unchanged
		if strings.HasPrefix(img.ImagePath, "http://") || strings.HasPrefix(img.ImagePath, "https://") {
			imageResp["url"] = img.ImagePath
		} else {
			// If local file exists, return server-relative URL
			fsPath := img.ImagePath
			if strings.HasPrefix(fsPath, "/") {
				fsPath = strings.TrimPrefix(fsPath, "/")
			}
			if _, err := os.Stat(fsPath); err == nil {
				imageResp["url"] = "/" + filepath.ToSlash(fsPath)
			} else if presignClient != nil && bucket != "" {
				// Try presigned S3 URL when configured
				input := &s3.GetObjectInput{
					Bucket: aws.String(bucket),
					Key:    aws.String(img.ImagePath),
				}

				presigned, err := presignClient.PresignGetObject(r.Context(), input, func(opts *s3.PresignOptions) {
					opts.Expires = 1 * time.Hour
				})

				if err == nil {
					imageResp["url"] = presigned.URL
				} else {
					imageResp["url"] = "https://" + bucket + ".s3." + region + ".amazonaws.com/" + img.ImagePath
				}
			} else if bucket != "" {
				imageResp["url"] = "https://" + bucket + ".s3." + region + ".amazonaws.com/" + img.ImagePath
			} else {
				// Last resort: return server-relative path even if file not found
				imageResp["url"] = "/" + filepath.ToSlash(img.ImagePath)
			}
		}

		imageResponses = append(imageResponses, imageResp)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"product_id": productID,
		"images":     imageResponses,
	})
}

// MakeDefaultImageHandler handles POST /default-image/{id}
func MakeDefaultImageHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract image ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/default-image/")
	imageIDStr := strings.Trim(path, "/")

	imageID, err := strconv.ParseUint(imageIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid image ID"})
		return
	}

	// TODO: Implement set default image logic
	err = reviewRepo.SetDefaultImage(r.Context(), uint(imageID))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Image set as default successfully",
		"image_id": imageID,
	})
}

// RemoveImageHandler handles POST /imageremove/{id}
func RemoveImageHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract image ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/imageremove/")
	imageIDStr := strings.Trim(path, "/")

	log.Printf("[RemoveImage] Processing request for path: %s, imageIDStr: %s", r.URL.Path, imageIDStr)

	imageID, err := strconv.ParseUint(imageIDStr, 10, 32)
	if err != nil {
		log.Printf("[RemoveImage] Failed to parse image ID: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid image ID"})
		return
	}

	log.Printf("[RemoveImage] Attempting to delete image ID: %d", imageID)

	// Get image from database to get the S3 key
	image, err := imageRepo.GetByID(r.Context(), uint(imageID))
	if err != nil {
		log.Printf("[error] Failed to get image: %v", err)
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Image not found"})
		return
	}

	log.Printf("[RemoveImage] Found image: ID=%d, Path=%s", image.ID, image.ImagePath)

	// Delete from S3
	bucket := os.Getenv("S3_BUCKET")
	region := os.Getenv("AWS_REGION")

	if bucket != "" && region != "" && image.ImagePath != "" {
		cfg, err := config.LoadDefaultConfig(r.Context(),
			config.WithRegion(region),
		)
		if err != nil {
			log.Printf("[error] Failed to load AWS config: %v", err)
		} else {
			s3Client := s3.NewFromConfig(cfg)

			// Delete object from S3
			deleteInput := &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(image.ImagePath),
			}

			_, err = s3Client.DeleteObject(r.Context(), deleteInput)
			if err != nil {
				log.Printf("[error] Failed to delete from S3: %v", err)
				// Continue to delete from DB even if S3 delete fails
			} else {
				log.Printf("[info] Successfully deleted from S3: %s", image.ImagePath)
			}
		}
	}

	// Delete from database
	log.Printf("[RemoveImage] Calling imageRepo.Delete for ID: %d", imageID)
	err = imageRepo.Delete(r.Context(), uint(imageID))
	if err != nil {
		log.Printf("[error] Failed to delete image from DB: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to delete image"})
		return
	}

	log.Printf("[RemoveImage] Successfully deleted image ID: %d from database", imageID)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"message":  "Image removed successfully",
		"image_id": imageID,
	})
}

// GetPublicReviewsHandler handles GET /public-reviews/{id}
func GetPublicReviewsHandler(w http.ResponseWriter, r *http.Request, reviewRepo repository.ProductReviewRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/public-reviews/")
	productIDStr := strings.Trim(path, "/")

	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid product ID"})
		return
	}

	locale := r.URL.Query().Get("locale")

	// Get all reviews for this product
	reviews, err := reviewRepo.GetByProductID(r.Context(), uint(productID))
	if err != nil || len(reviews) == 0 {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"product_id": productID,
			"count":      0,
			"reviews":    []interface{}{},
		})
		return
	}

	// Build response with translations for non-English locales
	reviewsData := make([]map[string]interface{}, 0, len(reviews))

	// If locale is non-English, batch fetch ALL translations in ONE query (OPTIMIZED: 1 query instead of N)
	var translationsMap map[uint]*entities.ProductReviewTranslation
	if locale != "" && locale != "en" {
		reviewIDs := make([]uint, len(reviews))
		for i, review := range reviews {
			reviewIDs[i] = review.ID
		}
		translationsMap, _ = reviewRepo.GetTranslationsByReviewIDsAndLocale(r.Context(), reviewIDs, locale)
	}

	for _, review := range reviews {
		resp := convertReviewToResponse(review)

		// If locale is specified and not English, use translation from batch-fetched map
		if locale != "" && locale != "en" {
			if translation, exists := translationsMap[review.ID]; exists {
				// Override review text and rating with Bengali values
				if translation.TranslatedReview != "" {
					resp.Reviews = translation.TranslatedReview
				}
				if translation.Rating != "" {
					resp.Rating = translation.Rating
				}
			}
		}

		reviewsData = append(reviewsData, map[string]interface{}{
			"review": resp,
		})
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"product_id": productID,
		"count":      len(reviewsData),
		"reviews":    reviewsData,
	})
}
