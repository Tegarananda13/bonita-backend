package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Dokumen struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	PendaftaranID   uuid.UUID
	JenisDokumen    string
	FilePath        string
	StatusValidasi  string
	CreatedAt time.Time

	// Relasi
	Pendaftaran Pendaftaran `gorm:"foreignKey:PendaftaranID"`
}

func (d *Dokumen) BeforeCreate(tx *gorm.DB) (err error) {
	d.ID = uuid.New()
	return
}