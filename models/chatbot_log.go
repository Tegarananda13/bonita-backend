package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ChatbotLog struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey"`
	// Phase 8A: CustomerID sudah dihapus dari DB.
	CustomerNIK *string    `gorm:"column:customer_nik"` // FK ke customer.nik (nullable)
	Pertanyaan  string
	Jawaban     string
	CreatedAt   time.Time

	// Relasi — CustomerNIK sebagai FK (nullable, opsional)
	Customer Customer `gorm:"foreignKey:CustomerNIK;references:NIK"`
}

func (c *ChatbotLog) BeforeCreate(tx *gorm.DB) (err error) {
	c.ID = uuid.New()
	return
}