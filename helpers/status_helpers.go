package helpers

import (
	"bonita-backend/config"
	"bonita-backend/models"
)

// UpdateStatusPendaftaran menghitung ulang status utama satu Pendaftaran
// berdasarkan StatusPembayaran Invoice dan DocumentStatus Pendaftaran.
//
// Aturan "Siap Berangkat" untuk invoice grup:
// Invoice harus lunas DAN seluruh Pendaftaran dalam invoice yang sama
// harus memiliki DocumentStatus == "lengkap".
//
// Phase 3: parameter berubah dari uuid.UUID → string (nomor_pendaftaran).
func UpdateStatusPendaftaran(nomorPendaftaran string) {
	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomorPendaftaran).
		First(&pendaftaran).Error; err != nil {
		return
	}

	// Tentukan status pembayaran dari Invoice
	paymentLunas := pendaftaran.NomorInvoice != "" &&
		pendaftaran.Invoice.StatusPembayaran == models.InvoiceStatusLunas

	documentLengkap := pendaftaran.DocumentStatus == DocumentLengkap

	var status string

	if paymentLunas && documentLengkap {
		// Untuk "Siap Berangkat", pastikan SEMUA pendaftaran dalam invoice ini
		// sudah lengkap dokumennya.
		if pendaftaran.NomorInvoice != "" {
			var belumLengkap int64
			config.DB.Model(&models.Pendaftaran{}).
				Where("nomor_invoice = ? AND document_status != ?", pendaftaran.NomorInvoice, DocumentLengkap).
				Count(&belumLengkap)
			if belumLengkap > 0 {
				// Invoice sudah lunas tapi ada jamaah lain yang dokumennya belum lengkap
				status = StatusMenungguDokumen
			} else {
				status = StatusSiapBerangkat
			}
		} else {
			status = StatusSiapBerangkat
		}
	} else if paymentLunas {
		status = StatusMenungguDokumen
	} else if documentLengkap {
		status = StatusMenungguPembayaran
	} else {
		status = StatusProses
	}

	// Gunakan Update dengan map agar GORM tidak ikut-sertakan FK di WHERE clause
	config.DB.Model(&models.Pendaftaran{}).
		Where("nomor_pendaftaran = ?", pendaftaran.NomorPendaftaran).
		Update("status", status)
}
