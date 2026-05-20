package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PaketUmroh struct {
	ID                uuid.UUID `gorm:"type:uuid;primaryKey"`
	NamaPaket         string
	Harga             float64
	TanggalBerangkat  time.Time
	Durasi            int
	Deskripsi         string
	CreatedAt         time.Time
}

func (p *PaketUmroh) BeforeCreate(tx *gorm.DB) (err error) {
	p.ID = uuid.New()
	return nil
}