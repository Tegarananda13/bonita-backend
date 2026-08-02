package config

import "bonita-backend/models"

func Migrate() {
	// Step 1: AutoMigrate semua model
	// Invoice harus dimigrasi sebelum Pendaftaran & Pembayaran
	// karena Pendaftaran.invoice_id dan Pembayaran.invoice_id adalah FK ke invoices.
	DB.AutoMigrate(
		&models.Invoice{},
	)
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
	DB.Exec(`
		UPDATE customer
		SET nik = 'OLD-' || LEFT(REPLACE(gen_random_uuid()::text, '-', ''), 12)
		WHERE nik IS NULL OR nik = ''
	`)

	// Step 3: Salin data alamat lama ke alamat_lengkap
	DB.Exec(`
		UPDATE customer
		SET alamat_lengkap = alamat
		WHERE (alamat_lengkap IS NULL OR alamat_lengkap = '')
		  AND alamat IS NOT NULL AND alamat != ''
	`)

	// Step 4 & 5 (data migration lama) sudah dihapus karena database
	// sudah fully migrated — semua pendaftaran sudah memiliki invoice_id
	// dan kolom nomor_invoice lama sudah tidak ada di tabel pendaftaran.

	// Step 6: Set invoice_id di tabel pembayaran jika masih ada yang kosong
	// (hanya berjalan jika kolom pendaftaran_id masih ada di tabel pembayaran)
	DB.Exec(`
		UPDATE pembayaran pb
		SET invoice_id = p.invoice_id
		FROM pendaftaran p
		WHERE pb.invoice_id IS NULL
		  AND p.invoice_id IS NOT NULL
		  AND EXISTS (
		    SELECT 1 FROM information_schema.columns
		    WHERE table_name = 'pembayaran' AND column_name = 'pendaftaran_id'
		  )
	`)
}
