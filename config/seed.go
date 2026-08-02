package config

import (
	"bonita-backend/models"
	"time"
)

// Seed menanamkan data dummy untuk verifikasi relasi Invoice.
// Hanya berjalan jika tabel invoices kosong (idempotent).
func Seed() {
	var count int64
	DB.Model(&models.Invoice{}).Count(&count)
	if count > 0 {
		return // sudah ada data, skip
	}

	// Cari paket pertama yang tersedia untuk dijadikan acuan harga
	var paket models.PaketUmroh
	if err := DB.First(&paket).Error; err != nil {
		return // tidak ada paket, skip seed
	}

	// ── INV-001: 1 Invoice → 1 Pendaftaran ──────────────────────────────────

	inv1 := models.Invoice{
		NomorInvoice:     "INV-SEED-001",
		TotalOrang:       1,
		TotalTagihan:     paket.Harga,
		TotalPembayaran:  0,
		StatusPembayaran: models.InvoiceStatusBelumBayar,
	}
	if err := DB.Create(&inv1).Error; err != nil {
		return
	}

	// Customer seed 1
	cust1 := models.Customer{
		NIK:           "1234567890123456",
		Nama:          "Seed Customer Satu",
		TempatLahir:   "Jakarta",
		TanggalLahir:  time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Laki-laki",
		NoHP:          "08111000001",
		Email:         "seed1@example.com",
		AlamatLengkap: "Jl. Seed No. 1",
		CreatedAt:     time.Now(),
	}
	if err := DB.Create(&cust1).Error; err != nil {
		return
	}

	// Pendaftaran seed 1
	nomor1 := "UMR-SEED-0001"
	pend1 := models.Pendaftaran{
		NomorPendaftaran: nomor1,
		CustomerID:       cust1.ID,
		PaketID:          paket.ID,
		InvoiceID:        &inv1.ID,
		DocumentStatus:   "belum",
		Status:           "proses",
		TanggalDaftar:    time.Now(),
	}
	DB.Create(&pend1)

	// ── INV-002: 1 Invoice → 2 Pendaftaran + 2 Pembayaran ───────────────────

	inv2 := models.Invoice{
		NomorInvoice:     "INV-SEED-002",
		TotalOrang:       2,
		TotalTagihan:     paket.Harga * 2,
		TotalPembayaran:  10_000_000,
		StatusPembayaran: models.InvoiceStatusDP,
	}
	if err := DB.Create(&inv2).Error; err != nil {
		return
	}

	// Customer seed 2
	cust2 := models.Customer{
		NIK:           "9876543210987654",
		Nama:          "Seed Customer Dua",
		TempatLahir:   "Bandung",
		TanggalLahir:  time.Date(1985, 6, 15, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Perempuan",
		NoHP:          "08111000002",
		Email:         "seed2@example.com",
		AlamatLengkap: "Jl. Seed No. 2",
		CreatedAt:     time.Now(),
	}
	if err := DB.Create(&cust2).Error; err != nil {
		return
	}

	// Customer seed 3
	cust3 := models.Customer{
		NIK:           "1111222233334444",
		Nama:          "Seed Customer Tiga",
		TempatLahir:   "Surabaya",
		TanggalLahir:  time.Date(1992, 3, 20, 0, 0, 0, 0, time.UTC),
		JenisKelamin:  "Laki-laki",
		NoHP:          "08111000003",
		Email:         "seed3@example.com",
		AlamatLengkap: "Jl. Seed No. 3",
		CreatedAt:     time.Now(),
	}
	if err := DB.Create(&cust3).Error; err != nil {
		return
	}

	// Pendaftaran seed 2 & 3 (keduanya ke invoice yang sama)
	pend2 := models.Pendaftaran{
		NomorPendaftaran: "UMR-SEED-0002",
		CustomerID:       cust2.ID,
		PaketID:          paket.ID,
		InvoiceID:        &inv2.ID,
		DocumentStatus:   "belum",
		Status:           "proses",
		TanggalDaftar:    time.Now(),
	}
	DB.Create(&pend2)

	pend3 := models.Pendaftaran{
		NomorPendaftaran: "UMR-SEED-0003",
		CustomerID:       cust3.ID,
		PaketID:          paket.ID,
		InvoiceID:        &inv2.ID,
		DocumentStatus:   "belum",
		Status:           "proses",
		TanggalDaftar:    time.Now(),
	}
	DB.Create(&pend3)

	// 2 Pembayaran untuk INV-002
	pay1 := models.Pembayaran{
		InvoiceID:    inv2.ID,
		Jumlah:       5_000_000,
		TanggalBayar: time.Now().AddDate(0, 0, -7),
		Status:       "diterima",
	}
	DB.Create(&pay1)

	pay2 := models.Pembayaran{
		InvoiceID:    inv2.ID,
		Jumlah:       5_000_000,
		TanggalBayar: time.Now().AddDate(0, 0, -3),
		Status:       "diterima",
	}
	DB.Create(&pay2)
}
