package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StatusPembayaran Invoice
const (
	InvoiceStatusBelumBayar = "belum"
	InvoiceStatusDP         = "dp"
	InvoiceStatusBelumLunas = "belum_lunas"
	InvoiceStatusLunas      = "lunas"
)

type Invoice struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	NomorInvoice     string    `gorm:"unique;not null"`
	TotalOrang       int       `gorm:"default:1"`
	TotalTagihan     float64   // Harga Paket × TotalOrang, dihitung saat dibuat
	TotalPembayaran  float64   `gorm:"default:0"` // Jumlah yang sudah diterima
	StatusPembayaran string    `gorm:"default:'belum'"`
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// Relasi
	Pendaftaran []Pendaftaran `gorm:"foreignKey:InvoiceID"`
	Pembayaran  []Pembayaran  `gorm:"foreignKey:InvoiceID"`
}

func (i *Invoice) BeforeCreate(tx *gorm.DB) (err error) {
	i.ID = uuid.New()
	return
}
