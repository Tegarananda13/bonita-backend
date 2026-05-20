package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type VerifikasiOTP struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	PendaftaranID   uuid.UUID
	KodeOTP         string
	ExpiredAt       time.Time
	IsUsed          bool
	CreatedAt       time.Time

	// Relasi
	Pendaftaran Pendaftaran `gorm:"foreignKey:PendaftaranID"`
}

func (v *VerifikasiOTP) BeforeCreate(tx *gorm.DB) (err error) {
	v.ID = uuid.New()
	return
}