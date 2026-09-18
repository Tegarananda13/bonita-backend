package models

import (
	"time"

	"github.com/google/uuid"
)

type Pendaftaran struct {
	// Phase 7B: NomorPendaftaran adalah Primary Key database dan GORM
	NomorPendaftaran   string     `gorm:"primaryKey;column:nomor_pendaftaran;not null"`
	// Phase 8A: CustomerID sudah dihapus dari DB. Relasi customer via CustomerNIK.
	CustomerNIK        string     `gorm:"column:customer_nik;not null;size:16"` // FK ke customer.nik
	PaketID            uuid.UUID  `gorm:"type:uuid"`
	UserID             *uuid.UUID `gorm:"type:uuid"`
	NomorInvoice       string     `gorm:"column:nomor_invoice;not null"` // FK ke Invoice.nomor_invoice
	DocumentStatus     string
	Status             string
	RegistrationSource string     `gorm:"default:'customer'"` // "customer" | "admin" | "chatbot"
	RegisteredBy       string     `gorm:"default:'Self'"`     // "Self" | nama admin | "AI Chatbot"
	TanggalDaftar      time.Time
	BatasWaktuDP       time.Time  // deadline untuk pembayaran DP

	// Relasi
	Customer Customer   `gorm:"foreignKey:CustomerNIK;references:NIK"`
	Paket    PaketUmroh `gorm:"foreignKey:PaketID"`
	User     User       `gorm:"foreignKey:UserID"`
	Invoice  Invoice    `gorm:"foreignKey:NomorInvoice;references:NomorInvoice"`
}
