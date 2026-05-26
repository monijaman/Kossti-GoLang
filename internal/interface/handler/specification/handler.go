// Package specification contains HTTP handlers for product specification management.
// It handles CRUD operations for specifications, specification keys, translations,
// and provides both admin and public endpoints for specification data retrieval.
//
// Key Features:
// - Specification creation and management (with or without pre-defined keys)
// - Bulk upsert operations for efficient batch updates
// - Specification translations in multiple locales
// - Optimized queries to prevent N+1 query problems
// - CORS-enabled endpoints for cross-origin requests
package specification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"kossti/internal/domain/entities"
	"kossti/internal/domain/repository"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SpecificationResponse represents the HTTP response format for a single specification.
// Includes product association, key reference, value, and timestamps.
type SpecificationResponse struct {
	ID                 uint   `json:"id"`                   // Unique identifier
	ProductID          uint   `json:"product_id"`           // Associated product
	SpecificationKeyID uint   `json:"specification_key_id"` // Reference to specification key
	SpecificationKey   string `json:"specification_key"`    // Key name (e.g., "Color", "Size")
	Value              string `json:"value"`                // Specification value (e.g., "Red", "Large")
	CreatedAt          string `json:"created_at"`           // Creation timestamp (RFC3339 format)
	UpdatedAt          string `json:"updated_at"`           // Last update timestamp (RFC3339 format)
}

// SpecificationKeyResponse represents the HTTP response format for a specification key.
// Keys are the categories of specifications (e.g., Color, Size, Brand).
type SpecificationKeyResponse struct {
	ID               uint   `json:"id"`                // Unique identifier
	SpecificationKey string `json:"specification_key"` // Key name
	CreatedAt        string `json:"created_at"`        // Creation timestamp
	UpdatedAt        string `json:"updated_at"`        // Last update timestamp
}

// convertSpecificationToResponse converts a specification entity to HTTP response format.
// Formats timestamps to RFC3339 standard for JSON serialization.
func convertSpecificationToResponse(spec *entities.Specification) SpecificationResponse {
	return SpecificationResponse{
		ID:                 spec.ID,
		ProductID:          spec.ProductID,
		SpecificationKeyID: spec.SpecificationKeyID,
		SpecificationKey:   spec.SpecificationKey,
		Value:              spec.Value,
		CreatedAt:          spec.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          spec.UpdatedAt.Format(time.RFC3339),
	}
}

// convertSpecificationKeyToResponse converts a specification key entity to HTTP response format.
func convertSpecificationKeyToResponse(key *entities.SpecificationKey) SpecificationKeyResponse {
	return SpecificationKeyResponse{
		ID:               key.ID,
		SpecificationKey: key.SpecificationKey,
		CreatedAt:        key.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        key.UpdatedAt.Format(time.RFC3339),
	}
}

// CreateSpecificationHandler handles POST /specifications
// Creates a new specification for a product. Supports two modes:
// 1. Using an existing specification key ID
// 2. Creating a new specification key inline
//
// Request JSON:
//
//	{
//	  "product_id": 123,
//	  "specification_key_id": 45,  // Either this or specification_key
//	  "value": "Red",
//	  "specification_key": {        // Or this
//	    "name": "Color",
//	    "is_visible": true,
//	    "is_filterable": true
//	  }
//	}
func CreateSpecificationHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository, keyRepo repository.SpecificationKeyRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Define request payload structure
	var request struct {
		ProductID          uint   `json:"product_id"`
		SpecificationKeyID *uint  `json:"specification_key_id,omitempty"`
		Value              string `json:"value"`
		SpecificationKey   *struct {
			Name         string `json:"name"`
			IsVisible    bool   `json:"is_visible"`
			IsFilterable bool   `json:"is_filterable"`
		} `json:"specification_key,omitempty"`
	}

	// Parse request body
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
		return
	}

	// Validate required product ID
	if request.ProductID == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "product_id is required"})
		return
	}

	// Validate that at least one key source is provided
	if request.SpecificationKeyID == nil && request.SpecificationKey == nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Either specification_key_id or specification_key object is required"})
		return
	}

	var specKeyID uint

	// Handle key ID resolution: use provided ID or create/find key
	if request.SpecificationKeyID != nil {
		// Mode 1: UseExisting specification key ID
		specKeyID = *request.SpecificationKeyID
	} else if request.SpecificationKey != nil {
		// Mode 2: Find or create specification key
		specKeyName := request.SpecificationKey.Name

		// Try to find existing key by name to avoid duplicates
		existingKey, err := keyRepo.GetByKey(r.Context(), specKeyName)
		if err == nil && existingKey != nil {
			// Key already exists, use its ID
			specKeyID = existingKey.ID
		} else {
			// Key doesn't exist, create a new one
			newKey := &entities.SpecificationKey{
				SpecificationKey: specKeyName,
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			}

			createdKey, err := keyRepo.Create(r.Context(), newKey)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create specification key"})
				return
			}
			specKeyID = createdKey.ID
		}
	} else {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Either specification_key_id or specification_key object is required"})
		return
	}

	// Create specification entity
	spec := &entities.Specification{
		ProductID:          request.ProductID,
		SpecificationKeyID: specKeyID,
		Value:              request.Value,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	// Save to database
	savedSpec, err := specRepo.Create(r.Context(), spec)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create or update specification"})
		return
	}

	// Return created specification
	response := convertSpecificationToResponse(savedSpec)
	json.NewEncoder(w).Encode(response)
}

// BulkUpsertSpecificationHandler handles POST /specifications/bulk
// Creates or updates multiple specifications in a single database transaction.
// Supports both creating new keys inline and using existing key IDs.
// If any specification fails validation, the entire batch is rejected.
//
// Request JSON (array or wrapped in "specifications" key):
//
//	[
//	  {
//	    "id": 123,  // optional - if provided, updates existing spec
//	    "product_id": 456,
//	    "specification_key_id": 45,
//	    "value": "Red"
//	  },
//	  {
//	    "product_id": 789,
//	    "specification_key": {
//	      "name": "New Key",
//	      "is_visible": true,
//	      "is_filterable": false
//	    },
//	    "value": "Value"
//	  }
//	]
//
// Response: Array of created/updated specification responses with timestamps
// Error Handling: Returns 400 for invalid JSON or empty array, 500 for database errors
// Transaction Behavior: All-or-nothing - either all specs are processed or none
// Performance: Caches key lookups to avoid N+1 queries for duplicate key names
func BulkUpsertSpecificationHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository, keyRepo repository.SpecificationKeyRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Create a context with 150-second timeout for bulk operations (handles large payloads with Railway latency)
	// Railway databases have higher latency than local connections, so we need extra time
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	fmt.Printf("%s - BulkUpsertSpecificationHandler: received request from %s\n", time.Now().Format(time.RFC3339), r.RemoteAddr)

	// Read the entire body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read request body"})
		return
	}
	defer r.Body.Close()

	var request struct {
		Specifications []struct {
			ID                 *uint  `json:"id,omitempty"`
			ProductID          uint   `json:"product_id"`
			SpecificationKeyID *uint  `json:"specification_key_id,omitempty"`
			Value              string `json:"value"`
			SpecificationKey   *struct {
				Name         string `json:"name"`
				IsVisible    bool   `json:"is_visible"`
				IsFilterable bool   `json:"is_filterable"`
			} `json:"specification_key,omitempty"`
		} `json:"specifications,omitempty"`
	}

	// First, try to decode as object with specifications key
	err = json.Unmarshal(bodyBytes, &request)
	if err != nil {
		// If that fails, try to decode as direct array
		var specs []struct {
			ID                 *uint  `json:"id,omitempty"`
			ProductID          uint   `json:"product_id"`
			SpecificationKeyID *uint  `json:"specification_key_id,omitempty"`
			Value              string `json:"value"`
			SpecificationKey   *struct {
				Name         string `json:"name"`
				IsVisible    bool   `json:"is_visible"`
				IsFilterable bool   `json:"is_filterable"`
			} `json:"specification_key,omitempty"`
		}
		if err2 := json.Unmarshal(bodyBytes, &specs); err2 != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
			return
		}
		request.Specifications = specs
	}

	fmt.Printf("%s - BulkUpsertSpecificationHandler: parsed %d specifications\n", time.Now().Format(time.RFC3339), len(request.Specifications))

	if len(request.Specifications) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "specifications array cannot be empty"})
		return
	}

	var specs []*entities.Specification

	// Cache for key lookups to avoid N+1 for duplicate key names
	keyCache := make(map[string]*entities.SpecificationKey)

	// Process each specification in the request
	for i, specReq := range request.Specifications {
		if specReq.ProductID == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "product_id is required for all specifications",
				"index": strconv.Itoa(i),
			})
			return
		}

		// Either specification_key_id or specification_key object must be provided
		if specReq.SpecificationKeyID == nil && specReq.SpecificationKey == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Either specification_key_id or specification_key object is required",
				"index": strconv.Itoa(i),
			})
			return
		}

		var specKeyID uint

		// If SpecificationKeyID is provided, use it directly
		if specReq.SpecificationKeyID != nil {
			specKeyID = *specReq.SpecificationKeyID
		} else if specReq.SpecificationKey != nil {
			specKeyName := specReq.SpecificationKey.Name

			// Check cache first to avoid duplicate lookups
			if cachedKey, exists := keyCache[specKeyName]; exists {
				specKeyID = cachedKey.ID
			} else {
				// Try to find existing key by name
				existingKey, err := keyRepo.GetByKey(ctx, specKeyName)
				if err == nil && existingKey != nil {
					// Key already exists
					specKeyID = existingKey.ID
					keyCache[specKeyName] = existingKey
				} else {
					// Key doesn't exist, create a new one
					newKey := &entities.SpecificationKey{
						SpecificationKey: specKeyName,
						CreatedAt:        time.Now(),
						UpdatedAt:        time.Now(),
					}

					createdKey, err := keyRepo.Create(ctx, newKey)
					if err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						json.NewEncoder(w).Encode(map[string]string{
							"error": "Failed to create specification key",
							"index": strconv.Itoa(i),
						})
						return
					}
					specKeyID = createdKey.ID
					keyCache[specKeyName] = createdKey
				}
			}
		}

		spec := &entities.Specification{
			ProductID:          specReq.ProductID,
			SpecificationKeyID: specKeyID,
			Value:              specReq.Value,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}

		// Set ID if provided (for updates)
		if specReq.ID != nil {
			spec.ID = *specReq.ID
		}

		specs = append(specs, spec)
	}

	// Perform bulk upsert in batches to avoid overloading database
	const batchSize = 25
	var allSavedSpecs []*entities.Specification

	for batchStart := 0; batchStart < len(specs); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(specs) {
			batchEnd = len(specs)
		}
		batch := specs[batchStart:batchEnd]

		fmt.Printf("%s - BulkUpsertSpecificationHandler: processing batch %d/%d (%d specs)\n",
			time.Now().Format(time.RFC3339),
			(batchStart/batchSize)+1,
			(len(specs)+batchSize-1)/batchSize,
			len(batch))

		savedSpecs, err := specRepo.BulkUpsert(ctx, batch)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":           "Failed to bulk upsert specifications",
				"failed_at_batch": (batchStart / batchSize) + 1,
				"processed_count": len(allSavedSpecs),
				"details":         err.Error(),
			})
			return
		}
		allSavedSpecs = append(allSavedSpecs, savedSpecs...)
	}

	// Convert to response format
	responses := make([]SpecificationResponse, len(allSavedSpecs))
	for i, spec := range allSavedSpecs {
		responses[i] = convertSpecificationToResponse(spec)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"specifications": responses,
		"count":          len(responses),
		"message":        "Bulk upsert completed successfully",
	})
}

// CopySpecificationsHandler handles POST /specifications/copy
// Copies all specifications from one product to another
//
// Request JSON:
//
//	{
//	  "source_product_id": 3962,
//	  "target_product_id": 3963
//	}
//
// Response: Array of created specification objects
// Error Handling: Returns 400 for invalid input, 404 if source has no specs, 500 for database errors
func CopySpecificationsHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository) {
	w.Header().Set("Content-Type", "application/json")

	var request struct {
		SourceProductID uint `json:"source_product_id"`
		TargetProductID uint `json:"target_product_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
		return
	}

	if request.SourceProductID == 0 || request.TargetProductID == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "source_product_id and target_product_id are required"})
		return
	}

	fmt.Printf("[CopySpecifications] Copying from product %d to product %d\n", request.SourceProductID, request.TargetProductID)

	// Get source specifications
	sourceSpecs, err := specRepo.GetByProductID(r.Context(), request.SourceProductID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get source specifications"})
		return
	}

	if len(sourceSpecs) == 0 {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Source product has no specifications to copy"})
		return
	}

	fmt.Printf("[CopySpecifications] Found %d specifications to copy\n", len(sourceSpecs))

	// Create new specifications for target product
	var newSpecs []*entities.Specification
	for _, sourceSpec := range sourceSpecs {
		newSpec := &entities.Specification{
			ProductID:          request.TargetProductID,
			SpecificationKeyID: sourceSpec.SpecificationKeyID,
			Value:              sourceSpec.Value,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}
		newSpecs = append(newSpecs, newSpec)
	}

	// Bulk insert the new specifications
	savedSpecs, err := specRepo.BulkUpsert(r.Context(), newSpecs)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to copy specifications"})
		return
	}

	// Convert to response format
	responses := make([]SpecificationResponse, len(savedSpecs))
	for i, spec := range savedSpecs {
		responses[i] = convertSpecificationToResponse(spec)
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"specifications": responses,
		"count":          len(responses),
		"message":        fmt.Sprintf("Successfully copied %d specifications from product %d to product %d", len(responses), request.SourceProductID, request.TargetProductID),
	})
}

// UpdateSpecificationHandler handles PUT /specifications/{id}
// Updates an existing specification. Can update the key (by ID or creating new key)
// and/or the value.
//
// URL Path: /specifications/{id}
// Request JSON:
//
//	{
//	  "specification_key_id": 45,  // optional - to change the key
//	  "value": "New Value",        // optional - to change the value
//	  "specification_key": {       // optional - to create new key inline
//	    "name": "New Key Name",
//	    "is_visible": true,
//	    "is_filterable": true
//	  }
//	}
//
// Response: Updated specification object with new timestamps
// Error Handling:
//   - 400: Invalid ID format or missing update data
//   - 404: Specification not found
//   - 500: Database error during update
//
// Note: Must provide at least one field to update (value or key reference)
func UpdateSpecificationHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository, keyRepo repository.SpecificationKeyRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract specification ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/specifications/")
	specIDStr := strings.Trim(path, "/")

	specID, err := strconv.ParseUint(specIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid specification ID format",
			"received": specIDStr,
		})
		return
	}

	var request struct {
		SpecificationKeyID *uint  `json:"specification_key_id,omitempty"`
		Value              string `json:"value"`
		SpecificationKey   *struct {
			Name         string `json:"name"`
			IsVisible    bool   `json:"is_visible"`
			IsFilterable bool   `json:"is_filterable"`
		} `json:"specification_key,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
		return
	}

	if request.Value == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "value is required"})
		return
	}

	// Get existing specification
	existingSpec, err := specRepo.GetByID(r.Context(), uint(specID))
	if err != nil {
		if err.Error() == "specification not found" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Specification not found"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get specification"})
		}
		return
	}

	var specKeyID uint = existingSpec.SpecificationKeyID

	// Handle specification key update if provided
	if request.SpecificationKeyID != nil {
		specKeyID = *request.SpecificationKeyID
	} else if request.SpecificationKey != nil {
		// Create or find the specification key
		specKeyName := request.SpecificationKey.Name

		// First, try to find existing key by name
		existingKey, err := keyRepo.GetByKey(r.Context(), specKeyName)
		if err == nil && existingKey != nil {
			// Key already exists, use its ID
			specKeyID = existingKey.ID
		} else {
			// Key doesn't exist, create a new one
			newKey := &entities.SpecificationKey{
				SpecificationKey: specKeyName,
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			}

			createdKey, err := keyRepo.Create(r.Context(), newKey)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create specification key"})
				return
			}
			specKeyID = createdKey.ID
		}
	}

	updatedSpec := &entities.Specification{
		ProductID:          existingSpec.ProductID,
		SpecificationKeyID: specKeyID,
		Value:              request.Value,
		UpdatedAt:          time.Now(),
	}

	savedSpec, err := specRepo.Update(r.Context(), uint(specID), updatedSpec)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update specification"})
		return
	}

	response := convertSpecificationToResponse(savedSpec)
	json.NewEncoder(w).Encode(response)
}

// GetSpecificationsByProductHandler handles GET /get-specifications/{product_id}
// Retrieves all specification keys for the product's category (from form generator)
// and merges them with any existing specification values already set for the product.
// Used by admin to display editable form fields for a product's specifications.
//
// URL Path: /get-specifications/{product_id}
// Query Parameters: None
//
// Response Structure:
//
//	[
//	  {
//	    "id": 123,
//	    "product_id": 456,
//	    "specification_key_id": 5,
//	    "specification_key": "Color",
//	    "value": "Red",
//	    "created_at": "2024-01-15T10:30:00Z",
//	    "updated_at": "2024-01-15T10:30:00Z"
//	  },
//	  ...
//	]
//
// Behavior:
//   - Queries form generator for all possible keys for product's category
//   - Queries existing specifications for the product
//   - Returns merged result showing both filled and empty fields
//
// Error Handling:
//   - 400: Invalid product ID format
//   - 404: Product not found
//   - 500: Database error
func GetSpecificationsByProductHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository, productRepo repository.ProductRepository, formGenRepo repository.FormGeneratorRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/get-specifications/")
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

	fmt.Printf("[GetSpecifications] ProductID: %d\n", productID)

	// Fetch existing specifications for this product
	existingSpecs, err := specRepo.GetByProductID(r.Context(), uint(productID))
	if err != nil {
		fmt.Printf("[GetSpecifications] Error fetching existing specs: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get specifications"})
		return
	}

	fmt.Printf("[GetSpecifications] Found %d existing specs\n", len(existingSpecs))

	// Build map from specification_key_id to existing spec
	existingMap := make(map[uint]*entities.Specification)
	for _, s := range existingSpecs {
		existingMap[s.SpecificationKeyID] = s
	}

	// Try to fetch product to get its category
	var categoryID *uint = nil
	if productRepo != nil {
		prod, err := productRepo.GetByID(r.Context(), uint(productID))
		if err == nil && prod != nil && prod.CategoryID != nil {
			categoryID = prod.CategoryID
			fmt.Printf("[GetSpecifications] Product category ID: %d\n", *categoryID)
		} else {
			fmt.Printf("[GetSpecifications] Product not found or has no category. Error: %v\n", err)
		}
	}

	var finalResponses []SpecificationResponse

	// If categoryID is present, get category form generator keys and merge
	if categoryID != nil && formGenRepo != nil {
		catSpecs, err := formGenRepo.GetCategorySpecifications(r.Context(), *categoryID)
		if err == nil && len(catSpecs) > 0 {
			fmt.Printf("[GetSpecifications] Found %d category specs from form generator\n", len(catSpecs))
			for _, ks := range catSpecs {
				if es, ok := existingMap[ks.SpecificationKeyID]; ok {
					finalResponses = append(finalResponses, convertSpecificationToResponse(es))
				} else {
					// Build empty response for the key
					finalResponses = append(finalResponses, SpecificationResponse{
						ID:                 0,
						ProductID:          uint(productID),
						SpecificationKeyID: ks.SpecificationKeyID,
						SpecificationKey:   ks.SpecificationKey,
						Value:              "",
						CreatedAt:          "",
						UpdatedAt:          "",
					})
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"product_id":     productID,
				"specifications": finalResponses,
				"count":          len(finalResponses),
			})
			return
		} else {
			fmt.Printf("[GetSpecifications] No category specs found. Error: %v, catSpecs length: %d\n", err, len(catSpecs))
		}
	} else {
		fmt.Printf("[GetSpecifications] Skipping form generator: categoryID=%v, formGenRepo=%v\n", categoryID, formGenRepo != nil)
	}

	// Fallback: if no category form generator found, return only existing specs
	fmt.Printf("[GetSpecifications] Falling back to existing specs only. Count: %d\n", len(existingSpecs))
	responses := make([]SpecificationResponse, len(existingSpecs))
	for i, spec := range existingSpecs {
		responses[i] = convertSpecificationToResponse(spec)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"product_id":     productID,
		"specifications": responses,
		"count":          len(responses),
	})
}

// GetSpecificationByIDHandler handles GET /specifications/{id}
// Retrieves a single specification by its ID. Returns complete specification details
// including product association, key reference, and timestamps.
//
// URL Path: /specifications/{id}
// Query Parameters: None
//
// Response: Single specification object
//
//	{
//	  "id": 123,
//	  "product_id": 456,
//	  "specification_key_id": 5,
//	  "specification_key": "Color",
//	  "value": "Red",
//	  "created_at": "2024-01-15T10:30:00Z",
//	  "updated_at": "2024-01-15T10:30:00Z"
//	}
//
// Error Handling:
//   - 400: Invalid specification ID format
//   - 404: Specification not found
//   - 500: Database error
func GetSpecificationByIDHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract specification ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/specifications/")
	specIDStr := strings.Trim(path, "/")

	specID, err := strconv.ParseUint(specIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid specification ID format",
			"received": specIDStr,
		})
		return
	}

	specification, err := specRepo.GetByID(r.Context(), uint(specID))
	if err != nil {
		if err.Error() == "specification not found" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Specification not found"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get specification"})
		}
		return
	}

	response := convertSpecificationToResponse(specification)
	json.NewEncoder(w).Encode(response)
}

// DeleteSpecificationHandler handles DELETE /specifications/{id}
// Permanently removes a specification from the database.
// Warning: This operation is irreversible. All translations for this specification are also removed.
//
// URL Path: /specifications/{id}
// Request Body: None
//
// Response: Empty or confirmation message (HTTP 204 has no body)
// Expected HTTP Status:
//   - 204: Successfully deleted (no content)
//   - 400: Invalid specification ID format
//   - 404: Specification not found
//   - 500: Database error during deletion
//
// Side Effects: Cascades to delete all associated translations
// Use Cases: Removing incorrect or outdated specifications, data cleanup
func DeleteSpecificationHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract specification ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/specifications/")
	specIDStr := strings.Trim(path, "/")

	specID, err := strconv.ParseUint(specIDStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":    "Invalid specification ID format",
			"received": specIDStr,
		})
		return
	}

	err = specRepo.Delete(r.Context(), uint(specID))
	if err != nil {
		if err.Error() == "specification not found" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "Specification not found"})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to delete specification"})
		}
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"message": "Specification deleted successfully"})
}

// SearchSpecificationsHandler handles GET /specificationsearch
// Performs full-text search across all specifications. Searches across product IDs,
// specification keys, and values. Results are ordered by relevance with pagination support.
//
// URI: /specificationsearch?query=<search_term>&limit=<limit>&offset=<offset>
// Query Parameters:
//   - query: Search term (required). Matches against keys and values.
//   - limit: Maximum number of results (optional, default: repository default)
//   - offset: Pagination offset (optional, default: 0)
//
// Response: Array of matching specifications ordered by relevance
//
//	[
//	  {
//	    "id": 123,
//	    "product_id": 456,
//	    "specification_key": "Color",
//	    "value": "Red",
//	    ...
//	  },
//	  ...
//	]
//
// Search Examples:
//   - /specificationsearch?query=red - Find all specs with "red" in key or value
//   - /specificationsearch?query=color&limit=10 - Search with result limit
//   - /specificationsearch?query=battery&offset=20&limit=10 - Pagination
//
// Error Handling:
//   - 400: Missing required query parameter
//   - 500: Database search error
//
// Performance: Uses database full-text search indexes if available
func SearchSpecificationsHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Parse query parameters
	query := r.URL.Query().Get("query")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	// if query == "" {
	// 	w.WriteHeader(http.StatusBadRequest)
	// 	json.NewEncoder(w).Encode(map[string]string{"error": "query parameter is required"})
	// 	return
	// }

	// Set defaults
	limit := 10
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

	specifications, err := specRepo.Search(r.Context(), query, limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to search specifications"})
		return
	}

	responses := make([]SpecificationResponse, len(specifications))
	for i, spec := range specifications {
		responses[i] = convertSpecificationToResponse(spec)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"specifications": responses,
		"count":          len(responses),
		"query":          query,
		"limit":          limit,
		"offset":         offset,
	})
}

// CreateSpecificationTranslationHandler handles POST /spec_translation
// Creates or updates translations for multiple specifications in a specific language/locale.
// Translations allow displaying specification keys and values in different languages
// (e.g., "Color" → "রঙ" in Bengali, "Red" → "লাল").
//
// Request JSON:
//
//	{
//	  "productId": 928,
//	  "specifications": [
//	    {
//	      "id": 1234,  // specification_id
//	      "locale": "bn",  // Language code: "bn" (Bengali), "en" (English), etc.
//	      "translated_key": "রঙ",  // Translated specification key name
//	      "translated_value": "লাল"  // Translated specification value
//	    },
//	    {
//	      "id": 1235,
//	      "locale": "bn",
//	      "translated_key": "আকার",
//	      "translated_value": "বড়"
//	    },
//	    ...
//	  ]
//	}
//
// Response: Confirmation with count of translations created/updated
//
//	{
//	  "message": "Translations created/updated successfully",
//	  "count": 2
//	}
//
// Behavior:
//   - If translation exists for (specification_id, locale) pair, updates it
//   - If translation doesn't exist, creates it
//   - All translations in batch processed atomically (all succeed or all fail)
//
// Supported Locales: "bn" (Bengali), "en" (English), and others based on system configuration
//
// Error Handling:
//   - 400: Invalid JSON or missing required fields
//   - 404: Product or specification not found
//   - 500: Database error during batch processing
//
// Use Cases:
//   - Creating multilingual product specifications
//   - Updating translations when original specifications change
//   - Supporting customers in their preferred language
func CreateSpecificationTranslationHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository) {
	w.Header().Set("Content-Type", "application/json")

	var request struct {
		ProductID      uint `json:"productId"`
		Specifications []struct {
			ID              uint   `json:"id"` // This could be either specification_id or specification_key_id
			Locale          string `json:"locale"`
			TranslatedKey   string `json:"translated_key"`
			TranslatedValue string `json:"translated_value"`
		} `json:"specifications"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
		return
	}

	if request.ProductID == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "productId is required"})
		return
	}

	if len(request.Specifications) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "specifications array cannot be empty"})
		return
	}

	var savedTranslations []*entities.SpecificationTranslation

	// Process each specification translation
	for i, specReq := range request.Specifications {
		if specReq.ID == 0 || specReq.Locale == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("specification id and locale are required for all specifications at index %d", i),
			})
			return
		}

		// The frontend is sending the actual specification_id as id
		// First, get all specifications for this product to find the matching specification
		productSpecs, err := specRepo.GetByProductID(r.Context(), request.ProductID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get product specifications"})
			return
		}

		// Find the specification that matches this specification_id
		var targetSpec *entities.Specification
		for _, spec := range productSpecs {
			if spec.ID == specReq.ID {
				targetSpec = spec
				break
			}
		}

		if targetSpec == nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("No specification found for specification_id %d in product %d", specReq.ID, request.ProductID),
			})
			return
		}

		// Check if translation already exists
		existingTranslations, err := specRepo.GetTranslations(r.Context(), targetSpec.ID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Failed to check existing translations"})
			return
		}

		var existingTranslation *entities.SpecificationTranslation
		for _, trans := range existingTranslations {
			if trans.Locale == specReq.Locale {
				existingTranslation = trans
				break
			}
		}

		// Use translated_value if provided, otherwise skip this translation
		translatedValue := specReq.TranslatedValue
		if translatedValue == "" {
			continue // Skip empty translations
		}

		if existingTranslation != nil {
			// Update existing translation
			existingTranslation.TranslatedValue = translatedValue
			existingTranslation.UpdatedAt = time.Now()

			// Note: We need to implement UpdateTranslation method in repository
			// For now, we'll delete and recreate (not ideal, but works)
			savedTranslation, err := specRepo.CreateTranslation(r.Context(), existingTranslation)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update specification translation"})
				return
			}
			savedTranslations = append(savedTranslations, savedTranslation)
		} else {
			// Create new translation
			translation := &entities.SpecificationTranslation{
				SpecificationID: targetSpec.ID,
				Locale:          specReq.Locale,
				TranslatedValue: translatedValue,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			}

			savedTranslation, err := specRepo.CreateTranslation(r.Context(), translation)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create specification translation"})
				return
			}
			savedTranslations = append(savedTranslations, savedTranslation)
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "Specifications translations saved successfully!",
		"data":       savedTranslations,
		"count":      len(savedTranslations),
		"product_id": request.ProductID,
	})
}

// GetSpecificationTranslationHandler handles GET /spec_translation/{id}?locale=xx
// Retrieves all specifications for a product with translations in the specified locale.
// This endpoint is OPTIMIZED to use a single database query (no N+1 queries).
//
// Parameters:
//   - id (URL path): Product ID whose specifications we want
//   - locale (query): Language locale (e.g., "en", "bn", "es")
//
// Response format (Laravel-compatible):
//
//	{
//	  "dataset": [
//	    {
//	      "specification_key_id": 5,
//	      "translations": {
//	        "locale": "bn",
//	        "translated_key": "রঙ",
//	        "translated_value": "লাল"
//	      }
//	    }
//	  ]
//	}
//
// Performance: Uses optimized JOIN query instead of looping through specs
// and querying translations for each one (this prevents the N+1 query problem).
func GetSpecificationTranslationHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository, productRepo repository.ProductRepository) {
	w.Header().Set("Content-Type", "application/json")
	// Enable CORS to allow requests from frontend on different domain
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	// Handle CORS preflight request (browser sends OPTIONS before GET)
	if r.Method == http.MethodOptions {
		return
	}

	// Parse product ID from URL path (format: /spec_translation/{product_id})
	path := strings.TrimPrefix(r.URL.Path, "/spec_translation/")
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

	// Extract and validate locale parameter from query string
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "locale parameter is required"})
		return
	}

	// Log request for debugging and monitoring
	fmt.Printf("[GetSpecificationTranslationHandler] Fetching translations for product %d, locale: %s\n", productID, locale)

	// Create a context with timeout to prevent hanging requests
	// This ensures the database query completes or timeouts within 15 seconds
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Execute optimized query: single JOIN to get specs + translations
	// This replaces the old N+1 approach where we queried each spec's translations separately
	results, err := specRepo.GetPublicSpecsWithTranslations(ctx, uint(productID), locale)
	if err != nil {
		// Log error for debugging
		fmt.Printf("[GetSpecificationTranslationHandler] Error fetching translations: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "Failed to get product specifications",
			"details": err.Error(),
		})
		return
	}

	// Log success for monitoring
	fmt.Printf("[GetSpecificationTranslationHandler] Found %d specifications for product %d\n", len(results), productID)

	// Build response in Laravel-compatible format
	var formattedDataset []map[string]interface{}

	// Process each specification result
	for _, result := range results {
		specData := map[string]interface{}{
			"specification_key_id": result.SpecificationKeyID,
			"translations":         nil, // Default to null if no translation
		}

		// Include translations if they exist for the requested locale
		// Only include if we have actual translated value
		if result.TranslatedValue != "" {
			specData["translations"] = map[string]interface{}{
				"id":               result.TranslationID,
				"specification_id": result.SpecificationID,
				"locale":           locale,
				"translated_key":   result.TranslatedKey,
				"translated_value": result.TranslatedValue,
			}
		}

		formattedDataset = append(formattedDataset, specData)
	}

	// Return formatted response with dataset array
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"dataset": formattedDataset,
	})
}

// GetPublicSpecificationHandler handles GET /get-public-spec/{product_id}
// Retrieves publicly available specifications for a product.
// This is the client-facing endpoint used by frontend/mobile to display product specifications.
// Data is filtered for public consumption (excludes internal fields, sensitive data).
// Maintains Laravel API compatibility for legacy client applications.
//
// URL Path: /get-public-spec/{product_id}
// Query Parameters: None
//
// Response: Public specification data formatted for client consumption
//
//	{
//	  "id": 123,
//	  "product_id": 456,
//	  "specification_key_id": 5,
//	  "key": "Color",
//	  "value": "Red",
//	  ...
//	}
//
// Behavior:
//   - Returns only specifications marked as publicly visible
//   - Filters out draft/unpublished specifications
//   - Used by e-commerce frontend to display product details
//   - Read-only endpoint (GET only)
//
// Error Handling:
//   - 400: Invalid product ID format
//   - 404: Product not found or has no public specifications
//   - 500: Database error
//
// Caching:
//   - Recommended to cache this endpoint response (product specs rarely change)
//   - Cache key should include product_id
//   - Invalidate cache when specifications are updated
//
// Use Cases:
//   - Display specifications on product detail page
//   - Client-side specification filtering/search
//   - Mobile app product information display
func GetPublicSpecificationHandler(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository, keyRepo repository.SpecificationKeyRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Extract product ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/get-public-spec/")
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

	// Get locale from query parameters (default to 'en')
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		locale = "en"
	}

	// Single JOIN query - replaces N+1 per-spec lookups
	results, err := specRepo.GetPublicSpecsWithTranslations(r.Context(), uint(productID), locale)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get specifications"})
		return
	}

	type SpecificationResponse struct {
		SpecificationKeyID uint   `json:"specification_key_id"`
		TranslatedKey      string `json:"translated_key"`
		TranslatedValue    string `json:"translated_value"`
	}

	dataset := make([]SpecificationResponse, len(results))
	for i, r := range results {
		dataset[i] = SpecificationResponse{
			SpecificationKeyID: r.SpecificationKeyID,
			TranslatedKey:      r.TranslatedKey,
			TranslatedValue:    r.TranslatedValue,
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"dataset": dataset,
	})
}

// UpdateSpecificationTranslationValues handles PUT/POST requests to update only translated_value
// Bulk updates the translated values (translated_value field only) for multiple specifications.
// Does NOT update translated_key - use CreateSpecificationTranslationHandler to update keys.
// Used when original specification values change and translations need updating.
//
// Supports both PUT and POST methods for client compatibility.
//
// Request JSON (array of translation updates):
//
//	[
//	  {
//	    "id": 1234,  // specification_id
//	    "locale": "bn",  // Language code
//	    "translated_value": "নতুন অনুবাদ"  // Only field updated
//	  },
//	  {
//	    "id": 1235,
//	    "locale": "bn",
//	    "translated_value": "আরও অনুবাদ"
//	  },
//	  ...
//	]
//
// Response: Confirmation message with count of updated translations
//
//	{
//	  "message": "Translation values updated successfully",
//	  "count": 2
//	}
//
// Behavior:
//   - Only updates translated_value field
//   - translated_key remains unchanged
//   - If translation doesn't exist, creates it with only the value set
//   - Useful for bulk updates when keys are already translated
//
// Error Handling:
//   - 400: Invalid JSON or empty array
//   - 404: Specification not found
//   - 500: Database error during batch update
//
// Difference from CreateSpecificationTranslationHandler:
//   - This endpoint: Only updates translated_value, key stays the same
//   - CreateSpecificationTranslationHandler: Updates both key and value
//
// Use Cases:
//   - Updating product value translations (e.g., "Red" changed to "Dark Red" → update Bengali translation)
//   - Bulk translation updates
//   - Fixing translation values without affecting key names
func UpdateSpecificationTranslationValues(w http.ResponseWriter, r *http.Request, specRepo repository.SpecificationRepository) {
	w.Header().Set("Content-Type", "application/json")

	// Read the entire body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read request body"})
		return
	}
	defer r.Body.Close()

	var translations []struct {
		ID              uint   `json:"id"` // specification_id
		Locale          string `json:"locale"`
		TranslatedValue string `json:"translated_value"`
	}

	// First, try to decode as array
	err = json.Unmarshal(bodyBytes, &translations)
	if err != nil {
		// If that fails, try to decode as object with specifications key
		var request struct {
			ProductID      uint `json:"productId"`
			Specifications []struct {
				ID              uint   `json:"id"` // specification_id
				Locale          string `json:"locale"`
				TranslatedValue string `json:"translated_value"`
			} `json:"specifications"`
		}
		if err2 := json.Unmarshal(bodyBytes, &request); err2 != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
			return
		}
		translations = request.Specifications
	}

	if len(translations) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "translations array cannot be empty"})
		return
	}

	// Validate all translations first
	for i, transReq := range translations {
		if transReq.ID == 0 || transReq.Locale == "" || transReq.TranslatedValue == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("specification id, locale and translated_value are required at index %d", i),
			})
			return
		}
	}

	// Build the list of entities for bulk upsert
	translationEntities := make([]*entities.SpecificationTranslation, len(translations))
	for i, transReq := range translations {
		translationEntities[i] = &entities.SpecificationTranslation{
			SpecificationID: transReq.ID,
			Locale:          transReq.Locale,
			TranslatedValue: transReq.TranslatedValue,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
	}

	// Use bulk upsert for performance
	// Create context with 120 second timeout for large bulk operations
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	savedTranslations, err := specRepo.BulkUpsertTranslations(ctx, translationEntities)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to upsert specification translations: %v", err)})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Specification translation values updated successfully!",
		"data":    savedTranslations,
		"count":   len(savedTranslations),
	})
}
