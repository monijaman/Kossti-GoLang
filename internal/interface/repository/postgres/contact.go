package postgres

import (
	"kossti/internal/domain/repository"
	postgresqlRepo "kossti/internal/infrastructure/repository/postgresql"

	"gorm.io/gorm"
)

// NewContactRepository creates a new PostgreSQL contact repository
func NewContactRepository(db *gorm.DB) repository.ContactRepository {
	return postgresqlRepo.NewContactRepository(db)
}
