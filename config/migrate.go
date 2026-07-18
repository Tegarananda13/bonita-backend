package config

import "bonita-backend/models"

func Migrate() {
	// Step 1: AutoMigrate – tambah kolom baru (nullable dulu untuk data lama)
	DB.AutoMigrate(
		&models.Customer{},
		&models.User{},
		&models.PaketUmroh{},
		&models.Pendaftaran{},
		&models.Dokumen{},
		&models.Pembayaran{},
		&models.VerifikasiOTP{},
		&models.ChatbotLog{},
		&models.CustomerSession{},
		&models.DetailFasilitas{},
	)

	// Step 2: Isi kolom NIK yang kosong dengan nilai placeholder unik
	// agar data lama tidak melanggar constraint UNIQUE ketika diset NOT NULL
	DB.Exec(`
		UPDATE customer
		SET nik = 'OLD-' || LEFT(REPLACE(gen_random_uuid()::text, '-', ''), 12)
		WHERE nik IS NULL OR nik = ''
	`)
}