package controllers

import (
	"fmt"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

// GetAllPengaduan — GET /admin/pengaduan
// Mengembalikan seluruh laporan pengaduan dengan informasi customer & pendaftaran.
func GetAllPengaduan(c *gin.Context) {

	statusFilter := c.Query("status") // opsional: ?status=menunggu

	var pengaduanList []models.Pengaduan

	q := config.DB.
		Preload("Pendaftaran").
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		Order("created_at DESC")

	if statusFilter != "" {
		q = q.Where("pengaduan.status = ?", statusFilter)
	}

	if err := q.Find(&pengaduanList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mengambil data pengaduan",
		})
		return
	}

	var result []gin.H
	for _, p := range pengaduanList {
		result = append(result, gin.H{
			"id":                p.ID,
			"nomor_pendaftaran": p.Pendaftaran.NomorPendaftaran,
			"nama_customer":     p.Pendaftaran.Customer.Nama,
			"no_hp":             p.Pendaftaran.Customer.NoHP,
			"paket":             p.Pendaftaran.Paket.NamaPaket,
			"judul":             p.Judul,
			"kategori":          p.Kategori,
			"status":            p.Status,
			"created_at":        p.CreatedAt,
		})
	}

	// Hitung jumlah yang masih "menunggu" untuk badge sidebar
	var countMenunggu int64
	config.DB.Model(&models.Pengaduan{}).Where("status = ?", helpers.PengaduanMenunggu).Count(&countMenunggu)

	c.JSON(http.StatusOK, gin.H{
		"total":          len(result),
		"count_menunggu": countMenunggu,
		"data":           result,
	})
}

// GetDetailPengaduan — GET /admin/pengaduan/:id
// Mengembalikan detail lengkap satu laporan pengaduan.
func GetDetailPengaduan(c *gin.Context) {

	id := c.Param("id")

	var pengaduan models.Pengaduan

	if err := config.DB.
		Preload("Pendaftaran").
		Preload("Pendaftaran.Customer").
		Preload("Pendaftaran.Paket").
		Preload("Pendaftaran.Invoice").
		First(&pengaduan, "pengaduan.id = ?", id).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{"error": "Pengaduan tidak ditemukan"})
		return
	}

	// Ambil data pembayaran terkait invoice pendaftaran
	var pembayaran []models.Pembayaran
	if pengaduan.Pendaftaran.NomorInvoice != "" {
		config.DB.Where("nomor_invoice = ?", pengaduan.Pendaftaran.NomorInvoice).Order("tanggal_bayar DESC").Find(&pembayaran)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":     pengaduan.ID,
		"judul":  pengaduan.Judul,
		"isi":    pengaduan.IsiPengaduan,
		"kategori": pengaduan.Kategori,
		"status": pengaduan.Status,
		"created_at": pengaduan.CreatedAt,
		"updated_at": pengaduan.UpdatedAt,
		"customer": gin.H{
			"nama":  pengaduan.Pendaftaran.Customer.Nama,
			"nik":   pengaduan.Pendaftaran.Customer.NIK,
			"no_hp": pengaduan.Pendaftaran.Customer.NoHP,
			"email": pengaduan.Pendaftaran.Customer.Email,
		},
		"pendaftaran": gin.H{
			"nomor_pendaftaran": pengaduan.Pendaftaran.NomorPendaftaran,
			"paket":             pengaduan.Pendaftaran.Paket.NamaPaket,
			"tanggal_berangkat": pengaduan.Pendaftaran.Paket.TanggalBerangkat,
			"payment_status":    paymentStatusFromPendaftaran(pengaduan.Pendaftaran),
			"document_status":   pengaduan.Pendaftaran.DocumentStatus,
			"status":            pengaduan.Pendaftaran.Status,
		},
		"riwayat_pembayaran": pembayaran,
	})
}

// UpdateStatusPengaduan — PATCH /admin/pengaduan/:id/status
// Admin mengubah status pengaduan, lalu kirim email notifikasi ke customer.
func UpdateStatusPengaduan(c *gin.Context) {

	id := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status wajib diisi"})
		return
	}

	// validasi status
	validStatuses := map[string]bool{
		helpers.PengaduanMenunggu: true,
		helpers.PengaduanDiproses: true,
		helpers.PengaduanSelesai:  true,
	}
	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Status tidak valid. Pilihan: menunggu, diproses, selesai",
		})
		return
	}

	// ambil pengaduan beserta relasi pendaftaran dan customer
	var pengaduan models.Pengaduan
	if err := config.DB.
		Preload("Pendaftaran.Customer").
		First(&pengaduan, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pengaduan tidak ditemukan"})
		return
	}

	// Cegah duplicate email: skip jika status tidak berubah
	if pengaduan.Status == req.Status {
		c.JSON(http.StatusOK, gin.H{
			"message":    "Status tidak berubah",
			"status":     pengaduan.Status,
			"email_sent": false,
		})
		return
	}

	// simpan status baru ke database
	if err := config.DB.Model(&pengaduan).Updates(map[string]interface{}{
		"status":     req.Status,
		"updated_at": time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengubah status"})
		return
	}

	// ── Kirim email notifikasi ke customer ────────────────────────────────────
	emailSent := false
	customer := pengaduan.Pendaftaran.Customer

	if customer.Email == "" {
		fmt.Printf("[Email Notifikasi] Warning: customer untuk pengaduan %s tidak memiliki email\n", id)
	} else {
		// Tentukan subject & emailBody berdasarkan status baru
		var subject, emailBody string
		nomorUMR := pengaduan.NomorPendaftaran

		switch req.Status {
		case helpers.PengaduanDiproses:
			subject = "Pengaduan Anda Sedang Diproses - Bonita Umrah"
			emailBody = fmt.Sprintf(
				"Halo %s,\n\n"+
					"Pengaduan Anda dengan nomor referensi %s saat ini telah berstatus Diproses.\n\n"+
					"Admin Bonita Umrah sedang menindaklanjuti pengaduan Anda.\n\n"+
					"Jika ada pertanyaan, Anda dapat menghubungi kami kembali melalui aplikasi.\n\n"+
					"Terima kasih,\n"+
					"Tim Bonita Umrah",
				customer.Nama, nomorUMR,
			)
		case helpers.PengaduanSelesai:
			subject = "Pengaduan Anda Telah Selesai - Bonita Umrah"
			emailBody = fmt.Sprintf(
				"Halo %s,\n\n"+
					"Pengaduan Anda dengan nomor referensi %s telah berstatus Selesai.\n\n"+
					"Pengaduan Anda telah selesai ditangani oleh Admin Bonita Umrah.\n\n"+
					"Jika ada pertanyaan lebih lanjut, Anda dapat menghubungi kami kembali melalui aplikasi.\n\n"+
					"Terima kasih,\n"+
					"Tim Bonita Umrah",
				customer.Nama, nomorUMR,
			)
		default:
			// Status 'menunggu' — tidak perlu email
		}

		if subject != "" {
			if err := helpers.SendEmail(customer.Email, subject, emailBody); err != nil {
				// Email gagal: catat ke log, status database tetap tersimpan
				fmt.Printf("[Email Notifikasi] Gagal kirim email ke %s untuk pengaduan %s: %v\n",
					customer.Email, id, err)
			} else {
				emailSent = true
				fmt.Printf("[Email Notifikasi] Email berhasil dikirim ke %s (pengaduan %s → %s)\n",
					customer.Email, id, req.Status)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Status berhasil diperbarui",
		"status":     req.Status,
		"email_sent": emailSent,
	})
}
