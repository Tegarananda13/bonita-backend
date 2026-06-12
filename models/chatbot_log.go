package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ChatbotLog struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey"`
	CustomerID *uuid.UUID `gorm:"type:uuid;null"`
	Pertanyaan string
	Jawaban    string
	CreatedAt  time.Time

	// Relasi
	Customer Customer `gorm:"foreignKey:CustomerID"`
}

func (c *ChatbotLog) BeforeCreate(tx *gorm.DB) (err error) {
	c.ID = uuid.New()
	return
}