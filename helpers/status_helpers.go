package helpers

import (
	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/google/uuid"
)

// UpdateStatusPendaftaran menghitung ulang status utama satu Pendaftaran
// berdasarkan StatusPembayaran Invoice dan DocumentStatus Pendaftaran.
//
// Aturan "Siap Berangkat" untuk invoice grup:
// Invoice harus lunas DAN seluruh Pendaftaran dalam invoice yang sama
// harus memiliki DocumentStatus == "lengkap".
func UpdateStatusPendaftaran(pendaftaranID uuid.UUID) {
	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		return
	}

	// Tentukan status pembayaran dari Invoice
	paymentLunas := pendaftaran.InvoiceID != nil &&
		pendaftaran.Invoice.StatusPembayaran == models.InvoiceStatusLunas

	documentLengkap := pendaftaran.DocumentStatus == DocumentLengkap

	var status string

	if paymentLunas && documentLengkap {
		// Untuk "Siap Berangkat", pastikan SEMUA pendaftaran dalam invoice ini
		// sudah lengkap dokumennya.
		if pendaftaran.InvoiceID != nil {
			var belumLengkap int64
			config.DB.Model(&models.Pendaftaran{}).
				Where("invoice_id = ? AND document_status != ?", pendaftaran.InvoiceID, DocumentLengkap).
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
		Where("id = ?", pendaftaran.ID).
		Update("status", status)
}
