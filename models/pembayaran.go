package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Pembayaran struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	InvoiceID       uuid.UUID // FK ke Invoice
	Jumlah          float64
	TanggalBayar    time.Time
	BuktiPembayaran string
	Status          string

	// Relasi
	Invoice Invoice `gorm:"foreignKey:InvoiceID" json:"-"`
}

func (p *Pembayaran) BeforeCreate(tx *gorm.DB) (err error) {
	p.ID = uuid.New()
	return
}
