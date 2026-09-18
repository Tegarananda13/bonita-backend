package models

import (
	"time"

)

type Customer struct {
	NIK            string    `gorm:"primaryKey;column:nik;not null;size:16"`
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
