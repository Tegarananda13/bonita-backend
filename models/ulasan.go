package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	UlasanStatusMenunggu  = "menunggu"
	UlasanStatusDisetujui = "disetujui"
	UlasanStatusDitolak   = "ditolak"
)

type Ulasan struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	PendaftaranID string    `gorm:"column:pendaftaran_id;not null;index" json:"pendaftaran_id"` // FK ke pendaftaran.nomor_pendaftaran
	Rating        int       `gorm:"not null" json:"rating"`                             // 1 - 5
	Komentar      string    `gorm:"type:text;not null" json:"komentar"`
	Status        string    `gorm:"default:'menunggu';index" json:"status"`             // menunggu, disetujui, ditolak
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Relasi
	Pendaftaran Pendaftaran  `gorm:"foreignKey:PendaftaranID;references:NomorPendaftaran;constraint:false" json:"pendaftaran,omitempty"`
	FotoUlasan  []FotoUlasan `gorm:"foreignKey:UlasanID;references:ID;constraint:false" json:"foto_ulasan,omitempty"`
}

func (u *Ulasan) BeforeCreate(tx *gorm.DB) (err error) {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	if u.Status == "" {
		u.Status = UlasanStatusDisetujui
	}
	return
}
