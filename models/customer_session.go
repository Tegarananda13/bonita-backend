package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CustomerSession struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	NomorPendaftaran string    `gorm:"column:nomor_pendaftaran;not null"` // FK ke pendaftaran.nomor_pendaftaran
	Token            string
	ExpiredAt        time.Time
	CreatedAt        time.Time

	// Relasi — constraint:false agar GORM tidak auto-create FK (sudah dibuat manual)
	Pendaftaran Pendaftaran `gorm:"foreignKey:NomorPendaftaran;references:NomorPendaftaran;constraint:false"`
}

func (c *CustomerSession) BeforeCreate(tx *gorm.DB) (err error) {
	c.ID = uuid.New()
	return
}