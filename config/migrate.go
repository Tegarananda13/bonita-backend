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

	// Step 7: Backfill registration_source dan registered_by untuk data lama.
	// Idempoten — hanya mengisi baris yang masih kosong.
	DB.Exec(`
		UPDATE pendaftaran
		SET registration_source = 'customer',
		    registered_by = 'Self'
		WHERE registration_source IS NULL OR registration_source = ''
	`)

	// Step 8: Backfill is_active untuk data paket lama.
	// AutoMigrate menambahkan kolom dengan DEFAULT true, tapi baris lama
	// mungkin NULL jika DB-nya tidak mendukung DEFAULT saat ALTER TABLE.
	DB.Exec(`
		UPDATE paket_umroh
		SET is_active = true
		WHERE is_active IS NULL
	`)

	// Step 9: Backfill is_finished untuk data paket lama.
	DB.Exec(`
		UPDATE paket_umroh
		SET is_finished = false
		WHERE is_finished IS NULL
	`)

	// Step 10: Backfill batas_waktu_dp untuk data pendaftaran lama.
	// Data lama tidak memiliki deadline, set ke tanggal_daftar + 24 jam.
	DB.Exec(`
		UPDATE pendaftaran
		SET batas_waktu_dp = tanggal_daftar + INTERVAL '24 hours'
		WHERE batas_waktu_dp IS NULL OR batas_waktu_dp = '0001-01-01 00:00:00'
	`)
}
