package postgres

import (
	"context"
	"errors"
	"fmt"
	"kossti/internal/domain/entities"
	"kossti/internal/domain/repository"
	"kossti/internal/infrastructure/database/models"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

type PostgresProductRepo struct {
	db *gorm.DB
}

func NewPostgresProductRepo(db *gorm.DB) *PostgresProductRepo {
	return &PostgresProductRepo{db: db}
}

// generateSlug creates a URL-friendly slug from a name
func generateSlug(name string) string {
	// Convert to lowercase
	slug := strings.ToLower(name)
	// Replace spaces and special characters with hyphens
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	slug = reg.ReplaceAllString(slug, "-")
	// Remove leading/trailing hyphens
	slug = strings.Trim(slug, "-")
	return slug
}

func (r *PostgresProductRepo) GetByID(ctx context.Context, id uint) (*entities.Product, error) {
	var productModel models.ProductModel
	if err := r.db.WithContext(ctx).Preload("Category").Preload("Brand").First(&productModel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("product not found")
		}
		return nil, err
	}
	return productModel.ToEntity(), nil
}

func (r *PostgresProductRepo) Create(ctx context.Context, product *entities.Product) (*entities.Product, error) {
	// Convert entity to model
	var productModel models.ProductModel
	productModel.FromEntity(product)

	// Generate slug if not provided
	if productModel.Slug == "" {
		productModel.Slug = generateSlug(productModel.Name)
	}

	// Set default category ID if not provided
	if productModel.CategoryID == nil {
		defaultCategoryID := uint(1)
		productModel.CategoryID = &defaultCategoryID
	}

	if err := r.db.WithContext(ctx).Create(&productModel).Error; err != nil {
		return nil, err
	}

	return productModel.ToEntity(), nil
}

func (r *PostgresProductRepo) GetBySlug(ctx context.Context, slug string) (*entities.Product, error) {
	var productModel models.ProductModel
	if err := r.db.WithContext(ctx).Preload("Category").Preload("Brand").Where("slug = ?", slug).First(&productModel).Error; err != nil {
		return nil, err
	}
	return productModel.ToEntity(), nil
}

func (r *PostgresProductRepo) Update(ctx context.Context, id uint, product *entities.Product) (*entities.Product, error) {
	var productModel models.ProductModel

	// First, get the existing product
	if err := r.db.WithContext(ctx).First(&productModel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("product not found")
		}
		return nil, err
	}

	// Preserve the existing slug before overwriting with entity data
	existingSlug := productModel.Slug

	// Update fields from entity
	productModel.FromEntity(product)
	productModel.ID = id // Ensure ID is preserved

	// If no slug provided, keep the existing one or generate a unique one from the name
	if productModel.Slug == "" {
		candidate := generateSlug(productModel.Name)
		// Check if the candidate slug is already used by a different product
		var conflict models.ProductModel
		err := r.db.WithContext(ctx).Where("slug = ? AND id != ?", candidate, id).First(&conflict).Error
		if err == nil {
			// Conflict with another product — append the ID to make it unique
			productModel.Slug = fmt.Sprintf("%s-%d", candidate, id)
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			productModel.Slug = candidate
		} else {
			return nil, err
		}
	}

	// If slug still ended up empty for any reason, fall back to the existing one
	if productModel.Slug == "" {
		productModel.Slug = existingSlug
	}

	if err := r.db.WithContext(ctx).Save(&productModel).Error; err != nil {
		return nil, err
	}

	return productModel.ToEntity(), nil
}

func (r *PostgresProductRepo) List(ctx context.Context, limit, offset int) ([]*entities.Product, error) {
	var productModels []models.ProductModel
	query := r.db.WithContext(ctx).Where("deleted_at IS NULL AND status >= 1").Preload("Category").Preload("Brand").Order("priority DESC, id DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&productModels).Error; err != nil {
		return nil, err
	}

	products := make([]*entities.Product, len(productModels))
	for i, model := range productModels {
		products[i] = model.ToEntity()
	}

	return products, nil
}

func (r *PostgresProductRepo) Search(ctx context.Context, query string, limit, offset int) ([]*entities.Product, error) {
	var productModels []models.ProductModel
	dbQuery := r.db.WithContext(ctx).Where("deleted_at IS NULL AND status >= 1").Preload("Category").Preload("Brand")

	if query != "" {
		searchTerm := "%" + query + "%"
		dbQuery = dbQuery.Where("name ILIKE ? OR description ILIKE ?", searchTerm, searchTerm)
	}

	if limit > 0 {
		dbQuery = dbQuery.Limit(limit)
	}
	if offset > 0 {
		dbQuery = dbQuery.Offset(offset)
	}

	if err := dbQuery.Find(&productModels).Error; err != nil {
		return nil, err
	}

	products := make([]*entities.Product, len(productModels))
	for i, model := range productModels {
		products[i] = model.ToEntity()
	}

	return products, nil
}

func (r *PostgresProductRepo) GetPopular(ctx context.Context, limit int) ([]*entities.Product, error) {
	var productModels []models.ProductModel
	query := r.db.WithContext(ctx).Preload("Category").Preload("Brand").Where("deleted_at IS NULL AND status >= 1").Order("views_count DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&productModels).Error; err != nil {
		return nil, err
	}

	products := make([]*entities.Product, len(productModels))
	for i, model := range productModels {
		products[i] = model.ToEntity()
	}

	return products, nil
}

func (r *PostgresProductRepo) GetByCategory(ctx context.Context, categoryID uint, limit, offset int) ([]*entities.Product, error) {
	var productModels []models.ProductModel
	query := r.db.WithContext(ctx).Where("deleted_at IS NULL AND status >= 1 AND category_id = ?", categoryID).Preload("Category").Preload("Brand")

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&productModels).Error; err != nil {
		return nil, err
	}

	products := make([]*entities.Product, len(productModels))
	for i, model := range productModels {
		products[i] = model.ToEntity()
	}

	return products, nil
}

func (r *PostgresProductRepo) GetByBrand(ctx context.Context, brandID uint, limit, offset int) ([]*entities.Product, error) {
	var productModels []models.ProductModel
	query := r.db.WithContext(ctx).Where("deleted_at IS NULL AND status >= 1 AND brand_id = ?", brandID).Preload("Category").Preload("Brand")

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&productModels).Error; err != nil {
		return nil, err
	}

	products := make([]*entities.Product, len(productModels))
	for i, model := range productModels {
		products[i] = model.ToEntity()
	}

	return products, nil
}

func (r *PostgresProductRepo) GetSimilarProducts(ctx context.Context, product *entities.Product, limit int) ([]*entities.Product, error) {
	var productModels []models.ProductModel

	if product.CategoryID == nil {
		return nil, nil // No category to match against
	}

	// Determine representative price from start/end for similarity calculation
	var centerPrice float64
	if product.StartPrice != nil {
		centerPrice = *product.StartPrice
	} else if product.EndPrice != nil {
		centerPrice = *product.EndPrice
	} else {
		centerPrice = product.Price
	}
	// Calculate price range (+/- 10%)
	minPrice := centerPrice * 0.9
	maxPrice := centerPrice * 1.1

	query := r.db.WithContext(ctx).
		Preload("Category").Preload("Brand").
		Where("deleted_at IS NULL AND status >= 1").
		Where("category_id = ?", *product.CategoryID).
		Where("id != ?", product.ID).
		Where("COALESCE(start_price, end_price, 0) BETWEEN ? AND ?", minPrice, maxPrice)

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&productModels).Error; err != nil {
		return nil, err
	}

	// Fetch average ratings for all similar products (OPTIMIZED: single query instead of N queries)
	if len(productModels) > 0 {
		productIDs := make([]uint, len(productModels))
		for i, model := range productModels {
			productIDs[i] = model.ID
		}

		// Query to get average ratings
		type RatingResult struct {
			ProductID     uint     `gorm:"column:product_id"`
			AverageRating *float64 `gorm:"column:average_rating"`
		}
		var ratings []RatingResult

		// Use Table().Select().Where().Group().Scan() approach for better GORM support
		err := r.db.WithContext(ctx).
			Table("product_reviews").
			Select(
				"product_id",
				"AVG(CAST(NULLIF(rating,'') AS NUMERIC)) as average_rating",
			).
			Where("product_id IN ? AND deleted_at IS NULL AND rating IS NOT NULL AND rating != ''", productIDs).
			Group("product_id").
			Scan(&ratings).Error

		if err == nil && len(ratings) > 0 {
			// Create a map for quick lookup
			ratingsMap := make(map[uint]*float64)
			for _, rating := range ratings {
				if rating.AverageRating != nil {
					ratingsMap[rating.ProductID] = rating.AverageRating
				}
			}

			// Assign ratings to products
			for i := range productModels {
				if rating, exists := ratingsMap[productModels[i].ID]; exists {
					productModels[i].AverageRating = rating
				}
			}
		}
	}

	products := make([]*entities.Product, len(productModels))
	for i, model := range productModels {
		products[i] = model.ToEntity()
	}

	return products, nil
}

func (r *PostgresProductRepo) IncrementViews(ctx context.Context, id uint) error {
	// First check if the product exists
	var productModel models.ProductModel
	if err := r.db.WithContext(ctx).Select("id").First(&productModel, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("product not found")
		}
		return err
	}

	// If product exists, increment the views count
	return r.db.WithContext(ctx).Model(&models.ProductModel{}).Where("id = ?", id).
		UpdateColumn("views_count", gorm.Expr("views_count + 1")).Error
}

func (r *PostgresProductRepo) Count(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.ProductModel{}).Where("deleted_at IS NULL AND status >= 1").Count(&count).Error
	return count, err
}

func (r *PostgresProductRepo) CountByCategory(ctx context.Context, categoryID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.ProductModel{}).
		Where("deleted_at IS NULL AND status >= 1 AND category_id = ?", categoryID).Count(&count).Error
	return count, err
}

func (r *PostgresProductRepo) CountByBrand(ctx context.Context, brandID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.ProductModel{}).
		Where("deleted_at IS NULL AND status >= 1 AND brand_id = ?", brandID).Count(&count).Error
	return count, err
}

// Translation methods
func (r *PostgresProductRepo) CreateTranslation(ctx context.Context, translation *entities.ProductTranslation) (*entities.ProductTranslation, error) {
	var translationModel models.ProductTranslationModel
	translationModel.FromEntity(translation)

	// Debug: Log what's being sent to database
	fmt.Printf("Repository CreateTranslation - Model before DB: ProductID=%d, Locale='%s', TranslatedName='%s', StartPrice=%v, EndPrice=%v\n",
		translationModel.ProductID, translationModel.Locale, translationModel.TranslatedName, translationModel.StartPrice, translationModel.EndPrice)

	// Validate that the translated_name field is not empty before database call
	if translationModel.TranslatedName == "" {
		fmt.Printf("ERROR: Model.TranslatedName is empty before database call!\n")
		return nil, fmt.Errorf("translated_name field cannot be empty")
	}

	// Create using raw SQL to ensure proper handling of nullable columns
	if err := r.db.WithContext(ctx).Create(&translationModel).Error; err != nil {
		fmt.Printf("ERROR creating translation: %v\n", err)
		return nil, err
	}

	fmt.Printf("Translation created successfully in database with ID: %d, StartPrice=%v, EndPrice=%v\n",
		translationModel.ID, translationModel.StartPrice, translationModel.EndPrice)
	return translationModel.ToEntity(), nil
}

func (r *PostgresProductRepo) GetTranslations(ctx context.Context, productID uint) ([]*entities.ProductTranslation, error) {
	var translationModels []models.ProductTranslationModel

	if err := r.db.WithContext(ctx).
		Where("product_id = ?", productID).
		Find(&translationModels).Error; err != nil {
		return nil, err
	}

	translations := make([]*entities.ProductTranslation, len(translationModels))
	for i, model := range translationModels {
		translations[i] = model.ToEntity()
	}

	return translations, nil
}

func (r *PostgresProductRepo) GetTranslationByLocale(ctx context.Context, productID uint, locale string) (*entities.ProductTranslation, error) {
	var translationModel models.ProductTranslationModel

	if err := r.db.WithContext(ctx).
		Where("product_id = ? AND locale = ?", productID, locale).
		First(&translationModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("translation not found")
		}
		return nil, err
	}

	return translationModel.ToEntity(), nil
}

func (r *PostgresProductRepo) UpdateTranslation(ctx context.Context, translation *entities.ProductTranslation) (*entities.ProductTranslation, error) {
	var translationModel models.ProductTranslationModel
	translationModel.FromEntity(translation)

	fmt.Printf("Repository UpdateTranslation - Updating ID=%d with StartPrice=%v, EndPrice=%v\n",
		translationModel.ID, translationModel.StartPrice, translationModel.EndPrice)

	// Use raw SQL to ensure nullable columns are properly updated (including NULLs)
	// This bypasses GORM's default behavior of skipping nil/zero values
	sql := `UPDATE product_translations 
	        SET translated_name = ?, 
	            start_price = ?, 
	            end_price = ?, 
	            updated_at = ? 
	        WHERE id = ?`

	if err := r.db.WithContext(ctx).Exec(sql,
		translationModel.TranslatedName,
		translationModel.StartPrice,
		translationModel.EndPrice,
		translationModel.UpdatedAt,
		translationModel.ID,
	).Error; err != nil {
		fmt.Printf("ERROR updating translation via raw SQL: %v\n", err)
		return nil, err
	}

	// Reload to get the latest data from database
	if err := r.db.WithContext(ctx).Where("id = ?", translationModel.ID).First(&translationModel).Error; err != nil {
		fmt.Printf("ERROR reloading translation after update: %v\n", err)
		return nil, err
	}

	fmt.Printf("Translation updated successfully - StartPrice=%v, EndPrice=%v\n",
		translationModel.StartPrice, translationModel.EndPrice)
	return translationModel.ToEntity(), nil
}

// GetWithFilters implements Laravel-compatible filtering for products
func (r *PostgresProductRepo) GetWithFilters(ctx context.Context, filters *repository.ProductFilters) ([]*entities.Product, int64, error) {
	var productModels []models.ProductModel
	var totalCount int64

	// Start building the query (only include non-deleted, active products by default)
	query := r.db.WithContext(ctx).Model(&models.ProductModel{}).
		Preload("Category").
		Preload("Brand").
		Where("products.deleted_at IS NULL AND products.status >= 1")

	// Apply filters (this will handle category/brand ID conversion from slugs)
	query = r.applyFilters(query, filters)

	// Get total count for pagination
	if err := query.Count(&totalCount).Error; err != nil {
		return nil, 0, err
	}

	// Debug logging
	fmt.Printf("[GetWithFilters] Total count: %d for category: '%s', page: %d, limit: %d\n", totalCount, filters.CategorySlug, filters.Page, filters.Limit)

	// Apply sorting
	query = r.applySorting(query, filters.SortBy)

	// Apply pagination
	offset := (filters.Page - 1) * filters.Limit
	if err := query.Limit(filters.Limit).Offset(offset).Find(&productModels).Error; err != nil {
		return nil, 0, err
	}

	// Fetch average ratings for all products in this batch (OPTIMIZED: single query instead of N queries)
	if len(productModels) > 0 {
		productIDs := make([]uint, len(productModels))
		for i, model := range productModels {
			productIDs[i] = model.ID
		}

		fmt.Printf("[DEBUG] Fetching ratings for product IDs: %v\n", productIDs)

		// Query to get average ratings
		type RatingResult struct {
			ProductID     uint     `gorm:"column:product_id"`
			AverageRating *float64 `gorm:"column:average_rating"`
		}
		var ratings []RatingResult

		// Use Table().Select().Where().Group().Scan() approach for better GORM support
		err := r.db.WithContext(ctx).
			Table("product_reviews").
			Select(
				"product_id",
				"AVG(CAST(NULLIF(rating,'') AS NUMERIC)) as average_rating",
			).
			Where("product_id IN ? AND deleted_at IS NULL AND rating IS NOT NULL AND rating != ''", productIDs).
			Group("product_id").
			Scan(&ratings).Error

		fmt.Printf("[DEBUG] Rating query executed - Error: %v, Results count: %d\n", err, len(ratings))
		if len(ratings) > 0 {
			fmt.Printf("[DEBUG] First result: ProductID=%d, Rating=%v\n", ratings[0].ProductID, ratings[0].AverageRating)
		}

		if err == nil && len(ratings) > 0 {
			// Create a map for quick lookup
			ratingsMap := make(map[uint]*float64)
			for _, rating := range ratings {
				if rating.AverageRating != nil {
					ratingsMap[rating.ProductID] = rating.AverageRating
					fmt.Printf("[DEBUG] Mapped Product %d -> Rating: %.2f\n", rating.ProductID, *rating.AverageRating)
				}
			}

			// Assign ratings to products
			for i := range productModels {
				if rating, exists := ratingsMap[productModels[i].ID]; exists {
					productModels[i].AverageRating = rating
				}
			}
		} else {
			fmt.Printf("[DEBUG] No ratings found. Query error was: %v\n", err)
		}
	}

	// Convert models to entities
	products := make([]*entities.Product, len(productModels))
	for i, model := range productModels {
		products[i] = model.ToEntity()
	}

	return products, totalCount, nil
}

// applyFilters applies all filters to the query and returns the modified query
// IMPORTANT: GORM's Where/Joins methods return a new *gorm.DB, so we must reassign
// OPTIMIZATION: Convert category/brand slugs to IDs first to avoid JOINs that interfere with Preload
func (r *PostgresProductRepo) applyFilters(query *gorm.DB, filters *repository.ProductFilters) *gorm.DB {
	// Search term filter
	if filters.SearchTerm != "" {
		query = query.Where("products.name ILIKE ?", "%"+filters.SearchTerm+"%")
	}

	// Category filter - convert slug to ID to avoid JOIN (better for preloading)
	if filters.CategorySlug != "" {
		categoryValue := filters.CategorySlug
		if isNumeric(categoryValue) {
			// It's already an ID - use directly
			query = query.Where("products.category_id = ?", categoryValue)
		} else {
			// It's a slug - fetch the category ID from slug to avoid JOIN
			// Use LOWER() for case-insensitive slug matching
			var category models.CategoryModel
			if err := r.db.WithContext(query.Statement.Context).
				Where("LOWER(slug) = LOWER(?)", categoryValue).
				First(&category).Error; err == nil {
				// Successfully found category, filter by its ID
				query = query.Where("products.category_id = ?", category.ID)
				fmt.Printf("[applyFilters] Found category '%s' with ID %d for slug '%s'\n", category.Name, category.ID, categoryValue)
			} else {
				// Category not found - log warning
				fmt.Printf("[applyFilters] WARNING: Category slug '%s' not found in database - no results will be returned\n", categoryValue)
			}
		}
	}

	// Brand filter (multiple brands supported)
	// Optimize: Convert slugs to IDs first to avoid JOIN
	if len(filters.BrandSlugs) > 0 {
		var brandIDs []uint
		var brands []models.BrandModel

		// Fetch brand IDs by slugs efficiently (single query instead of per-product queries)
		if err := r.db.WithContext(query.Statement.Context).
			Where("slug IN ?", filters.BrandSlugs).
			Select("id").
			Find(&brands).Error; err == nil && len(brands) > 0 {
			for _, b := range brands {
				brandIDs = append(brandIDs, b.ID)
			}
			if len(brandIDs) > 0 {
				query = query.Where("products.brand_id IN ?", brandIDs)
			}
		}
	}

	// Price range filter - use COALESCE of start/end price for comparisons
	if filters.MinPrice != nil {
		query = query.Where("COALESCE(products.start_price, products.end_price, 0) >= ?", *filters.MinPrice)
	}
	if filters.MaxPrice != nil {
		query = query.Where("COALESCE(products.start_price, products.end_price, 0) <= ?", *filters.MaxPrice)
	}

	return query
}

// isNumeric checks if a string represents a numeric value
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// applySorting applies sorting based on sortby parameter and returns the modified query
func (r *PostgresProductRepo) applySorting(query *gorm.DB, sortBy string) *gorm.DB {
	switch sortBy {
	case "popular":
		query = query.Order("views_count DESC, priority DESC, updated_at DESC")
	case "price_asc":
		query = query.Order("priority DESC, COALESCE(start_price, end_price) ASC, updated_at DESC")
	case "price_desc":
		query = query.Order("priority DESC, COALESCE(start_price, end_price) DESC, updated_at DESC")
	case "priority":
		// Priority with updated_at as secondary sort for "Latest Reviews"
		query = query.Order("priority DESC, updated_at DESC")
	default:
		// Default sorting by priority descending, then latest updates
		query = query.Order("priority DESC, updated_at DESC")
	}
	return query
}

// DeleteTranslation deletes a product translation by its ID
func (r *PostgresProductRepo) DeleteTranslation(ctx context.Context, translationID uint) error {
	err := r.db.WithContext(ctx).Delete(&models.ProductTranslationModel{}, translationID).Error
	if err != nil {
		return fmt.Errorf("failed to delete product translation: %w", err)
	}
	return nil
}

// GetTranslatedNamesByProductIDs batch-fetches translated names for a list of product IDs.
// Returns a map of productID -> translatedName for the given locale.
func (r *PostgresProductRepo) GetTranslatedNamesByProductIDs(ctx context.Context, productIDs []uint, locale string) (map[uint]string, error) {
	if len(productIDs) == 0 || locale == "" {
		return map[uint]string{}, nil
	}

	var rows []struct {
		ProductID      uint   `gorm:"column:product_id"`
		TranslatedName string `gorm:"column:translated_name"`
	}

	if err := r.db.WithContext(ctx).
		Table("product_translations").
		Select("product_id, translated_name").
		Where("product_id IN ? AND locale = ?", productIDs, locale).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to batch-fetch translations: %w", err)
	}

	result := make(map[uint]string, len(rows))
	for _, row := range rows {
		if row.TranslatedName != "" {
			result[row.ProductID] = row.TranslatedName
		}
	}
	return result, nil
}

var _ repository.ProductRepository = (*PostgresProductRepo)(nil)
