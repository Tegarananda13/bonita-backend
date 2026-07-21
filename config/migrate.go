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
		&models.Pengaduan{},
	)

	// Step 2: Isi kolom NIK yang kosong dengan nilai placeholder unik
	// agar data lama tidak melanggar constraint UNIQUE ketika diset NOT NULL
	DB.Exec(`
		UPDATE customer
		SET nik = 'OLD-' || LEFT(REPLACE(gen_random_uuid()::text, '-', ''), 12)
		WHERE nik IS NULL OR nik = ''
	`)

	// Step 3: Salin data alamat lama ke alamat_lengkap
	// agar data customer yang sudah ada tidak hilang setelah migration
	DB.Exec(`
		UPDATE customer
		SET alamat_lengkap = alamat
		WHERE (alamat_lengkap IS NULL OR alamat_lengkap = '')
		  AND alamat IS NOT NULL AND alamat != ''
	`)
}