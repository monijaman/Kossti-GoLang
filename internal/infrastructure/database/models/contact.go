// Package models contains database-specific implementations.
// This package is part of the infrastructure layer in Clean Architecture.
// These models are tightly coupled to GORM and handle database persistence.
package models

import (
	"kossti/internal/domain/entities"
	"time"

	"gorm.io/gorm"
)

// ContactModel represents the database model for contact form submissions (GORM-specific)
type ContactModel struct {
	ID        uint           `gorm:"primaryKey;autoIncrement"`
	Name      string         `gorm:"type:varchar(255);not null"`
	Email     string         `gorm:"type:varchar(255);not null"`
	Subject   string         `gorm:"type:varchar(500);not null"`
	Message   string         `gorm:"type:text;not null"`
	IPAddress string         `gorm:"type:varchar(45)"`
	UserAgent string         `gorm:"type:text"`
	Status    string         `gorm:"type:varchar(50);default:'new'"`
	AdminNote string         `gorm:"type:text"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// TableName specifies the table name for GORM
func (ContactModel) TableName() string {
	return "contacts"
}

// ToEntity converts GORM model to domain entity
func (c *ContactModel) ToEntity() *entities.Contact {
	return &entities.Contact{
		ID:        c.ID,
		Name:      c.Name,
		Email:     c.Email,
		Subject:   c.Subject,
		Message:   c.Message,
		IPAddress: c.IPAddress,
		UserAgent: c.UserAgent,
		Status:    c.Status,
		AdminNote: c.AdminNote,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		DeletedAt: c.DeletedAt,
	}
}

// FromEntity converts domain entity to GORM model
func (c *ContactModel) FromEntity(entity *entities.Contact) {
	c.ID = entity.ID
	c.Name = entity.Name
	c.Email = entity.Email
	c.Subject = entity.Subject
	c.Message = entity.Message
	c.IPAddress = entity.IPAddress
	c.UserAgent = entity.UserAgent
	c.Status = entity.Status
	c.AdminNote = entity.AdminNote
	c.CreatedAt = entity.CreatedAt
	c.UpdatedAt = entity.UpdatedAt
	c.DeletedAt = entity.DeletedAt
}
