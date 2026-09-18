package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Pembayaran struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	NomorInvoice    string    `gorm:"column:nomor_invoice;not null"` // FK ke Invoice.nomor_invoice (Phase 6C)
	Jumlah          float64
	TanggalBayar    time.Time
	BuktiPembayaran string
	Status          string

	// Relasi
	Invoice Invoice `gorm:"foreignKey:NomorInvoice;references:NomorInvoice" json:"-"`
}

func (p *Pembayaran) BeforeCreate(tx *gorm.DB) (err error) {
	p.ID = uuid.New()
	return
}
