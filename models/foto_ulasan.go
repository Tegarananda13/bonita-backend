package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FotoUlasan struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UlasanID  uuid.UUID `gorm:"type:uuid;not null;index" json:"ulasan_id"`
	FilePath  string    `gorm:"not null" json:"file_path"` // Public URL / file path di Supabase Storage bucket ulasan
	CreatedAt time.Time `json:"created_at"`

	// Relasi ke parent
	Ulasan *Ulasan `gorm:"foreignKey:UlasanID;references:ID;constraint:false" json:"-"`
}

func (f *FotoUlasan) BeforeCreate(tx *gorm.DB) (err error) {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return
}
