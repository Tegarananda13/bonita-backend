package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Pendaftaran struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primaryKey"`
	NomorPendaftaran   string     `gorm:"unique"`
	CustomerID         uuid.UUID
	PaketID            uuid.UUID
	UserID             *uuid.UUID
	InvoiceID          *uuid.UUID // FK ke Invoice (nullable selama migration)
	DocumentStatus     string
	Status             string
	RegistrationSource string     `gorm:"default:'customer'"` // "customer" | "admin" | "chatbot"
	RegisteredBy       string     `gorm:"default:'Self'"`     // "Self" | nama admin | "AI Chatbot"
	TanggalDaftar      time.Time
	BatasWaktuDP       time.Time  // deadline untuk pembayaran DP (24 jam dari TanggalDaftar)

	// Relasi
	Customer Customer   `gorm:"foreignKey:CustomerID"`
	Paket    PaketUmroh `gorm:"foreignKey:PaketID"`
	User     User       `gorm:"foreignKey:UserID"`
	Invoice  Invoice    `gorm:"foreignKey:InvoiceID"`
}

func (p *Pendaftaran) BeforeCreate(tx *gorm.DB) (err error) {
	p.ID = uuid.New()
	return
}
