package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// DPDeadlineHours adalah batas waktu DP dalam jam.
const DPDeadlineHours = 24

// ProcessExpiry memeriksa semua pendaftaran "proses" yang sudah melewati BatasWaktuDP
// tanpa pembayaran DP yang terverifikasi, kemudian:
//  1. Mengubah status menjadi "kadaluarsa"
//  2. Mengembalikan kuota ke paket sesuai jumlah anggota per Invoice
//
// Idempotent: hanya memproses status = "proses", jadi tidak akan double-expire.
func ProcessExpiry() {
	now := time.Now()

	// Ambil semua pendaftaran "proses" yang sudah melewati batas waktu DP
	var pendaftaranList []models.Pendaftaran
	if err := config.DB.
		Where("status = ? AND batas_waktu_dp < ? AND batas_waktu_dp != '0001-01-01 00:00:00+00'", helpers.StatusProses, now).
		Find(&pendaftaranList).Error; err != nil {
		log.Printf("[Expiry] Gagal query pendaftaran: %v", err)
		return
	}

	if len(pendaftaranList) == 0 {
		return
	}

	// Kelompokkan per InvoiceID untuk pengecekan DP dan return kuota secara grup
	type invoiceGroup struct {
		paketID string
		count   int
		hasDP   bool
	}

	invoiceMap := make(map[string]*invoiceGroup)

	for _, p := range pendaftaranList {
		if p.InvoiceID == nil {
			// Pendaftaran tanpa invoice langsung kadaluarsakan
			config.DB.Model(&models.Pendaftaran{}).
				Where("id = ?", p.ID).
				Update("status", helpers.StatusKadaluarsa)
			continue
		}

		key := p.InvoiceID.String()
		if _, exists := invoiceMap[key]; !exists {
			// Cek apakah invoice ini sudah punya DP yang diterima
			var countDP int64
			config.DB.Model(&models.Pembayaran{}).
				Where("invoice_id = ? AND status = ?", p.InvoiceID, helpers.PaymentVerificationDiterima).
				Count(&countDP)

			invoiceMap[key] = &invoiceGroup{
				paketID: p.PaketID.String(),
				hasDP:   countDP > 0,
			}
		}
		invoiceMap[key].count++
	}

	expiredCount := 0
	for invoiceIDStr, group := range invoiceMap {
		// Jika sudah ada DP terverifikasi → skip, pendaftaran tetap aktif
		if group.hasDP {
			continue
		}

		// Update semua pendaftaran dalam invoice ini → kadaluarsa
		result := config.DB.
			Model(&models.Pendaftaran{}).
			Where("invoice_id = ? AND status = ?", invoiceIDStr, helpers.StatusProses).
			Update("status", helpers.StatusKadaluarsa)
		if result.Error != nil {
			log.Printf("[Expiry] Gagal update status invoice %s: %v", invoiceIDStr, result.Error)
			continue
		}

		// Kembalikan kuota ke paket (aman dari double-return karena hanya proses "proses")
		if err := config.DB.
			Model(&models.PaketUmroh{}).
			Where("id = ?", group.paketID).
			UpdateColumn("kuota_terpakai", gorm.Expr("GREATEST(kuota_terpakai - ?, 0)", group.count)).Error; err != nil {
			log.Printf("[Expiry] Gagal kembalikan kuota paket %s: %v", group.paketID, err)
		}

		expiredCount += group.count
	}

	if expiredCount > 0 {
		log.Printf("[Expiry] %d pendaftaran dikadaluarsakan, kuota dikembalikan", expiredCount)
	}
}

// StartExpiryScheduler menjalankan ProcessExpiry secara periodik di background.
// Interval: setiap 15 menit.
func StartExpiryScheduler() {
	go func() {
		// Jalankan sekali saat startup agar pendaftaran expired langsung tertangani
		ProcessExpiry()
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ProcessExpiry()
		}
	}()
	log.Println("[Expiry] Scheduler aktif (interval: 15 menit)")
}

// ManualProcessExpiry — endpoint untuk trigger expiry manual oleh Admin.
// POST /admin/pendaftaran/process-expiry
func ManualProcessExpiry(c *gin.Context) {
	ProcessExpiry()
	c.JSON(http.StatusOK, gin.H{"message": "Proses expiry berhasil dijalankan"})
}
