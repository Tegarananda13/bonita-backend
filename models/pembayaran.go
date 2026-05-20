package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Pembayaran struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	PendaftaranID   uuid.UUID
	Jumlah          float64
	TanggalBayar    time.Time
	BuktiPembayaran string
	Status          string

	// Relasi
	Pendaftaran Pendaftaran `gorm:"foreignKey:PendaftaranID"`
}

func (p *Pembayaran) BeforeCreate(tx *gorm.DB) (err error) {
	p.ID = uuid.New()
	return
}