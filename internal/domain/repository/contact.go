package repository

import (
	"context"
	"kossti/internal/domain/entities"
)

// ContactRepository defines the interface for contact form operations
type ContactRepository interface {
	// Basic CRUD operations
	Create(ctx context.Context, contact *entities.Contact) (*entities.Contact, error)
	GetByID(ctx context.Context, id uint) (*entities.Contact, error)
	GetAll(ctx context.Context, limit, offset int) ([]*entities.Contact, int64, error)
	Update(ctx context.Context, id uint, contact *entities.Contact) (*entities.Contact, error)
	Delete(ctx context.Context, id uint) error

	// Status operations
	GetByStatus(ctx context.Context, status string, limit, offset int) ([]*entities.Contact, int64, error)
	UpdateStatus(ctx context.Context, id uint, status string, adminNote string) error

	// Statistics
	Count(ctx context.Context) (int64, error)
	CountByStatus(ctx context.Context, status string) (int64, error)
}
