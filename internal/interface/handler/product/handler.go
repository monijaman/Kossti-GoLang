package product

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"kossti/internal/domain/entities"
	"kossti/internal/domain/repository"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// CategoryResponse represents the response format for categories
type CategoryResponse struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// BrandResponse represents the response format for brands
type BrandResponse struct {
	ID             uint    `json:"id"`
	Name           string  `json:"name"`
	TranslatedName *string `json:"translated_name,omitempty"`
	Slug           string  `json:"slug"`
}

// ProductResponse represents the response format for products
type ProductResponse struct {
	ID             uint              `json:"id"`
	Name           string            `json:"name"`
	TranslatedName *string           `json:"translated_name,omitempty"`
	Description    *string           `json:"description,omitempty"`
	Slug           string            `json:"slug"`
	Price          float64           `json:"price"`
	StartPrice     *float64          `json:"start_price,omitempty"`
	EndPrice       *float64          `json:"end_price,omitempty"`
	AverageRating  *float64          `json:"average_rating,omitempty"`
	CategoryID     *uint             `json:"category_id,omitempty"`
	CategorySlug   *string           `json:"category_slug,omitempty"`
	BrandID        *uint             `json:"brand_id,omitempty"`
	BrandSlug      *string           `json:"brand_slug,omitempty"`
	Category       *CategoryResponse `json:"category,omitempty"`
	Brand          *BrandResponse    `json:"brand,omitempty"`
	Photo          *string           `json:"photo,omitempty"`
	DefaultPhoto   *int              `json:"defaultphoto,omitempty"`
	ViewsCount     int64             `json:"views_count"`
	Status         bool              `json:"status"`
	Priority       int               `json:"priority"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
}

// ProductListResponse represents paginated product list response
type ProductListResponse struct {
	Products []ProductResponse `json:"products"`
	Count    int               `json:"count"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
	Total    int64             `json:"total,omitempty"`
}

// convertProductToResponse converts domain entity to response format
// OPTIMIZED: Only uses preloaded data, no fallback queries to prevent N+1
// imagesMap is optional - if provided, uses pre-fetched images; otherwise none
func convertProductToResponse(product *entities.Product, categoryRepo repository.CategoryRepository, brandRepo repository.BrandRepository, imageRepo repository.ImageRepository, imagesMap ...map[uint]*entities.Image) ProductResponse {
	response := ProductResponse{
		ID:            product.ID,
		Name:          product.Name,
		Description:   product.Description,
		Slug:          product.Slug,
		Price:         product.Price,
		StartPrice:    product.StartPrice,
		EndPrice:      product.EndPrice,
		AverageRating: product.AverageRating,
		CategoryID:    product.CategoryID,
		BrandID:       product.BrandID,
		ViewsCount:    product.ViewsCount,
		Status:        product.Status,
		Priority:      product.Priority,
		CreatedAt:     product.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     product.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	// Use preloaded category information (OPTIMIZED: no fallback query)
	if product.Category != nil {
		response.CategorySlug = &product.Category.Slug
		response.Category = &CategoryResponse{
			ID:   product.Category.ID,
			Name: product.Category.Name,
			Slug: product.Category.Slug,
		}
	}

	// Use preloaded brand information (OPTIMIZED: no fallback query)
	if product.Brand != nil {
		response.BrandSlug = &product.Brand.Slug
		response.Brand = &BrandResponse{
			ID:   product.Brand.ID,
			Name: product.Brand.Name,
			Slug: product.Brand.Slug,
		}
	}

	// Use pre-fetched images from imagesMap if available (batch loaded)
	if len(imagesMap) > 0 && imagesMap[0] != nil {
		if img, exists := imagesMap[0][product.ID]; exists {
			photoURL := generateImageURL(img.ImagePath)
			response.Photo = &photoURL
			response.DefaultPhoto = &img.DefaultPhoto
		}
	}

	return response
}

// convertProductToResponseSimple converts domain entity to response format without fetching related data
func convertProductToResponseSimple(product *entities.Product, imageRepo repository.ImageRepository) ProductResponse {
	response := ProductResponse{
		ID:            product.ID,
		Name:          product.Name,
		Description:   product.Description,
		Slug:          product.Slug,
		Price:         product.Price,
		StartPrice:    product.StartPrice,
		EndPrice:      product.EndPrice,
		AverageRating: product.AverageRating,
		CategoryID:    product.CategoryID,
		BrandID:       product.BrandID,
		ViewsCount:    product.ViewsCount,
		Status:        product.Status,
		Priority:      product.Priority,
		CreatedAt:     product.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     product.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	// Fetch and set product photo if imageRepo is available
	if imageRepo != nil {
		images, err := imageRepo.GetByImageableID(context.Background(), "Product", product.ID)
		if err == nil && len(images) > 0 {
			// Find default photo or use first image
			var selectedImage *entities.Image
			for _, img := range images {
				if img.DefaultPhoto == 1 {
					selectedImage = img
					break
				}
			}
			if selectedImage == nil {
				selectedImage = images[0]
			}

			// Generate image URL
			photoURL := generateImageURL(selectedImage.ImagePath)
			response.Photo = &photoURL
			// Set default photo flag from DB (0/1)
			response.DefaultPhoto = &selectedImage.DefaultPhoto
		}
	}

	return response
}

// generateImageURL generates an S3 URL for image access
func generateImageURL(imagePath string) string {
	// If the imagePath is already a full URL, return as-is
	if strings.HasPrefix(imagePath, "http://") || strings.HasPrefix(imagePath, "https://") {
		return imagePath
	}

	// Check local filesystem first (development static files)
	fsPath := strings.TrimPrefix(imagePath, "/")
	if _, err := os.Stat(fsPath); err == nil {
		return "/" + filepath.ToSlash(fsPath)
	}

	bucket := os.Getenv("S3_BUCKET")
	region := os.Getenv("AWS_REGION")
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	if bucket == "" {
		bucket = "kossti"
	}
	if region == "" {
		region = "ap-southeast-1"
	}

	// Only attempt presigned URL when credentials are explicitly set
	if accessKey != "" && secretKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cfg, err := config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		)
		if err == nil {
			s3Client := s3.NewFromConfig(cfg)
			presignClient := s3.NewPresignClient(s3Client)

			presigned, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(imagePath),
			}, func(opts *s3.PresignOptions) {
				opts.Expires = 1 * time.Hour
			})
			if err == nil {
				return presigned.URL
			}
		}
	}

	// Fallback: direct S3 URL (works if bucket/object is public-read)
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, region, imagePath)
}

// batchApplyTranslations fetches Bengali (or any non-English locale) translated
// names in a single DB query and sets TranslatedName on each ProductResponse.
func batchApplyTranslations(ctx context.Context, responses []ProductResponse, repo repository.ProductRepository, locale string) {
	if locale == "" || locale == "en" || len(responses) == 0 {
		return
	}
	ids := make([]uint, len(responses))
	for i, r := range responses {
		ids[i] = r.ID
	}
	translationMap, err := repo.GetTranslatedNamesByProductIDs(ctx, ids, locale)
	if err != nil {
		return
	}
	for i := range responses {
		if name, ok := translationMap[responses[i].ID]; ok {
			n := name
			responses[i].TranslatedName = &n
		}
	}
}

// GetProductByIDHandler handles GET /products/{id}
func GetProductByIDHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, categoryRepo repository.CategoryRepository, brandRepo repository.BrandRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// fmt.Println("========== DEBUG: GetProductByIDHandler called ==========")

	// Extract ID from URL path - handle both /products/{id} and /products/{id}/
	path := strings.TrimPrefix(r.URL.Path, "/products/")
	path = strings.TrimSuffix(path, "/")

	// Split by slash and get the first part as ID
	pathParts := strings.Split(path, "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Product ID is required"})
		return
	}

	idStr := pathParts[0]
	productID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": idStr,
		})
		return
	}

	product, err := repo.GetByID(r.Context(), uint(productID))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Product not found",
			"id":    idStr,
		})
		return
	}

	// DEBUG: Log the product entity before conversion
	// fmt.Printf("DEBUG: Product entity - ID: %d, Status: %v, Priority: %d\n", product.ID, product.Status, product.Priority)

	response := convertProductToResponse(product, categoryRepo, brandRepo, imageRepo)

	// Apply locale translation if non-English
	locale := r.URL.Query().Get("locale")
	if locale != "" && locale != "en" {
		responses := []ProductResponse{response}
		batchApplyTranslations(r.Context(), responses, repo, locale)
		response = responses[0]

		// Apply brand translation if brand exists
		if response.Brand != nil && response.Brand.ID > 0 {
			if brandTranslation, err := brandRepo.GetTranslationByLocale(r.Context(), response.Brand.ID, locale); err == nil && brandTranslation != nil {
				response.Brand.TranslatedName = &brandTranslation.TranslatedName
			}
		}
	}

	// DEBUG: Log the response before JSON encoding
	// fmt.Printf("DEBUG: Response struct - ID: %d, Status: %v, Priority: %d\n", response.ID, response.Status, response.Priority)

	json.NewEncoder(w).Encode(response)
}

// GetProductBySlugHandler handles GET /products-by-slug/{slug}
func GetProductBySlugHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, categoryRepo repository.CategoryRepository, brandRepo repository.BrandRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract slug from URL path
	path := strings.TrimPrefix(r.URL.Path, "/products-by-slug/")
	path = strings.TrimSuffix(path, "/")

	if path == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Product slug is required"})
		return
	}

	product, err := repo.GetBySlug(r.Context(), path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Product not found",
			"slug":  path,
		})
		return
	}

	response := convertProductToResponse(product, categoryRepo, brandRepo, imageRepo)

	// Apply locale translation if non-English
	locale := r.URL.Query().Get("locale")
	if locale != "" && locale != "en" {
		responses := []ProductResponse{response}
		batchApplyTranslations(r.Context(), responses, repo, locale)
		response = responses[0]

		// Apply brand translation if brand exists
		if response.Brand != nil && response.Brand.ID > 0 {
			if brandTranslation, err := brandRepo.GetTranslationByLocale(r.Context(), response.Brand.ID, locale); err == nil && brandTranslation != nil {
				response.Brand.TranslatedName = &brandTranslation.TranslatedName
			}
		}
	}

	json.NewEncoder(w).Encode(response)
}

// CreateProductHandler handles POST /products
func CreateProductHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	var req entities.Product
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Product name is required"})
		return
	}

	// Check for duplicate product names by searching for existing products
	existingProducts, err := repo.Search(r.Context(), req.Name, 1, 0)
	if err == nil && len(existingProducts) > 0 {
		// Check for exact name match (case-insensitive)
		for _, existing := range existingProducts {
			if strings.EqualFold(existing.Name, req.Name) {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":   "Product with this name already exists",
					"message": fmt.Sprintf("A product named '%s' already exists", req.Name),
					"field":   "name",
					"code":    "DUPLICATE_PRODUCT_NAME",
				})
				return
			}
		}
	}

	// Generate slug if not provided to check for slug duplicates
	slug := req.Slug
	if slug == "" {
		// Generate slug from name (same logic as in repository)
		slug = strings.ToLower(req.Name)
		reg := regexp.MustCompile(`[^a-z0-9]+`)
		slug = reg.ReplaceAllString(slug, "-")
		slug = strings.Trim(slug, "-")
	}

	// Check for duplicate slug
	existingProduct, err := repo.GetBySlug(r.Context(), slug)
	if err == nil && existingProduct != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":      "Product with this slug already exists",
			"message":    fmt.Sprintf("A product with slug '%s' already exists", slug),
			"field":      "slug",
			"code":       "DUPLICATE_PRODUCT_SLUG",
			"suggestion": fmt.Sprintf("Consider using a different name or slug. Suggested slug: %s-1", slug),
		})
		return
	}

	product, err := repo.Create(r.Context(), &req)
	if err != nil {
		// Check if the error is related to database constraints (e.g., unique constraint violations)
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") ||
			strings.Contains(strings.ToLower(err.Error()), "unique") ||
			strings.Contains(strings.ToLower(err.Error()), "constraint") {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Duplicate product detected",
				"message": "A product with similar details already exists in the database",
				"details": err.Error(),
				"code":    "DATABASE_CONSTRAINT_VIOLATION",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	response := convertProductToResponseSimple(product, imageRepo)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// UpdateProductHandler handles PATCH /products/{id}
func UpdateProductHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/products/")
	path = strings.TrimSuffix(path, "/")

	pathParts := strings.Split(path, "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Product ID is required"})
		return
	}

	idStr := pathParts[0]
	productID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": idStr,
		})
		return
	}

	var req entities.Product
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// DEBUG: Read body for debugging
		r.Body.Close()
		// fmt.Printf("DEBUG: Failed to decode request body: %v\n", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	// DEBUG: Log received product data
	// fmt.Printf("DEBUG: Received product update - ID: %d, Name: %s, Status: %v, Priority: %d\n",
	// 	req.ID, req.Name, req.Status, req.Priority)

	product, err := repo.Update(r.Context(), uint(productID), &req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	response := convertProductToResponseSimple(product, imageRepo)
	json.NewEncoder(w).Encode(response)
}

// ListProductsHandler handles GET /products
func ListProductsHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, categoryRepo repository.CategoryRepository, brandRepo repository.BrandRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	searchQuery := r.URL.Query().Get("search")
	categoryIDStr := r.URL.Query().Get("category_id")
	brandIDStr := r.URL.Query().Get("brand_id")

	// Set defaults
	limit := 20
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	var products []*entities.Product
	var err error

	// Handle different query types
	if searchQuery != "" {
		products, err = repo.Search(r.Context(), searchQuery, limit, offset)
	} else if categoryIDStr != "" {
		categoryID, parseErr := strconv.ParseUint(categoryIDStr, 10, 32)
		if parseErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid category ID"})
			return
		}
		products, err = repo.GetByCategory(r.Context(), uint(categoryID), limit, offset)
	} else if brandIDStr != "" {
		brandID, parseErr := strconv.ParseUint(brandIDStr, 10, 32)
		if parseErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid brand ID"})
			return
		}
		products, err = repo.GetByBrand(r.Context(), uint(brandID), limit, offset)
	} else {
		products, err = repo.List(r.Context(), limit, offset)
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Batch fetch images (OPTIMIZED: 1 query instead of N+1)
	var imagesMap map[uint]*entities.Image = make(map[uint]*entities.Image)
	if imageRepo != nil && len(products) > 0 {
		productIDs := make([]uint, len(products))
		for i, p := range products {
			productIDs[i] = p.ID
		}
		imagesMap, _ = imageRepo.GetDefaultImagesByProductIDs(r.Context(), productIDs)
	}

	// Convert products to response format
	productResponses := make([]ProductResponse, len(products))
	for i, product := range products {
		productResponses[i] = convertProductToResponse(product, categoryRepo, brandRepo, imageRepo, imagesMap)
	}

	// Get total count
	total, _ := repo.Count(r.Context())

	response := ProductListResponse{
		Products: productResponses,
		Count:    len(productResponses),
		Limit:    limit,
		Offset:   offset,
		Total:    total,
	}

	json.NewEncoder(w).Encode(response)
}

// GetFilteredProductsHandler handles Laravel-compatible filtered product listing
// GET /products?locale=en&page=1&limit=10&category=&brand=&priceRange=&searchterm=&sortby=&exclude=1,2,3
func GetFilteredProductsHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, categoryRepo repository.CategoryRepository, brandRepo repository.BrandRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
	locale := r.URL.Query().Get("locale")
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")
	category := r.URL.Query().Get("category")
	brandParam := r.URL.Query().Get("brand")
	priceRange := r.URL.Query().Get("priceRange")
	searchterm := r.URL.Query().Get("search")
	sortby := r.URL.Query().Get("sortby")
	excludeParam := r.URL.Query().Get("exclude")

	// Set defaults
	page := 1
	limit := 10

	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Debug: Log what limit is being used
	fmt.Printf("[GetFilteredProductsHandler] page=%d, limit=%d, category=%s\n", page, limit, category)

	// Convert brand parameter to array (Laravel sends comma-separated)
	var brands []string
	if brandParam != "" {
		brands = strings.Split(brandParam, ",")
		// Trim whitespace from each brand
		for i, brand := range brands {
			brands[i] = strings.TrimSpace(brand)
		}
	}

	// Parse exclude parameter (comma-separated product IDs)
	var excludeIDs []uint
	if excludeParam != "" {
		excludeStrings := strings.Split(excludeParam, ",")
		for _, idStr := range excludeStrings {
			idStr = strings.TrimSpace(idStr)
			if id, err := strconv.ParseUint(idStr, 10, 32); err == nil {
				excludeIDs = append(excludeIDs, uint(id))
			}
		}
	}

	// Create filters struct
	filters := &repository.ProductFilters{
		Page:              page,
		Limit:             limit,
		Locale:            locale,
		SearchTerm:        searchterm,
		Category:          category,
		CategorySlug:      category,   // Use category value as slug for filtering
		Brand:             brandParam, // Store original comma-separated string
		BrandSlugs:        brands,     // Store as array for filtering
		PriceRange:        priceRange,
		SortBy:            sortby,
		ExcludeProductIDs: excludeIDs,
	}
	// Get filtered products
	products, totalCount, err := repo.GetWithFilters(r.Context(), filters)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Batch fetch images for all products (OPTIMIZED: 1 query instead of N+1)
	var imagesMap map[uint]*entities.Image = make(map[uint]*entities.Image)
	if imageRepo != nil && len(products) > 0 {
		productIDs := make([]uint, len(products))
		for i, p := range products {
			productIDs[i] = p.ID
		}
		imagesMap, _ = imageRepo.GetDefaultImagesByProductIDs(r.Context(), productIDs)
	}

	// Convert products to response format
	productResponses := make([]ProductResponse, len(products))
	for i, product := range products {
		productResponses[i] = convertProductToResponse(product, categoryRepo, brandRepo, imageRepo, imagesMap)
	}

	// Batch-apply Bengali (or any non-English) translations in one query
	batchApplyTranslations(r.Context(), productResponses, repo, locale)

	// Apply brand translations in BATCH if non-English locale (OPTIMIZED: 1 query instead of N+1)
	if locale != "" && locale != "en" {
		// Collect all unique brand IDs
		brandIDSet := make(map[uint]bool)
		for _, p := range productResponses {
			if p.Brand != nil && p.Brand.ID > 0 {
				brandIDSet[p.Brand.ID] = true
			}
		}

		if len(brandIDSet) > 0 {
			// Convert to slice for batch query
			brandIDs := make([]uint, 0, len(brandIDSet))
			for id := range brandIDSet {
				brandIDs = append(brandIDs, id)
			}

			// Fetch all translations in ONE query
			brandTranslationsMap, _ := brandRepo.GetTranslationsByLocaleAndBrandIDs(r.Context(), brandIDs, locale)

			// Apply translations from map
			for i := range productResponses {
				if productResponses[i].Brand != nil && productResponses[i].Brand.ID > 0 {
					if trans, exists := brandTranslationsMap[productResponses[i].Brand.ID]; exists {
						productResponses[i].Brand.TranslatedName = &trans.TranslatedName
					}
				}
			}
		}
	}

	// Calculate pagination info
	totalPages := (totalCount + int64(limit) - 1) / int64(limit)
	hasNextPage := page < int(totalPages)
	hasPrevPage := page > 1

	// Debug logging
	fmt.Printf("[GetFilteredProductsHandler] Response - totalCount: %d, limit: %d, page: %d, totalPages: %d, productsReturned: %d\n",
		totalCount, limit, page, totalPages, len(productResponses))

	// Laravel-compatible response format
	response := map[string]interface{}{
		"data": productResponses,
		"meta": map[string]interface{}{
			"current_page":  page,
			"per_page":      limit,
			"total":         totalCount,
			"last_page":     totalPages,
			"from":          (page-1)*limit + 1,
			"to":            (page-1)*limit + len(productResponses),
			"has_next_page": hasNextPage,
			"has_prev_page": hasPrevPage,
		},
		"filters": map[string]interface{}{
			"locale":      locale,
			"category":    category,
			"brand":       brandParam,
			"price_range": priceRange,
			"search_term": searchterm,
			"sort_by":     sortby,
		},
	}

	json.NewEncoder(w).Encode(response)
}

// GetPopularProductsHandler handles GET /popular-products
func GetPopularProductsHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, categoryRepo repository.CategoryRepository, brandRepo repository.BrandRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	limitStr := r.URL.Query().Get("limit")
	limit := 10

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// If a category query param is provided, return popular products within that category
	category := r.URL.Query().Get("category")
	if category != "" {
		filters := &repository.ProductFilters{
			Page:         1,
			Limit:        limit,
			Locale:       r.URL.Query().Get("locale"),
			SearchTerm:   "",
			Category:     category,
			CategorySlug: category,
			SortBy:       "popular",
		}

		products, total, err := repo.GetWithFilters(r.Context(), filters)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Batch fetch images (OPTIMIZED: 1 query instead of N+1)
		var imagesMap map[uint]*entities.Image = make(map[uint]*entities.Image)
		if imageRepo != nil && len(products) > 0 {
			productIDs := make([]uint, len(products))
			for i, p := range products {
				productIDs[i] = p.ID
			}
			imagesMap, _ = imageRepo.GetDefaultImagesByProductIDs(r.Context(), productIDs)
		}

		productResponses := make([]ProductResponse, len(products))
		for i, product := range products {
			productResponses[i] = convertProductToResponse(product, categoryRepo, brandRepo, imageRepo, imagesMap)
		}

		// Batch-apply translations for non-English locales
		locale := r.URL.Query().Get("locale")
		batchApplyTranslations(r.Context(), productResponses, repo, locale)

		// Apply brand translations in BATCH if non-English locale (OPTIMIZED: 1 query instead of N+1)
		if locale != "" && locale != "en" {
			// Collect all unique brand IDs
			brandIDSet := make(map[uint]bool)
			for _, p := range productResponses {
				if p.Brand != nil && p.Brand.ID > 0 {
					brandIDSet[p.Brand.ID] = true
				}
			}

			if len(brandIDSet) > 0 {
				// Convert to slice for batch query
				brandIDs := make([]uint, 0, len(brandIDSet))
				for id := range brandIDSet {
					brandIDs = append(brandIDs, id)
				}

				// Fetch all translations in ONE query
				brandTranslationsMap, _ := brandRepo.GetTranslationsByLocaleAndBrandIDs(r.Context(), brandIDs, locale)

				// Apply translations from map
				for i := range productResponses {
					if productResponses[i].Brand != nil && productResponses[i].Brand.ID > 0 {
						if trans, exists := brandTranslationsMap[productResponses[i].Brand.ID]; exists {
							productResponses[i].Brand.TranslatedName = &trans.TranslatedName
						}
					}
				}
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"products":      productResponses,
			"totalProducts": total,
		})
		return
	}

	// No category filter: return globally popular products
	products, err := repo.GetPopular(r.Context(), limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Batch fetch images (OPTIMIZED: 1 query instead of N+1)
	var imagesMap map[uint]*entities.Image = make(map[uint]*entities.Image)
	if imageRepo != nil && len(products) > 0 {
		productIDs := make([]uint, len(products))
		for i, p := range products {
			productIDs[i] = p.ID
		}
		imagesMap, _ = imageRepo.GetDefaultImagesByProductIDs(r.Context(), productIDs)
	}

	productResponses := make([]ProductResponse, len(products))
	for i, product := range products {
		productResponses[i] = convertProductToResponse(product, categoryRepo, brandRepo, imageRepo, imagesMap)
	}

	// Batch-apply translations for non-English locales
	locale := r.URL.Query().Get("locale")
	batchApplyTranslations(r.Context(), productResponses, repo, locale)

	// Apply brand translations in BATCH if non-English locale (OPTIMIZED: 1 query instead of N+1)
	if locale != "" && locale != "en" {
		// Collect all unique brand IDs
		brandIDSet := make(map[uint]bool)
		for _, p := range productResponses {
			if p.Brand != nil && p.Brand.ID > 0 {
				brandIDSet[p.Brand.ID] = true
			}
		}

		if len(brandIDSet) > 0 {
			// Convert to slice for batch query
			brandIDs := make([]uint, 0, len(brandIDSet))
			for id := range brandIDSet {
				brandIDs = append(brandIDs, id)
			}

			// Fetch all translations in ONE query
			brandTranslationsMap, _ := brandRepo.GetTranslationsByLocaleAndBrandIDs(r.Context(), brandIDs, locale)

			// Apply translations from map
			for i := range productResponses {
				if productResponses[i].Brand != nil && productResponses[i].Brand.ID > 0 {
					if trans, exists := brandTranslationsMap[productResponses[i].Brand.ID]; exists {
						productResponses[i].Brand.TranslatedName = &trans.TranslatedName
					}
				}
			}
		}
	}

	response := ProductListResponse{
		Products: productResponses,
		Count:    len(productResponses),
		Limit:    limit,
		Offset:   0,
	}

	json.NewEncoder(w).Encode(response)
}

// GetSimilarProductsHandler handles GET /products-by-slug/{slug}/similar
func GetSimilarProductsHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, categoryRepo repository.CategoryRepository, brandRepo repository.BrandRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract slug from URL path
	path := strings.TrimPrefix(r.URL.Path, "/products-by-slug/")
	path = strings.TrimSuffix(path, "/similar")
	path = strings.TrimSuffix(path, "/")

	if path == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Product slug is required"})
		return
	}

	// 1. Get the main product first
	product, err := repo.GetBySlug(r.Context(), path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Product not found",
			"slug":  path,
		})
		return
	}

	// 2. Get similar products
	// Default limit 4, but can be overridden
	limit := 4
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	similarProducts, err := repo.GetSimilarProducts(r.Context(), product, limit)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Batch fetch images (OPTIMIZED: 1 query instead of N+1)
	var imagesMap map[uint]*entities.Image = make(map[uint]*entities.Image)
	if imageRepo != nil && len(similarProducts) > 0 {
		productIDs := make([]uint, len(similarProducts))
		for i, p := range similarProducts {
			productIDs[i] = p.ID
		}
		imagesMap, _ = imageRepo.GetDefaultImagesByProductIDs(r.Context(), productIDs)
	}

	// 3. Convert to response
	productResponses := make([]ProductResponse, len(similarProducts))
	for i, p := range similarProducts {
		productResponses[i] = convertProductToResponse(p, categoryRepo, brandRepo, imageRepo, imagesMap)
	}

	// Batch-apply translations for non-English locales
	locale := r.URL.Query().Get("locale")
	batchApplyTranslations(r.Context(), productResponses, repo, locale)

	// Apply brand translations in BATCH if non-English locale (OPTIMIZED: 1 query instead of N+1)
	if locale != "" && locale != "en" {
		// Collect all unique brand IDs
		brandIDSet := make(map[uint]bool)
		for _, p := range productResponses {
			if p.Brand != nil && p.Brand.ID > 0 {
				brandIDSet[p.Brand.ID] = true
			}
		}

		if len(brandIDSet) > 0 {
			// Convert to slice for batch query
			brandIDs := make([]uint, 0, len(brandIDSet))
			for id := range brandIDSet {
				brandIDs = append(brandIDs, id)
			}

			// Fetch all translations in ONE query
			brandTranslationsMap, _ := brandRepo.GetTranslationsByLocaleAndBrandIDs(r.Context(), brandIDs, locale)

			// Apply translations from map
			for i := range productResponses {
				if productResponses[i].Brand != nil && productResponses[i].Brand.ID > 0 {
					if trans, exists := brandTranslationsMap[productResponses[i].Brand.ID]; exists {
						productResponses[i].Brand.TranslatedName = &trans.TranslatedName
					}
				}
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"products": productResponses,
		"count":    len(productResponses),
	})
}

// IncrementProductViewsHandler handles POST /products/{id}/increment-views
func IncrementProductViewsHandler(w http.ResponseWriter, r *http.Request, repo repository.ProductRepository, categoryRepo repository.CategoryRepository, brandRepo repository.BrandRepository, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from URL path - expecting /products/{id}/increment-views
	path := strings.TrimPrefix(r.URL.Path, "/products/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) < 2 || pathParts[0] == "" || pathParts[1] != "increment-views" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format"})
		return
	}

	idStr := pathParts[0]
	productID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": idStr,
		})
		return
	}

	err = repo.IncrementViews(r.Context(), uint(productID))
	if err != nil {
		if err.Error() == "product not found" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Product not found"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		return
	}

	// Return updated product
	product, err := repo.GetByID(r.Context(), uint(productID))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get updated product"})
		return
	}

	response := convertProductToResponse(product, categoryRepo, brandRepo, imageRepo)
	json.NewEncoder(w).Encode(response)
}

// AddProductImageHandler handles POST /addproductimage/{productId}
func AddProductImageHandler(w http.ResponseWriter, r *http.Request, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/addproductimage/")
	productIDStr := strings.Trim(path, "/")

	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": productIDStr,
		})
		return
	}

	// Parse multipart form
	err = r.ParseMultipartForm(10 << 20) // 10 MB max memory
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to parse form"})
		return
	}

	file, handler, err := r.FormFile("image")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No image file provided"})
		return
	}
	defer file.Close()

	// Create uploads directory if it doesn't exist
	uploadDir := "uploads/products"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create upload directory"})
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%d_%d_%s", productID, time.Now().Unix(), handler.Filename)
	filePath := filepath.Join(uploadDir, filename)

	// Create the file
	dst, err := os.Create(filePath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create file"})
		return
	}
	defer dst.Close()

	// Copy file content
	_, err = io.Copy(dst, file)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to save file"})
		return
	}

	// Save image record to database
	image := &entities.Image{
		ImageableType: "product",
		ImageableID:   uint(productID),
		ImagePath:     filePath,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	savedImage, err := imageRepo.Create(r.Context(), image)
	if err != nil {
		// Clean up file if database save fails
		os.Remove(filePath)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to save image record"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "Image uploaded successfully",
		"image_id":   savedImage.ID,
		"image_path": savedImage.ImagePath,
	})
}

// GetProductImageHandler handles GET /get-product-image/{productId}
func GetProductImageHandler(w http.ResponseWriter, r *http.Request, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/get-product-image/")
	productIDStr := strings.Trim(path, "/")

	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": productIDStr,
		})
		return
	}

	images, err := imageRepo.GetByImageableID(r.Context(), "Product", uint(productID))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get images"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"product_id": productID,
		"images":     images,
	})
}

// AddProductImageAltHandler handles POST /products/{product}/image
func AddProductImageAltHandler(w http.ResponseWriter, r *http.Request, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/products/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) < 2 || pathParts[1] != "image" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format"})
		return
	}

	productIDStr := pathParts[0]
	_, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": productIDStr,
		})
		return
	}

	// Reconstruct the URL for AddProductImageHandler
	r.URL.Path = "/addproductimage/" + productIDStr
	AddProductImageHandler(w, r, imageRepo)
}

// GetProductImagesHandler handles GET /products/{product}/images
func GetProductImagesHandler(w http.ResponseWriter, r *http.Request, imageRepo repository.ImageRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/products/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) < 2 || pathParts[1] != "images" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format"})
		return
	}

	productIDStr := pathParts[0]
	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": productIDStr,
		})
		return
	}

	images, err := imageRepo.GetByImageableID(r.Context(), "product", uint(productID))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get images"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"product_id": productID,
		"images":     images,
	})
}

// CreateProductTranslationHandler handles POST /products/{id}/translation and POST /product-trans/{id}
// Creates a new translation if one doesn't exist for the product and locale,
// or updates an existing translation if one is found.
// Returns 201 (Created) for new translations, 200 (OK) for updates.
func CreateProductTranslationHandler(w http.ResponseWriter, r *http.Request, productRepo repository.ProductRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Debug log
	// fmt.Printf("CreateProductTranslationHandler called with URL: %s\n", r.URL.Path)

	// Extract product ID from URL path - handle both /products/{id}/translation and /product-trans/{id}
	var productIDStr string

	if strings.HasPrefix(r.URL.Path, "/products/") {
		// Handle /products/{id}/translation
		path := strings.TrimPrefix(r.URL.Path, "/products/")
		pathParts := strings.Split(path, "/")

		if len(pathParts) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format for /products/{id}/translation"})
			return
		}
		productIDStr = pathParts[0]
	} else if strings.HasPrefix(r.URL.Path, "/product-trans/") {
		// Handle /product-trans/{id}
		path := strings.TrimPrefix(r.URL.Path, "/product-trans/")
		path = strings.TrimSuffix(path, "/")

		if path == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format for /product-trans/{id}"})
			return
		}
		productIDStr = path
	} else {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format"})
		return
	}

	// fmt.Printf("Extracted product ID string: %s\n", productIDStr)

	productID, err := strconv.ParseUint(productIDStr, 10, 32)

	// fmt.Printf("============================== Product ID: %d\n", productID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": productIDStr,
		})
		return
	}

	var request struct {
		Locale         string  `json:"locale"`
		TranslatedName string  `json:"translated_name"`
		StartPrice     *string `json:"start_price"`
		EndPrice       *string `json:"end_price"`
	}

	// Read and log the raw body for debugging
	bodyBytes, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		fmt.Printf("Error reading body: %v\n", readErr)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read request body"})
		return
	}
	fmt.Printf("Raw request body: %s\n", string(bodyBytes))

	// Decode the body
	if err := json.Unmarshal(bodyBytes, &request); err != nil {
		fmt.Printf("JSON decode error: %v\n", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload", "details": err.Error()})
		return
	}

	// Debug: Log the decoded request
	fmt.Printf("Decoded request: Locale='%s', TranslatedName='%s'\n", request.Locale, request.TranslatedName)
	fmt.Printf("Raw TranslatedName bytes: %v\n", []byte(request.TranslatedName))

	// Parse string prices to *float64
	var startPrice *string
	var endPrice *string

	if request.StartPrice != nil && *request.StartPrice != "" {
		startPrice = request.StartPrice
	}

	if request.EndPrice != nil && *request.EndPrice != "" {
		endPrice = request.EndPrice
	}

	if request.Locale == "" || request.TranslatedName == "" {
		// fmt.Printf("Validation failed: Locale='%s' (len=%d), TranslatedName='%s' (len=%d)\n",
		// 	request.Locale, len(request.Locale), request.TranslatedName, len(request.TranslatedName))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Locale and translated_name are required"})
		return
	}

	// Additional validation for unicode characters
	if len(strings.TrimSpace(request.TranslatedName)) == 0 {
		fmt.Printf("TranslatedName contains only whitespace\n")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "translated_name cannot be empty or contain only whitespace"})
		return
	}

	// First, fetch the product so we can use its prices as defaults
	product, productErr := productRepo.GetByID(r.Context(), uint(productID))
	if productErr != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Product not found",
			"id":    productIDStr,
		})
		return
	}

	// Check if translation already exists for this product and locale
	existingTranslation, err := productRepo.GetTranslationByLocale(r.Context(), uint(productID), request.Locale)

	// Log the result of checking existing translation
	if err != nil {
		fmt.Printf("Error checking existing translation: %v\n", err)
	} else if existingTranslation != nil {
		fmt.Printf("Found existing translation: ID=%d, Locale=%s\n", existingTranslation.ID, existingTranslation.Locale)
	} else {
		fmt.Printf("No existing translation found for product %d, locale %s\n", productID, request.Locale)
	}

	var savedTranslation *entities.ProductTranslation
	var isUpdate bool

	if err == nil && existingTranslation != nil {
		// Translation exists - update it
		isUpdate = true
		fmt.Printf("Updating existing translation for product %d, locale %s\n", productID, request.Locale)
		existingTranslation.TranslatedName = request.TranslatedName
		// Only overwrite start/end if provided in request (allow omission to keep existing)
		if startPrice != nil {
			existingTranslation.StartPrice = startPrice
		}
		if endPrice != nil {
			existingTranslation.EndPrice = endPrice
		}
		existingTranslation.UpdatedAt = time.Now()

		savedTranslation, err = productRepo.UpdateTranslation(r.Context(), existingTranslation)
		if err != nil {
			fmt.Printf("Database error updating translation: %v\n", err)
			fmt.Printf("Translation data: ID=%d, ProductID=%d, Locale=%s, Name=%s\n",
				existingTranslation.ID, existingTranslation.ProductID, existingTranslation.Locale, existingTranslation.TranslatedName)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "Failed to update translation",
				"details": err.Error(),
			})
			return
		}

		fmt.Printf("Translation updated successfully - StartPrice: %v, EndPrice: %v\n",
			savedTranslation.StartPrice, savedTranslation.EndPrice)
	} else {
		// Translation doesn't exist - create new one
		isUpdate = false
		fmt.Printf("Creating new translation for product %d, locale %s\n", productID, request.Locale)
		// Ensure we're not passing empty values
		translatedName := strings.TrimSpace(request.TranslatedName)
		if translatedName == "" {
			fmt.Printf("ERROR: translatedName is empty after trimming\n")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "translated_name cannot be empty"})
			return
		}

		// Determine start/end price to save: prefer request, then product defaults
		// IMPORTANT: Declare price strings outside if blocks to keep them in scope
		var spStr, epStr string
		var sp *string
		var ep *string

		if startPrice != nil {
			sp = startPrice
		} else if product != nil && product.StartPrice != nil {
			// Convert product start price float to string
			spStr = fmt.Sprintf("%.2f", *product.StartPrice)
			sp = &spStr
		} else if product != nil && product.Price != 0 {
			// Convert product price float to string as fallback
			spStr = fmt.Sprintf("%.2f", product.Price)
			sp = &spStr
		}

		if endPrice != nil {
			ep = endPrice
		} else if product != nil && product.EndPrice != nil {
			// Convert product end price float to string
			epStr = fmt.Sprintf("%.2f", *product.EndPrice)
			ep = &epStr
		}

		fmt.Printf("Setting translation prices - StartPrice: %v, EndPrice: %v (from product: Start=%v, End=%v, Price=%v)\n",
			sp, ep, product.StartPrice, product.EndPrice, product.Price)

		translation := &entities.ProductTranslation{
			ProductID:      uint(productID),
			Locale:         request.Locale,
			TranslatedName: translatedName,
			StartPrice:     sp,
			EndPrice:       ep,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		// Debug: Log the translation object before database call
		fmt.Printf("Translation object before DB call: ProductID=%d, Locale='%s', TranslatedName='%s' (len=%d)\n",
			translation.ProductID, translation.Locale, translation.TranslatedName, len(translation.TranslatedName))

		// Validate that all required fields are actually set
		if translation.TranslatedName == "" {
			fmt.Printf("ERROR: TranslatedName is empty in translation object!\n")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Internal error: TranslatedName is empty"})
			return
		}

		savedTranslation, err = productRepo.CreateTranslation(r.Context(), translation)
		if err != nil {
			fmt.Printf("Database error creating translation: %v\n", err)
			fmt.Printf("Translation data: ProductID=%d, Locale=%s, Name=%s\n",
				translation.ProductID, translation.Locale, translation.TranslatedName)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "Failed to create translation",
				"details": err.Error(),
			})
			return
		}

		fmt.Printf("Translation created successfully - StartPrice: %v, EndPrice: %v\n",
			savedTranslation.StartPrice, savedTranslation.EndPrice)
	}

	// Set appropriate status code
	if isUpdate {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}

	// Return success response
	message := "Translation updated successfully"
	if !isUpdate {
		message = "Translation created successfully"
	}

	// Explicitly structure translation response with start_price and end_price always included
	translationResp := map[string]interface{}{
		"id":              savedTranslation.ID,
		"product_id":      savedTranslation.ProductID,
		"locale":          savedTranslation.Locale,
		"translated_name": savedTranslation.TranslatedName,
		"start_price":     savedTranslation.StartPrice,
		"end_price":       savedTranslation.EndPrice,
		"created_at":      savedTranslation.CreatedAt,
		"updated_at":      savedTranslation.UpdatedAt,
	}

	fmt.Printf("Final response - start_price: %v, end_price: %v\n",
		translationResp["start_price"], translationResp["end_price"])

	response := map[string]interface{}{
		"message":     message,
		"translation": translationResp,
		"action":      map[string]bool{"created": !isUpdate, "updated": isUpdate},
	}
	json.NewEncoder(w).Encode(response)
}

// GetPublicReviewsHandler handles GET /public-reviews/{id}
// This endpoint matches the Laravel API: Route::get('/public-reviews/{id}', [ProductReviewController::class, 'getPublicReviews']);
func GetPublicReviewsHandler(w http.ResponseWriter, r *http.Request, productRepo repository.ProductRepository) {
	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/public-reviews/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) == 0 || pathParts[0] == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Product ID is required"}`))
		return
	}

	productIDStr := pathParts[0]
	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid product ID"}`))
		return
	}

	// Get locale from query parameter (default to 'en' like Laravel)
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		locale = "en"
	}

	// For now, return a simple response structure matching Laravel
	// TODO: Implement actual review fetching logic when review repository is available
	reviews := []map[string]interface{}{
		{
			"id":                 1,
			"product_id":         uint(productID),
			"rating":             5,
			"reviews":            "Excellent product! Highly recommended.",
			"additional_details": "Great quality and fast delivery",
			"price":              999.99,
			"locale":             locale,
			"created_at":         time.Now().Format("2006-01-02T15:04:05Z07:00"),
			"updated_at":         time.Now().Format("2006-01-02T15:04:05Z07:00"),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"data":    reviews,
		"success": true,
		"message": "Public reviews retrieved successfully",
	}

	json.NewEncoder(w).Encode(response)
}

// GetProductTranslationsHandler handles GET /products/{id}/translations
// Retrieves all translations for a specific product, with optional locale filtering
func GetProductTranslationsHandler(w http.ResponseWriter, r *http.Request, productRepo repository.ProductRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/products/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) < 2 || pathParts[0] == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format for /products/{id}/translations"})
		return
	}

	productIDStr := pathParts[0]
	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": productIDStr,
		})
		return
	}

	// First, verify that the product exists
	_, productErr := productRepo.GetByID(r.Context(), uint(productID))
	if productErr != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Product not found",
			"id":    productIDStr,
		})
		return
	}

	// Check if locale filter is provided
	locale := r.URL.Query().Get("locale")

	if locale != "" {
		// Get specific translation by locale
		translation, err := productRepo.GetTranslationByLocale(r.Context(), uint(productID), locale)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error":  "Translation not found for the specified locale",
				"locale": locale,
			})
			return
		}

		// Only return start_price and end_price (and core fields)
		resp := map[string]interface{}{
			"id":              translation.ID,
			"product_id":      translation.ProductID,
			"locale":          translation.Locale,
			"translated_name": translation.TranslatedName,
			"start_price":     translation.StartPrice,
			"end_price":       translation.EndPrice,
			"created_at":      translation.CreatedAt,
			"updated_at":      translation.UpdatedAt,
		}
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"data":    resp,
			"success": true,
			"message": fmt.Sprintf("Translation retrieved successfully for locale %s", locale),
		}
		json.NewEncoder(w).Encode(response)
	} else {
		// Get all translations for the product
		translations, err := productRepo.GetTranslations(r.Context(), uint(productID))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "Failed to retrieve translations",
				"details": err.Error(),
			})
			return
		}

		// Map to only include start_price/end_price and core fields
		respList := make([]map[string]interface{}, len(translations))
		for i, t := range translations {
			respList[i] = map[string]interface{}{
				"id":              t.ID,
				"product_id":      t.ProductID,
				"locale":          t.Locale,
				"translated_name": t.TranslatedName,
				"start_price":     t.StartPrice,
				"end_price":       t.EndPrice,
				"created_at":      t.CreatedAt,
				"updated_at":      t.UpdatedAt,
			}
		}
		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"data":    respList,
			"success": true,
			"message": "Translations retrieved successfully",
		}
		json.NewEncoder(w).Encode(response)
	}
}

// DeleteProductTranslationHandler handles DELETE /products/{id}/translations?locale=xx
// Deletes a specific translation for a product by locale
func DeleteProductTranslationHandler(w http.ResponseWriter, r *http.Request, productRepo repository.ProductRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/products/")
	pathParts := strings.Split(path, "/")

	if len(pathParts) < 2 || pathParts[0] == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL format for /products/{id}/translations"})
		return
	}

	productIDStr := pathParts[0]
	productID, err := strconv.ParseUint(productIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid product ID format",
			"received": productIDStr,
		})
		return
	}

	// Get locale from query parameter
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "locale query parameter is required"})
		return
	}

	// First, verify that the product exists
	_, productErr := productRepo.GetByID(r.Context(), uint(productID))
	if productErr != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Product not found",
			"id":    productIDStr,
		})
		return
	}

	// Check if translation exists
	translation, err := productRepo.GetTranslationByLocale(r.Context(), uint(productID), locale)
	if err != nil || translation == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "Translation not found for the specified locale",
			"locale": locale,
		})
		return
	}

	// Delete the translation
	err = productRepo.DeleteTranslation(r.Context(), translation.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Failed to delete translation",
			"details": err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Translation deleted successfully for locale %s", locale),
	}
	json.NewEncoder(w).Encode(response)
}

// MarketProduct represents a product from the market
type MarketProduct struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	Price       float64 `json:"price,omitempty"`
	CategoryID  uint    `json:"category_id,omitempty"`
}

// GetMarketProductsHandler handles GET /market-products
func GetMarketProductsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get query parameters
	brandID := r.URL.Query().Get("brand_id")
	brandName := r.URL.Query().Get("brand_name")

	log.Printf("Market products request - Brand ID: %s, Brand Name: %s", brandID, brandName)

	// Get OpenAI API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	// Remove quotes if present (from .env file)
	apiKey = strings.Trim(apiKey, `"`)
	log.Printf("OPENAI_API_KEY length after trimming: %d", len(apiKey))
	if len(apiKey) > 10 {
		log.Printf("OPENAI_API_KEY starts with: %s", apiKey[:10]+"...")
	}
	if apiKey == "" || apiKey == "your_openai_api_key_here" {
		log.Println("OPENAI_API_KEY not set or is placeholder, returning fallback products")
		response := map[string]interface{}{
			"success": true,
			"data":    getFallbackProducts(),
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// Call OpenAI to research new products
	products, err := researchNewProducts(apiKey, brandName)
	if err != nil {
		log.Printf("Error researching products: %v", err)
		// Return fallback products instead of error
		response := map[string]interface{}{
			"success": true,
			"data":    getFallbackProducts(),
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"data":    products,
	}
	json.NewEncoder(w).Encode(response)
}

// researchNewProducts uses OpenAI to research and generate new product ideas
func researchNewProducts(apiKey string, brandName string) ([]MarketProduct, error) {
	var prompt string
	if brandName != "" {
		prompt = fmt.Sprintf(`Research and suggest 5 new innovative consumer electronics products that would fit well with the %s brand.
For each product, provide:
- Name: A catchy product name that could be from %s
- Description: A brief description (2-3 sentences) of what makes this product innovative
- Type: The product category (e.g., Smartphone, Laptop, Smart Home Device, Wearable Technology, Accessories, etc.)

Make the products relevant to %s's typical product line and brand positioning.
Format the response as a JSON array of objects with keys: name, description, type.
Focus on trending technologies like AI, IoT, sustainability, and emerging markets.`, brandName, brandName, brandName)
		log.Printf("Generated brand-specific prompt for %s: %s", brandName, prompt)
	} else {
		prompt = `Research and suggest 5 new innovative consumer electronics products that could be available in the market. 
	For each product, provide:
	- Name: A catchy product name
	- Description: A brief description (2-3 sentences)
	- Type: The product category (e.g., Smartphone, Laptop, Smart Home Device, etc.)
	
	Format the response as a JSON array of objects with keys: name, description, type.
	Focus on trending technologies like AI, IoT, sustainability, and emerging markets.`
		log.Println("Generated generic prompt (no brand specified)")
	}

	requestBody := map[string]interface{}{
		"model": "gpt-3.5-turbo",
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens":  1000,
		"temperature": 0.7,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call OpenAI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OpenAI API error: %s", string(body))
	}

	var openaiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&openaiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	content := openaiResp.Choices[0].Message.Content

	// Strip markdown code block formatting if present
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
	}
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	// Parse the JSON response from OpenAI
	var products []MarketProduct
	if err := json.Unmarshal([]byte(content), &products); err != nil {
		log.Printf("Failed to parse OpenAI response as JSON: %v", err)
		log.Printf("OpenAI response (after stripping): %s", content)
		// Return sample products as fallback
		return getFallbackProducts(), nil
	}

	// Add default price and category
	for i := range products {
		products[i].Price = 99.99 + float64(i*50) // Sample prices
		products[i].CategoryID = 1                // Default category
	}

	return products, nil
}

// getFallbackProducts returns sample products when OpenAI fails
func getFallbackProducts() []MarketProduct {
	return []MarketProduct{
		{
			Name:        "AI-Powered Smart Glasses",
			Description: "Revolutionary smart glasses with built-in AI assistant for real-time translation and augmented reality navigation.",
			Type:        "Wearable Technology",
			Price:       299.99,
			CategoryID:  1,
		},
		{
			Name:        "Eco-Friendly Wireless Charger",
			Description: "Sustainable wireless charging pad made from recycled materials with solar panel integration.",
			Type:        "Accessories",
			Price:       49.99,
			CategoryID:  1,
		},
		{
			Name:        "Smart Home Energy Monitor",
			Description: "IoT device that tracks and optimizes energy usage in your home with AI-powered recommendations.",
			Type:        "Smart Home",
			Price:       79.99,
			CategoryID:  1,
		},
	}
}
