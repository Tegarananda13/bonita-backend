package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Pengaduan struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	PendaftaranID  uuid.UUID `gorm:"type:uuid;not null;index"`
	Pendaftaran    Pendaftaran
	Judul          string    `gorm:"not null"`
	IsiPengaduan   string    `gorm:"type:text;not null"`
	Kategori       string
	Status         string    `gorm:"default:'menunggu'"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (p *Pengaduan) BeforeCreate(tx *gorm.DB) (err error) {
	p.ID = uuid.New()
	return
}
