package specification

import (
	"kossti/internal/domain/repository"
	"net/http"
	"strings"
)

// RegisterSpecificationRoutes registers specification-related endpoints to the mux.
// This function sets up all HTTP handlers for product specifications, specification keys, and their translations.
//
// Route Categories:
// 1. PUBLIC SPECIFICATION ENDPOINTS - CRUD operations for product specifications
//   - POST /specifications - Create new specification for product
//   - POST /specifications/bulk - Bulk upsert specifications (insert or update many at once)
//   - GET /get-specifications/{product_id} - Retrieve all specs for a specific product (with form generator metadata)
//   - GET /specifications/{id} - Get single specification by ID
//   - PUT /specifications/{id} - Update existing specification
//   - DELETE /specifications/{id} - Delete specification by ID
//   - GET /specificationsearch - Full-text search across all specifications
//
// 2. TRANSLATION ENDPOINTS - Manage specification translations to different languages
//   - POST /spec_translation - Create/update specification translation
//   - GET /spec_translation/{product_id}?locale=xx - Retrieve all specifications with translations in specified locale
//     (OPTIMIZED: Uses single JOIN query instead of N+1 loop - critical for performance)
//   - PUT /spec_translation/values - Update only the translated values (bulk update)
//
// 3. PUBLIC API ENDPOINTS - Client-facing read-only specification endpoints
//   - GET /get-public-spec/{product_id} - Get public specification data (filtered for client consumption)
//
// 4. SPECIFICATION KEY ENDPOINTS - Manage the specification key definitions/schema
//   - POST /speckey - Create new specification key or list all keys via GET
//   - GET /speckey - Get all specification keys with pagination
//   - GET /speckey/{id} - Get single specification key by ID
//   - POST /specremove/{id} - Delete specification key by ID
//
// 5. SPECIFICATION KEY TRANSLATION ENDPOINTS - Translate specification key names to different languages
//   - POST /speckey-translation - Create/update specification key translation
//   - GET /speckey-translation - Get all specification key translations
//
// Parameters:
//   - mux: HTTP request multiplexer to register handlers with
//   - specRepo: Repository for specification CRUD and translation operations
//   - keyRepo: Repository for specification key CRUD and translation operations
//   - productRepo: Repository for product lookups
//   - formGenRepo: Repository for form generator configurations
//
// Performance Notes:
// - Specification translation endpoint (GET /spec_translation/{id}) uses optimized JOIN query
// - Avoids N+1 query problems that previously occurred when looping through specs
// - Database context timeout: 15 seconds for all database operations
func RegisterSpecificationRoutes(mux *http.ServeMux, specRepo repository.SpecificationRepository, keyRepo repository.SpecificationKeyRepository, productRepo repository.ProductRepository, formGenRepo repository.FormGeneratorRepository) {

	// ============= PUBLIC SPECIFICATION ENDPOINTS =============
	// CRUD operations for product specifications

	// POST /specifications - Create specification
	// Creates a new specification for a product. Can either use an existing specification key
	// or create a new key during the creation process.
	//
	// Request Body:
	//   {
	//     "product_id": <number>,
	//     "specification_key_id": <number>,  // optional if creating new key
	//     "key": "<string>",                  // new key name (if specification_key_id not provided)
	//     "value": "<string>"
	//   }
	//
	// Response: Created specification object with ID
	// Expected HTTP Status: 201 Created | 400 Bad Request | 500 Internal Server Error
	mux.HandleFunc("/specifications", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only POST method is allowed"}`))
			return
		}
		CreateSpecificationHandler(w, r, specRepo, keyRepo)
	})

	// POST /specifications/bulk - Bulk upsert specifications
	// Inserts or updates multiple specifications in a single request. Useful for batch operations
	// when seeding database or migrating product specifications from external sources.
	//
	// Request Body:
	//   Array of specification objects with same structure as single create
	//   [
	//     {"product_id": 1, "specification_key_id": 5, "value": "..."},
	//     {"product_id": 2, "key": "new_key", "value": "..."}
	//   ]
	//
	// Response: Array of created/updated specification objects with their IDs
	// Expected HTTP Status: 201 Created | 400 Bad Request | 500 Internal Server Error
	mux.HandleFunc("/specifications/bulk", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only POST method is allowed"}`))
			return
		}
		BulkUpsertSpecificationHandler(w, r, specRepo, keyRepo)
	})

	// POST /specifications/copy - Copy specifications from one product to another
	// Copies all specifications from source product to target product
	//
	// Request Body:
	//   {
	//     "source_product_id": 3962,
	//     "target_product_id": 3963
	//   }
	//
	// Response: Array of created specification objects
	// Expected HTTP Status: 201 Created | 400 Bad Request | 404 Not Found | 500 Internal Server Error
	mux.HandleFunc("/specifications/copy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only POST method is allowed"}`))
			return
		}
		CopySpecificationsHandler(w, r, specRepo)
	})

	// GET /get-specifications/{product_id} - Get specifications by product ID
	// Retrieves all specifications for a product with associated form generator metadata.
	// Used by admin to view/manage all specs for a specific product.
	//
	// URL Path: /get-specifications/{product_id}
	// Query Parameters:
	//   - None required (can include pagination if form generator supports it)
	//
	// Response: Array of specification objects with form generator field definitions
	// Expected HTTP Status: 200 OK | 404 Not Found | 500 Internal Server Error
	mux.HandleFunc("/get-specifications/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only GET method is allowed"}`))
			return
		}
		GetSpecificationsByProductHandler(w, r, specRepo, productRepo, formGenRepo)
	})

	// GET/PUT/DELETE /specifications/{id} - Get, update, or delete specification by ID
	// Flexible endpoint supporting multiple operations on a single specification:
	//   - GET: Retrieve single specification details
	//   - PUT: Update specification (can change key or value)
	//   - DELETE: Remove specification from database
	//
	// URL Path: /specifications/{id}
	// Response: JSON specification object or empty on successful DELETE
	// Expected HTTP Status:
	//   - GET: 200 OK | 404 Not Found | 500 Internal Server Error
	//   - PUT: 200 OK | 400 Bad Request | 404 Not Found | 500 Internal Server Error
	//   - DELETE: 204 No Content | 404 Not Found | 500 Internal Server Error
	mux.HandleFunc("/specifications/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			GetSpecificationByIDHandler(w, r, specRepo)
		} else if r.Method == http.MethodPut {
			UpdateSpecificationHandler(w, r, specRepo, keyRepo)
		} else if r.Method == http.MethodDelete {
			DeleteSpecificationHandler(w, r, specRepo)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only GET, PUT, and DELETE methods are allowed"}`))
		}
	})

	// GET /specificationsearch - Search specifications
	// Full-text search across all specifications. Returns specifications matching the search query
	// across product ID, keys, or values.
	//
	// Query Parameters:
	//   - q: Search query string (required)
	//   - limit: Maximum results to return (optional, default applies)
	//   - offset: Pagination offset (optional, default 0)
	//
	// Response: Array of matching specification objects with relevance ordering
	// Expected HTTP Status: 200 OK | 400 Bad Request (missing query) | 500 Internal Server Error
	mux.HandleFunc("/specificationsearch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only GET method is allowed"}`))
			return
		}
		SearchSpecificationsHandler(w, r, specRepo)
	})

	// ============= TRANSLATION ENDPOINTS =============
	// Manage specification translations to different languages (Bengali, English, etc.)

	// POST /spec_translation - Create specification translation
	// Creates or updates a translation for a specification in a specific language.
	// Each specification can have translations in multiple languages.
	//
	// Request Body:
	//   {
	//     "specification_id": <number>,
	//     "translated_key": "<string in target language>",
	//     "translated_value": "<string in target language>",
	//     "locale": "bn"  // or "en", "fr", etc.
	//   }
	//
	// Response: Created/updated translation object with all fields
	// Expected HTTP Status: 201 Created | 200 OK (if updated) | 400 Bad Request | 500 Internal Server Error
	mux.HandleFunc("/spec_translation", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only POST method is allowed"}`))
			return
		}
		CreateSpecificationTranslationHandler(w, r, specRepo)
	})

	// PUT /spec_translation/values - Update only translated values
	// Bulk updates translation values (the translated_value field only, not keys) for multiple
	// specifications. Used when original specification values change and translations need updating.
	//
	// Request Body:
	//   Array of update objects:
	//   [
	//     {
	//       "specification_id": 123,
	//       "translated_value": "আপডেট করা মূল্য",
	//       "locale": "bn"
	//     },
	//     ...
	//   ]
	//
	// Response: Array of updated translation objects
	// Expected HTTP Status: 200 OK | 400 Bad Request | 404 Not Found | 500 Internal Server Error
	mux.HandleFunc("/spec_translation/values", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only PUT and POST methods are allowed"}`))
			return
		}
		UpdateSpecificationTranslationValues(w, r, specRepo)
	})

	// GET /spec_translation/{product_id} - Get specification translations
	// Retrieves all specifications for a product with their translations in the specified language.
	// CRITICAL PERFORMANCE ENDPOINT: Uses optimized single JOIN query (no N+1 queries).
	//
	// This endpoint was previously causing timeouts due to N+1 query pattern (looping through
	// ~900+ specifications and querying translations individually = ~900+ database round trips).
	// Now uses single SELECT with JOIN to fetch all specs and their translations together.
	//
	// URL Path: /spec_translation/{product_id}
	// Query Parameters:
	//   - locale: Language code (required, e.g., "bn" for Bengali, "en" for English)
	//
	// Response: Array of specification objects with translated_key and translated_value fields
	//   [
	//     {
	//       "id": 123,
	//       "product_id": 928,
	//       "specification_key_id": 5,
	//       "value": "...",
	//       "translated_key": "উদ্ভাবনী",
	//       "translated_value": "অত্যাধুনিক",
	//       "locale": "bn"
	//     },
	//     ...
	//   ]
	//
	// Expected HTTP Status: 200 OK | 400 Bad Request (missing locale) | 404 Not Found | 500 Internal Server Error
	mux.HandleFunc("/spec_translation/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only GET method is allowed"}`))
			return
		}
		GetSpecificationTranslationHandler(w, r, specRepo, productRepo)
	})

	// ============= PUBLIC API ENDPOINTS =============
	// Client-facing read-only endpoints for consuming product specifications

	// GET /get-public-spec/{product_id} - Get public specification
	// Retrieves publicly available specification data for a product. Data is filtered
	// for client consumption (excludes internal fields, sensitive data).
	// Used by frontend/mobile clients to display product specifications.
	//
	// URL Path: /get-public-spec/{product_id}
	// Query Parameters: None
	//
	// Response: Public specification object with filtered fields
	// Expected HTTP Status: 200 OK | 404 Not Found | 500 Internal Server Error
	mux.HandleFunc("/get-public-spec/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only GET method is allowed"}`))
			return
		}
		GetPublicSpecificationHandler(w, r, specRepo, keyRepo)
	})

	// ============= SPECIFICATION KEY ENDPOINTS =============
	// Manage specification key definitions (schema). Specification keys define the available
	// fields/properties that specifications can have (e.g., "Color", "Weight", "Size").
	// These are typically set up once and reused across all products.

	// POST /speckey - Create or update specification key
	// GET /speckey - Get all specification keys with pagination
	// Flexible endpoint supporting:
	//   - POST: Create new specification key or batch create
	//   - GET: Retrieve all specification keys with optional pagination
	//
	// POST Request Body:
	//   {
	//     "key": "<string>",  // e.g., "Color", "Weight"
	//     "value_type": "<string>"  // optional: "text", "number", "boolean", etc.
	//   }
	//
	// Response (POST): Created key object with ID
	// Response (GET): Array of specification keys
	// Expected HTTP Status: 201 Created (POST) | 200 OK (GET) | 400 Bad Request | 500 Internal Server Error
	mux.HandleFunc("/speckey", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			CreateOrUpdateSpecificationKeyHandler(w, r, keyRepo)
		} else if r.Method == http.MethodGet {
			GetAllSpecificationKeysHandler(w, r, keyRepo)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only GET and POST methods are allowed"}`))
		}
	})

	// GET /speckey/{id} - Get specification key by ID
	// Retrieves a single specification key by its ID. The endpoint includes a redirect
	// mechanism to normalize requests (/ suffix redirects to /speckey with query params).
	//
	// URL Path: /speckey/{id}
	// Query Parameters: None
	//
	// Response: Specification key object with all metadata and translations
	// Expected HTTP Status: 200 OK | 301 Moved Permanently (redirect case) | 404 Not Found | 500 Internal Server Error
	// Auto-redirect: Requests to /speckey/ without ID are 301 redirected to /speckey
	mux.HandleFunc("/speckey/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/speckey/")
		if path == "" || path == "/" {
			// This is the base /speckey/ endpoint, redirect to /speckey with query parameters preserved
			redirectURL := "/speckey"
			if r.URL.RawQuery != "" {
				redirectURL += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, redirectURL, http.StatusMovedPermanently)
			return
		}

		if r.Method != http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only GET method is allowed"}`))
			return
		}
		GetSpecificationKeyByIDHandler(w, r, keyRepo)
	})

	// POST /specremove/{id} - Delete specification key
	// Removes a specification key from the system. This typically cascades to remove
	// all associated specifications that use this key, unless protected by foreign key constraints.
	//
	// URL Path: /specremove/{id}
	// Request Body: Empty (ID in URL)
	//
	// Response: Empty or confirmation message
	// Expected HTTP Status: 204 No Content | 404 Not Found | 409 Conflict (if in use) | 500 Internal Server Error
	// Warning: Deleting a key deletes all specifications using that key
	mux.HandleFunc("/specremove/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only POST method is allowed"}`))
			return
		}
		DeleteSpecificationKeyHandler(w, r, keyRepo)
	})

	// ============= SPECIFICATION KEY TRANSLATION ENDPOINTS =============
	// Manage translations of specification key names to different languages.
	// Example: "Color" key translated to "রঙ" in Bengali, "Couleur" in French, etc.

	// POST /speckey-translation - Create specification key translation
	// GET /speckey-translation - Get all specification key translations
	// Flexible endpoint supporting:
	//   - POST: Create/update translation for a specification key
	//   - GET: Retrieve all specification key translations with optional filtering
	//
	// POST Request Body:
	//   {
	//     "specification_key_id": 5,
	//     "translated_key": "রঙ",  // e.g., Bengali translation of "Color"
	//     "locale": "bn"
	//   }
	//
	// Response (POST): Created translation object
	// Response (GET): Array of all key translations across all locales
	// Query Parameters (GET):
	//   - key_id: Filter by specification key ID (optional)
	//   - locale: Filter by language code (optional)
	//
	// Expected HTTP Status: 201 Created (POST) | 200 OK (GET) | 400 Bad Request | 500 Internal Server Error
	mux.HandleFunc("/speckey-translation", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			CreateSpecificationKeyTranslationHandler(w, r, keyRepo)
		} else if r.Method == http.MethodGet {
			GetAllSpecificationKeyTranslationsHandler(w, r, keyRepo)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			w.Write([]byte(`{"error": "Only GET and POST methods are allowed"}`))
		}
	})
}
