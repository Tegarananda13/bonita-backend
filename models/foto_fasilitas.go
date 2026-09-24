package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FotoFasilitas menyimpan satu foto untuk satu DetailFasilitas.
// Satu fasilitas bisa memiliki banyak foto (1:N).
// Urutan menentukan urutan tampil (ASC).
type FotoFasilitas struct {
	ID                uuid.UUID       `gorm:"type:uuid;primaryKey"                                                        json:"id"`
	DetailFasilitasID uuid.UUID       `gorm:"type:uuid;not null;index;column:detail_fasilitas_id"                         json:"detail_fasilitas_id"`
	FilePath          string          `gorm:"not null"                                                                    json:"file_path"`
	Urutan            int             `gorm:"default:0"                                                                   json:"urutan"`
	CreatedAt         time.Time       `                                                                                   json:"created_at"`

	// relasi ke parent — tidak di-serialise ke JSON
	DetailFasilitas DetailFasilitas `gorm:"foreignKey:DetailFasilitasID;references:ID;constraint:OnDelete:CASCADE" json:"-"`
}

func (f *FotoFasilitas) BeforeCreate(tx *gorm.DB) error {
	f.ID = uuid.New()
	return nil
}
