package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Customer struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	NIK            string    `gorm:"uniqueIndex"`
	Nama           string
	TempatLahir    string
	TanggalLahir   time.Time
	JenisKelamin   string
	NoHP           string
	Email          string
	AlamatLengkap  string
	Provinsi       string
	KabupatenKota  string
	Kecamatan      string
	KelurahanDesa  string
	KodePos        string
	CreatedAt      time.Time
}

func (c *Customer) BeforeCreate(tx *gorm.DB) (err error) {
	c.ID = uuid.New()
	return
}