package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type DetailFasilitas struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	PaketID        uuid.UUID `gorm:"type:uuid;not null"`
	NamaFasilitas  string
	Deskripsi      string
	CreatedAt      time.Time

	// relasi
	Paket         PaketUmroh      `json:"-"`
	FotoFasilitas []FotoFasilitas `gorm:"foreignKey:DetailFasilitasID;references:ID" json:"foto_fasilitas,omitempty"`
}

func (d *DetailFasilitas) BeforeCreate(tx *gorm.DB) (err error) {
	d.ID = uuid.New()
	return
}