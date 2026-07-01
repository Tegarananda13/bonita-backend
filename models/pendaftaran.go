package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Pendaftaran struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey"`
	NomorPendaftaran   string    `gorm:"unique"`
	CustomerID         uuid.UUID
	PaketID            uuid.UUID
	UserID             *uuid.UUID
	NomorInvoice       string    `gorm:"default:''"`
	PaymentStatus      string
	DocumentStatus     string
	Status             string
	TanggalDaftar      time.Time

	// Relasi
	Customer Customer   `gorm:"foreignKey:CustomerID"`
	Paket    PaketUmroh `gorm:"foreignKey:PaketID"`
	User     User       `gorm:"foreignKey:UserID"`
}

func (p *Pendaftaran) BeforeCreate(tx *gorm.DB) (err error) {
	p.ID = uuid.New()
	return
}