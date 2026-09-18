package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type VerifikasiOTP struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	NomorPendaftaran string    `gorm:"column:nomor_pendaftaran;not null"` // FK ke pendaftaran.nomor_pendaftaran
	KodeOTP          string
	ExpiredAt        time.Time
	IsUsed           bool
	CreatedAt        time.Time

	// Relasi — constraint:false agar GORM tidak auto-create FK (sudah dibuat manual)
	Pendaftaran Pendaftaran `gorm:"foreignKey:NomorPendaftaran;references:NomorPendaftaran;constraint:false"`
}

func (v *VerifikasiOTP) BeforeCreate(tx *gorm.DB) (err error) {
	v.ID = uuid.New()
	return
}