package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CustomerSession struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"`
	PendaftaranID uuid.UUID
	Token         string
	ExpiredAt     time.Time
	CreatedAt     time.Time

	// Relasi
	Pendaftaran Pendaftaran `gorm:"foreignKey:PendaftaranID"`
}

func (c *CustomerSession) BeforeCreate(tx *gorm.DB) (err error) {
	c.ID = uuid.New()
	return
}