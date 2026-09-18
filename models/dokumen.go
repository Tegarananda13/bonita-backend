package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Dokumen struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	NomorPendaftaran string    `gorm:"column:nomor_pendaftaran;not null"` // FK ke pendaftaran.nomor_pendaftaran
	JenisDokumen     string
	FilePath         string
	StatusValidasi   string
	CreatedAt        time.Time

	// Relasi — constraint:false agar GORM tidak auto-create FK (sudah dibuat manual)
	Pendaftaran Pendaftaran `gorm:"foreignKey:NomorPendaftaran;references:NomorPendaftaran;constraint:false" json:"-"`
}

func (d *Dokumen) BeforeCreate(tx *gorm.DB) (err error) {
	d.ID = uuid.New()
	return
}