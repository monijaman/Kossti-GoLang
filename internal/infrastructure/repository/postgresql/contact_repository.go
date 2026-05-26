// Package postgresql provides PostgreSQL-specific implementations of repository interfaces.
// This package is part of the infrastructure layer in Clean Architecture.
// It contains concrete implementations that interact with PostgreSQL database using GORM.
package postgresql

import (
	"context"
	"kossti/internal/domain/entities"
	"kossti/internal/domain/repository"
	models "kossti/internal/infrastructure/database/models"

	"gorm.io/gorm"
)

// contactRepository implements the ContactRepository interface using PostgreSQL
type contactRepository struct {
	db *gorm.DB
}

// NewContactRepository creates a new PostgreSQL contact repository
func NewContactRepository(db *gorm.DB) repository.ContactRepository {
	return &contactRepository{
		db: db,
	}
}

// Create creates a new contact form submission
func (r *contactRepository) Create(ctx context.Context, contact *entities.Contact) (*entities.Contact, error) {
	model := &models.ContactModel{}
	model.FromEntity(contact)

	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return nil, err
	}

	return model.ToEntity(), nil
}

// GetByID retrieves a contact by ID
func (r *contactRepository) GetByID(ctx context.Context, id uint) (*entities.Contact, error) {
	var model models.ContactModel
	if err := r.db.WithContext(ctx).First(&model, id).Error; err != nil {
		return nil, err
	}
	return model.ToEntity(), nil
}

// GetAll retrieves all contacts with pagination
func (r *contactRepository) GetAll(ctx context.Context, limit, offset int) ([]*entities.Contact, int64, error) {
	var contactModels []models.ContactModel
	var total int64

	// Get total count
	if err := r.db.WithContext(ctx).Model(&models.ContactModel{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated results ordered by created_at desc
	if err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&contactModels).Error; err != nil {
		return nil, 0, err
	}

	contacts := make([]*entities.Contact, len(contactModels))
	for i, model := range contactModels {
		contacts[i] = model.ToEntity()
	}
	return contacts, total, nil
}

// Update updates an existing contact
func (r *contactRepository) Update(ctx context.Context, id uint, contact *entities.Contact) (*entities.Contact, error) {
	model := &models.ContactModel{}
	model.FromEntity(contact)
	model.ID = id

	if err := r.db.WithContext(ctx).Model(&model).Updates(model).Error; err != nil {
		return nil, err
	}

	return model.ToEntity(), nil
}

// Delete soft deletes a contact
func (r *contactRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&models.ContactModel{}, id).Error
}

// GetByStatus retrieves contacts by status with pagination
func (r *contactRepository) GetByStatus(ctx context.Context, status string, limit, offset int) ([]*entities.Contact, int64, error) {
	var contactModels []models.ContactModel
	var total int64

	// Get total count for this status
	if err := r.db.WithContext(ctx).
		Model(&models.ContactModel{}).
		Where("status = ?", status).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated results
	if err := r.db.WithContext(ctx).
		Where("status = ?", status).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&contactModels).Error; err != nil {
		return nil, 0, err
	}

	contacts := make([]*entities.Contact, len(contactModels))
	for i, model := range contactModels {
		contacts[i] = model.ToEntity()
	}
	return contacts, total, nil
}

// UpdateStatus updates the status and admin note of a contact
func (r *contactRepository) UpdateStatus(ctx context.Context, id uint, status string, adminNote string) error {
	return r.db.WithContext(ctx).
		Model(&models.ContactModel{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     status,
			"admin_note": adminNote,
		}).Error
}

// Count returns total number of contacts
func (r *contactRepository) Count(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.ContactModel{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountByStatus returns number of contacts by status
func (r *contactRepository) CountByStatus(ctx context.Context, status string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&models.ContactModel{}).
		Where("status = ?", status).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
