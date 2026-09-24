package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FotoPaket menyimpan satu foto untuk satu PaketUmroh.
// Satu paket bisa memiliki banyak foto (1:N).
// Urutan menentukan urutan tampil (ASC).
// IsUtama menandai foto cover/utama — hanya boleh ada satu per paket.
type FotoPaket struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey"                  json:"id"`
	PaketID   uuid.UUID  `gorm:"type:uuid;not null;index"              json:"paket_id"`
	FilePath  string     `gorm:"not null"                              json:"file_path"`
	Urutan    int        `gorm:"default:0"                             json:"urutan"`
	IsUtama   bool       `gorm:"default:false"                         json:"is_utama"`
	CreatedAt time.Time  `                                              json:"created_at"`

	// relasi ke parent — tidak di-serialise ke JSON
	Paket PaketUmroh `gorm:"foreignKey:PaketID;references:ID;constraint:OnDelete:CASCADE" json:"-"`
}

func (f *FotoPaket) BeforeCreate(tx *gorm.DB) error {
	f.ID = uuid.New()
	return nil
}
