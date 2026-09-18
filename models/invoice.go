package models

import (
	"time"
)

// StatusPembayaran Invoice
const (
	InvoiceStatusBelumBayar = "belum"
	InvoiceStatusDP         = "dp"
	InvoiceStatusBelumLunas = "belum_lunas"
	InvoiceStatusLunas      = "lunas"
)

type Invoice struct {
	// Phase 6D: NomorInvoice adalah Primary Key database dan GORM
	NomorInvoice     string    `gorm:"primaryKey;column:nomor_invoice;not null"`
	TotalOrang       int       `gorm:"default:1"`
	TotalTagihan     float64   // Harga Paket × TotalOrang, dihitung saat dibuat
	TotalPembayaran  float64   `gorm:"default:0"` // Jumlah yang sudah diterima
	StatusPembayaran string    `gorm:"default:'belum'"`
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// Relasi via NomorInvoice
	Pendaftaran []Pendaftaran `gorm:"foreignKey:NomorInvoice;references:NomorInvoice"`
	Pembayaran  []Pembayaran  `gorm:"foreignKey:NomorInvoice;references:NomorInvoice"`
}
