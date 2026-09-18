package config

import "bonita-backend/models"

func Migrate() {
	// Step 0: Drop FK constraint lama invoice_id jika masih ada
	// (Phase 6A sudah drop ini; langkah ini idempoten untuk safety)
	DB.Exec(`ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS fk_invoice_pembayaran`)
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS fk_invoice_pendaftaran`)

	// Step 0c (Phase 8A): Drop legacy customer_id dari pendaftaran dan chatbot_log.
	// Idempoten — IF EXISTS mencegah error jika sudah di-drop.
	// Urutan: drop FK dulu, baru drop kolom.
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS fk_pendaftaran_customer`)
	DB.Exec(`ALTER TABLE pendaftaran DROP COLUMN IF EXISTS customer_id`)
	DB.Exec(`ALTER TABLE chatbot_log DROP CONSTRAINT IF EXISTS fk_chatbot_log_customer`)
	DB.Exec(`ALTER TABLE chatbot_log DROP COLUMN IF EXISTS customer_id`)

	// Step 0d (Phase 8B): Pastikan PK customer = nik setelah AutoMigrate.
	// Strategi aman (konsisten dengan Phase 7B + 6D):
	//  1. Lepas FK anak ke customer.nik
	//  2. Drop PK lama (customer.id)
	//  3. AutoMigrate set PK baru via GORM primaryKey tag (nik)
	//  4. ADD PK eksplisit setelah AutoMigrate
	//  5. Restore FK anak
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS fk_pendaftaran_customer_nik`)
	DB.Exec(`ALTER TABLE chatbot_log DROP CONSTRAINT IF EXISTS fk_chatbot_log_customer_nik`)
	DB.Exec(`ALTER TABLE customer DROP CONSTRAINT IF EXISTS customer_pkey`)
	DB.Exec(`ALTER TABLE customer DROP CONSTRAINT IF EXISTS uni_customer_nik`)
	DB.Exec(`DROP INDEX IF EXISTS idx_customer_nik`)

	// Step 0b (Phase 7B+6D): Guard idempoten — pastikan PK pendaftaran & invoice tetap pada
	// kolom bisnis setelah AutoMigrate (nomor_pendaftaran & nomor_invoice).
	//
	// Strategi aman:
	//  1. DROP FK anak yang bergantung pada PK (agar PK bisa di-drop tanpa CASCADE)
	//  2. DROP PK lama
	//  3. AutoMigrate (set PK baru via GORM tag primaryKey)
	//  4. ADD PK eksplisit (re-promote jika GORM gagal)
	//  5. RESTORE FK anak

	// -- pendaftaran: sementara lepas FK anak sebelum drop PK --
	DB.Exec(`ALTER TABLE dokumen DROP CONSTRAINT IF EXISTS fk_dokumen_nomor_pendaftaran`)
	DB.Exec(`ALTER TABLE pengaduan DROP CONSTRAINT IF EXISTS fk_pengaduan_nomor_pendaftaran`)
	DB.Exec(`ALTER TABLE customer_session DROP CONSTRAINT IF EXISTS fk_customer_session_nomor_pendaftaran`)
	DB.Exec(`ALTER TABLE verifikasi_otp DROP CONSTRAINT IF EXISTS fk_verifikasi_otp_nomor_pendaftaran`)
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS pendaftaran_pkey`)
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS uni_pendaftaran_nomor_pendaftaran`)

	// -- invoice: lepas FK anak sebelum drop PK --
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS fk_pendaftaran_nomor_invoice`)
	DB.Exec(`ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS fk_pembayaran_nomor_invoice`)
	DB.Exec(`ALTER TABLE invoice DROP CONSTRAINT IF EXISTS invoice_pkey`)
	DB.Exec(`ALTER TABLE invoice DROP CONSTRAINT IF EXISTS uni_invoice_nomor_invoice`)

	// Step 1: AutoMigrate semua model
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

	// Step 1b: Set ulang PK eksplisit (idempoten)
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS pendaftaran_pkey`)
	DB.Exec(`ALTER TABLE pendaftaran ADD CONSTRAINT pendaftaran_pkey PRIMARY KEY (nomor_pendaftaran)`)

	DB.Exec(`ALTER TABLE invoice DROP CONSTRAINT IF EXISTS invoice_pkey`)
	DB.Exec(`ALTER TABLE invoice ADD CONSTRAINT invoice_pkey PRIMARY KEY (nomor_invoice)`)

	// Phase 8B: Set PK customer = nik (idempoten)
	DB.Exec(`ALTER TABLE customer DROP CONSTRAINT IF EXISTS customer_pkey`)
	DB.Exec(`ALTER TABLE customer ADD CONSTRAINT customer_pkey PRIMARY KEY (nik)`)

	// Step 1c: Restore FK anak ke pendaftaran.nomor_pendaftaran (idempoten)
	DB.Exec(`ALTER TABLE dokumen DROP CONSTRAINT IF EXISTS fk_dokumen_nomor_pendaftaran`)
	DB.Exec(`ALTER TABLE dokumen ADD CONSTRAINT fk_dokumen_nomor_pendaftaran FOREIGN KEY (nomor_pendaftaran) REFERENCES pendaftaran(nomor_pendaftaran) ON UPDATE CASCADE ON DELETE RESTRICT`)
	DB.Exec(`ALTER TABLE pengaduan DROP CONSTRAINT IF EXISTS fk_pengaduan_nomor_pendaftaran`)
	DB.Exec(`ALTER TABLE pengaduan ADD CONSTRAINT fk_pengaduan_nomor_pendaftaran FOREIGN KEY (nomor_pendaftaran) REFERENCES pendaftaran(nomor_pendaftaran) ON UPDATE CASCADE ON DELETE RESTRICT`)
	DB.Exec(`ALTER TABLE customer_session DROP CONSTRAINT IF EXISTS fk_customer_session_nomor_pendaftaran`)
	DB.Exec(`ALTER TABLE customer_session ADD CONSTRAINT fk_customer_session_nomor_pendaftaran FOREIGN KEY (nomor_pendaftaran) REFERENCES pendaftaran(nomor_pendaftaran) ON UPDATE CASCADE ON DELETE RESTRICT`)
	DB.Exec(`ALTER TABLE verifikasi_otp DROP CONSTRAINT IF EXISTS fk_verifikasi_otp_nomor_pendaftaran`)
	DB.Exec(`ALTER TABLE verifikasi_otp ADD CONSTRAINT fk_verifikasi_otp_nomor_pendaftaran FOREIGN KEY (nomor_pendaftaran) REFERENCES pendaftaran(nomor_pendaftaran) ON UPDATE CASCADE ON DELETE RESTRICT`)

	// Step 1d: Restore FK ke invoice.nomor_invoice (idempoten)
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS fk_pendaftaran_nomor_invoice`)
	DB.Exec(`ALTER TABLE pendaftaran ADD CONSTRAINT fk_pendaftaran_nomor_invoice FOREIGN KEY (nomor_invoice) REFERENCES invoice(nomor_invoice) ON UPDATE CASCADE`)
	DB.Exec(`ALTER TABLE pembayaran DROP CONSTRAINT IF EXISTS fk_pembayaran_nomor_invoice`)
	DB.Exec(`ALTER TABLE pembayaran ADD CONSTRAINT fk_pembayaran_nomor_invoice FOREIGN KEY (nomor_invoice) REFERENCES invoice(nomor_invoice) ON UPDATE CASCADE`)

	// Step 1e (Phase 8B): Restore FK anak ke customer.nik (idempoten)
	DB.Exec(`ALTER TABLE pendaftaran DROP CONSTRAINT IF EXISTS fk_pendaftaran_customer_nik`)
	DB.Exec(`ALTER TABLE pendaftaran ADD CONSTRAINT fk_pendaftaran_customer_nik FOREIGN KEY (customer_nik) REFERENCES customer(nik) ON UPDATE CASCADE ON DELETE RESTRICT`)
	DB.Exec(`ALTER TABLE chatbot_log DROP CONSTRAINT IF EXISTS fk_chatbot_log_customer_nik`)
	DB.Exec(`ALTER TABLE chatbot_log ADD CONSTRAINT fk_chatbot_log_customer_nik FOREIGN KEY (customer_nik) REFERENCES customer(nik) ON UPDATE CASCADE ON DELETE RESTRICT`)

	// Step 2: Isi kolom NIK yang kosong dengan nilai placeholder unik
	DB.Exec(`
		UPDATE customer
		SET nik = 'OLD-' || LEFT(REPLACE(gen_random_uuid()::text, '-', ''), 12)
		WHERE nik IS NULL OR nik = ''
	`)

	// Step 3: (dihapus) — customer.alamat sudah tidak ada, migrasi ke alamat_lengkap selesai.

	// Step 4 & 5 (data migration lama) sudah dihapus karena database
	// sudah fully migrated — semua pendaftaran sudah memiliki invoice_id
	// dan kolom nomor_invoice lama sudah tidak ada di tabel pendaftaran.

	// Step 6: (Phase 6C) kolom invoice_id di pembayaran sudah di-drop.
	// Step ini tidak lagi diperlukan.

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
