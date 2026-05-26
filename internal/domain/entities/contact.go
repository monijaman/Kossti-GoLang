package entities

import (
	"time"

	"gorm.io/gorm"
)

// Contact represents a contact form submission from users
type Contact struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Name      string         `gorm:"type:varchar(255);not null" json:"name"`
	Email     string         `gorm:"type:varchar(255);not null" json:"email"`
	Subject   string         `gorm:"type:varchar(500);not null" json:"subject"`
	Message   string         `gorm:"type:text;not null" json:"message"`
	IPAddress string         `gorm:"type:varchar(45)" json:"ip_address"`
	UserAgent string         `gorm:"type:text" json:"user_agent"`
	Status    string         `gorm:"type:varchar(50);default:'new'" json:"status"` // new, read, replied, archived
	AdminNote string         `gorm:"type:text" json:"admin_note,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName returns the table name for Contact entity
func (Contact) TableName() string {
	return "contacts"
}
