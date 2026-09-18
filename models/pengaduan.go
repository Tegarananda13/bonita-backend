package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Pengaduan struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	NomorPendaftaran string    `gorm:"column:nomor_pendaftaran;not null"` // FK ke pendaftaran.nomor_pendaftaran
	// Relasi — constraint:false agar GORM tidak auto-create FK (sudah dibuat manual)
	Pendaftaran      Pendaftaran `gorm:"foreignKey:NomorPendaftaran;references:NomorPendaftaran;constraint:false"`
	Judul            string    `gorm:"not null"`
	IsiPengaduan     string    `gorm:"type:text;not null"`
	Kategori         string
	Status           string    `gorm:"default:'menunggu'"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (p *Pengaduan) BeforeCreate(tx *gorm.DB) (err error) {
	p.ID = uuid.New()
	return
}
