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

func CreatePembayaran(c *gin.Context) {
	var req struct {
		Jumlah float64 `json:"jumlah" binding:"required"`
	}

	// validasi body
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Jumlah wajib diisi",
		})
		return
	}

	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran

	if err := config.DB.
		Preload("Paket").
		First(
			&pendaftaran,
			"id = ?",
			pendaftaranID,
		).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	// 🔥 hitung total pembayaran diterima
	var total float64

	config.DB.
		Model(&models.Pembayaran{}).
		Where("pendaftaran_id = ? AND status = ?", pendaftaran.ID, helpers.PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&total)

	// 🔥 cek apakah pembayaran pertama
	if total == 0 {

		// pembayaran pertama wajib minimal 5 juta
		if req.Jumlah < 5000000 {

			c.JSON(http.StatusBadRequest, gin.H{
				"error": "DP minimal 5 juta",
			})
			return
		}
	}

	// 🔥 cek apakah melebihi harga paket
	if total+req.Jumlah > pendaftaran.Paket.Harga {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Pembayaran melebihi total harga paket",
		})
		return
	}

	// buat pembayaran
	pembayaran := models.Pembayaran{
		PendaftaranID: pendaftaran.ID,
		Jumlah:        req.Jumlah,
		TanggalBayar:  time.Now(),
		Status:        helpers.PaymentVerificationPending,
	}

	// simpan ke DB
	if err := config.DB.Create(&pembayaran).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membuat pembayaran",
		})
		return
	}

	// 🧾 Generate nomor invoice saat pembayaran pertama (jika belum ada)
	if pendaftaran.NomorInvoice == "" {
		nomorInvoice, err := helpers.GenerateNomorInvoice(config.DB)
		if err == nil {
			// Gunakan empty struct + WHERE agar GORM tidak membawa association
			// pendaftaran.Paket (yang sudah di-Preload) ke cascade upsert.
			config.DB.
				Model(&models.Pendaftaran{}).
				Where("id = ?", pendaftaran.ID).
				Update("nomor_invoice", nomorInvoice)
			pendaftaran.NomorInvoice = nomorInvoice
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Pembayaran berhasil dibuat",
		"data": gin.H{
			"id":             pembayaran.ID,
			"jumlah":         pembayaran.Jumlah,
			"status":         pembayaran.Status,
			"nomor_invoice":  pendaftaran.NomorInvoice,
		},
	})
}

func GetCustomerDashboard(c *gin.Context) {

    pendaftaranID := c.MustGet("pendaftaran_id")

    var pendaftaran models.Pendaftaran

    if err := config.DB.
        Preload("Customer").
        Preload("Paket").
        First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {

        c.JSON(http.StatusNotFound, gin.H{
            "error": "Pendaftaran tidak ditemukan",
        })
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "nama":            pendaftaran.Customer.Nama,
        "nomor":           pendaftaran.NomorPendaftaran,
        "paket":           pendaftaran.Paket.NamaPaket,
        "harga":           pendaftaran.Paket.Harga,
        "payment_status":  pendaftaran.PaymentStatus,
        "document_status": pendaftaran.DocumentStatus,
        "status":          pendaftaran.Status,
    })
}

func GetPembayaran(c *gin.Context) {

    // ambil dari customer token
    pendaftaranID := c.MustGet("pendaftaran_id")

    // ambil pendaftaran + paket untuk harga dan nomor invoice
    var pendaftaran models.Pendaftaran
    if err := config.DB.
        Preload("Paket").
        First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
        return
    }

    var pembayaran []models.Pembayaran

    if err := config.DB.
        Where("pendaftaran_id = ?", pendaftaranID).
        Order("tanggal_bayar ASC").
        Find(&pembayaran).Error; err != nil {

        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Gagal mengambil pembayaran",
        })
        return
    }

    // response clean
    var result []gin.H

    var totalDibayar float64

    for _, p := range pembayaran {

        // hitung hanya pembayaran yang sudah diterima
        if p.Status == helpers.PaymentVerificationDiterima {
            totalDibayar += p.Jumlah
        }

        result = append(result, gin.H{
            "id":      p.ID,
            "jumlah":  p.Jumlah,
            "status":  p.Status,
            "tanggal": p.TanggalBayar,
            "bukti":   p.BuktiPembayaran,
        })
    }

    c.JSON(http.StatusOK, gin.H{
        "total_dibayar":  totalDibayar,
        "harga_paket":    pendaftaran.Paket.Harga,
        "payment_status": pendaftaran.PaymentStatus,
        "nomor_invoice":  pendaftaran.NomorInvoice,
        "riwayat":        result,
    })
}


func UploadBuktiPembayaran(c *gin.Context) {

	pendaftaranID := c.MustGet("pendaftaran_id")

	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(
			&pendaftaran,
			"id = ?",
			pendaftaranID,
		).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pendaftaran tidak ditemukan",
		})
		return
	}

	// ambil file
	fileHeader, err := c.FormFile("bukti")

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File bukti wajib diupload",
		})
		return
	}

	// buka file
	file, err := fileHeader.Open()

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal membaca file",
		})
		return
	}

	defer file.Close()

	// nama file unik
	filename := fmt.Sprintf(
		"%d_%s",
		time.Now().Unix(),
		fileHeader.Filename,
	)

	// upload ke Supabase bucket pembayaran
	fileURL, err := helpers.UploadToSupabase(
		file,
		filename,
		"pembayaran",
	)

	if err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal upload bukti pembayaran",
		})
		return
	}

	// ambil id pembayaran dari URL
	pembayaranID := c.Param("id")

	var pembayaran models.Pembayaran

	if err := config.DB.
		Where(
			"id = ? AND pendaftaran_id = ?",
			pembayaranID,
			pendaftaran.ID,
		).
		First(&pembayaran).Error; err != nil {

		c.JSON(http.StatusNotFound, gin.H{
			"error": "Pembayaran tidak ditemukan",
		})
		return
	}

	// update URL bukti pembayaran
	pembayaran.BuktiPembayaran = fileURL

	if err := config.DB.
		Model(&pembayaran).
		Update(
			"bukti_pembayaran",
			pembayaran.BuktiPembayaran,
		).Error; err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal update bukti pembayaran",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Bukti pembayaran berhasil diupload",
		"file": pembayaran.BuktiPembayaran,
	})
}