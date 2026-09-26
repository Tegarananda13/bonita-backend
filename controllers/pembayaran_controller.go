package controllers

import (
	"fmt"
	"net/http"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// recalcInvoiceStatus menghitung ulang StatusPembayaran dan TotalPembayaran
// pada Invoice berdasarkan seluruh pembayaran yang sudah diterima.
// Phase 6B: parameter diubah dari UUID ke nomor_invoice (business key).
func recalcInvoiceStatus(nomorInvoice string) {
	var invoice models.Invoice
	if err := config.DB.Where("nomor_invoice = ?", nomorInvoice).First(&invoice).Error; err != nil {
		return
	}

	var totalDiterima float64
	config.DB.
		Model(&models.Pembayaran{}).
		Where("nomor_invoice = ? AND status = ?", nomorInvoice, helpers.PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&totalDiterima)

	invoice.TotalPembayaran = totalDiterima

	var newStatus string
	if totalDiterima <= 0 {
		newStatus = models.InvoiceStatusBelumBayar
	} else if totalDiterima >= invoice.TotalTagihan {
		newStatus = models.InvoiceStatusLunas
	} else {
		newStatus = models.InvoiceStatusDP
	}
	invoice.StatusPembayaran = newStatus

	config.DB.Model(&invoice).Updates(map[string]interface{}{
		"total_pembayaran":  invoice.TotalPembayaran,
		"status_pembayaran": invoice.StatusPembayaran,
	})

	// update status semua pendaftaran yang terhubung ke invoice ini
	var pendaftaranList []models.Pendaftaran
	config.DB.Where("nomor_invoice = ?", nomorInvoice).Find(&pendaftaranList)
	for _, p := range pendaftaranList {
		helpers.UpdateStatusPendaftaran(p.NomorPendaftaran)
	}
}

func CreatePembayaran(c *gin.Context) {
	var req struct {
		Jumlah float64 `json:"jumlah" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Jumlah wajib diisi"})
		return
	}

	nomor := c.MustGet("pendaftaran_id").(string)

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Paket").
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}


	// Pastikan Invoice ada (seharusnya selalu ada karena dibuat saat pendaftaran)
	if pendaftaran.NomorInvoice == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invoice untuk pendaftaran ini belum tersedia"})
		return
	}

	invoice := pendaftaran.Invoice

	// hitung total pembayaran diterima
	var total float64
	config.DB.
		Model(&models.Pembayaran{}).
		Where("nomor_invoice = ? AND status = ?", pendaftaran.NomorInvoice, helpers.PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&total)

	// cek pembayaran pertama
	if total == 0 && req.Jumlah < 5000000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "DP minimal 5 juta"})
		return
	}

	// cek tidak melebihi total tagihan
	if total+req.Jumlah > invoice.TotalTagihan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pembayaran melebihi total harga paket"})
		return
	}

	pembayaran := models.Pembayaran{
		NomorInvoice: invoice.NomorInvoice,
		Jumlah:       req.Jumlah,
		TanggalBayar: time.Now(),
		Status:       helpers.PaymentVerificationPending,
	}

	if err := config.DB.Create(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat pembayaran"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Pembayaran berhasil dibuat",
		"data": gin.H{
			"id":            pembayaran.ID,
			"jumlah":        pembayaran.Jumlah,
			"status":        pembayaran.Status,
			"nomor_invoice": invoice.NomorInvoice,
		},
	})
}

func GetCustomerDashboard(c *gin.Context) {
	nomor := c.MustGet("pendaftaran_id").(string)

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Paket.GambarPaket", func(db *gorm.DB) *gorm.DB {
			return db.Order("is_utama DESC, urutan ASC")
		}).
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	paymentStatus := ""
	totalTagihanDash := pendaftaran.Paket.Harga
	totalPembayaranDash := 0.0
	nomorInvoiceDash := ""
	tanggalInvoiceDash := pendaftaran.TanggalDaftar
	totalOrangDash := 1

	if pendaftaran.NomorInvoice != "" {
		paymentStatus = pendaftaran.Invoice.StatusPembayaran
		totalTagihanDash = pendaftaran.Invoice.TotalTagihan
		totalPembayaranDash = pendaftaran.Invoice.TotalPembayaran
		nomorInvoiceDash = pendaftaran.Invoice.NomorInvoice
		if pendaftaran.Invoice.TotalOrang > 0 {
			totalOrangDash = pendaftaran.Invoice.TotalOrang
		} else {
			var count int64
			config.DB.Model(&models.Pendaftaran{}).Where("nomor_invoice = ?", pendaftaran.NomorInvoice).Count(&count)
			if count > 0 {
				totalOrangDash = int(count)
			}
		}
		if !pendaftaran.Invoice.CreatedAt.IsZero() {
			tanggalInvoiceDash = pendaftaran.Invoice.CreatedAt
		}
	}

	sisaTagihanDash := totalTagihanDash - totalPembayaranDash
	if sisaTagihanDash < 0 {
		sisaTagihanDash = 0
	}

	fotoURL := ""
	for _, g := range pendaftaran.Paket.GambarPaket {
		if g.IsUtama {
			fotoURL = g.FilePath
			break
		}
	}
	if fotoURL == "" && len(pendaftaran.Paket.GambarPaket) > 0 {
		fotoURL = pendaftaran.Paket.GambarPaket[0].FilePath
	}
	if fotoURL == "" {
		fotoURL = pendaftaran.Paket.FotoPaket
	}

	c.JSON(http.StatusOK, gin.H{
		// Top-level legacy fields for backward compatibility
		"nama":             pendaftaran.Customer.Nama,
		"nomor":            pendaftaran.NomorPendaftaran,
		"paket":            pendaftaran.Paket.NamaPaket,
		"harga":            pendaftaran.Paket.Harga,
		"total_tagihan":    totalTagihanDash,
		"total_pembayaran": totalPembayaranDash,
		"sisa_tagihan":     sisaTagihanDash,
		"total_orang":      totalOrangDash,
		"nomor_invoice":    nomorInvoiceDash,
		"payment_status":   paymentStatus,
		"document_status":  pendaftaran.DocumentStatus,
		"status":           pendaftaran.Status,
		"batas_waktu_dp":   pendaftaran.BatasWaktuDP,
		"tanggal_daftar":   pendaftaran.TanggalDaftar,

		// Rich nested objects
		"customer": gin.H{
			"nik":            pendaftaran.Customer.NIK,
			"nama":           pendaftaran.Customer.Nama,
			"tempat_lahir":   pendaftaran.Customer.TempatLahir,
			"tanggal_lahir":  pendaftaran.Customer.TanggalLahir,
			"jenis_kelamin":  pendaftaran.Customer.JenisKelamin,
			"no_hp":          pendaftaran.Customer.NoHP,
			"email":          pendaftaran.Customer.Email,
			"alamat_lengkap": pendaftaran.Customer.AlamatLengkap,
			"provinsi":       pendaftaran.Customer.Provinsi,
			"kabupaten_kota": pendaftaran.Customer.KabupatenKota,
			"kecamatan":      pendaftaran.Customer.Kecamatan,
			"kelurahan_desa": pendaftaran.Customer.KelurahanDesa,
			"kode_pos":       pendaftaran.Customer.KodePos,
		},
		"paket_umroh": gin.H{
			"id":                pendaftaran.Paket.ID,
			"nama_paket":        pendaftaran.Paket.NamaPaket,
			"jenis_paket":       pendaftaran.Paket.JenisPaket,
			"foto_paket":        fotoURL,
			"harga":             pendaftaran.Paket.Harga,
			"tanggal_berangkat": pendaftaran.Paket.TanggalBerangkat,
			"durasi":            pendaftaran.Paket.Durasi,
			"deskripsi":         pendaftaran.Paket.Deskripsi,
		},
		"pendaftaran": gin.H{
			"nomor_pendaftaran": pendaftaran.NomorPendaftaran,
			"tanggal_daftar":    pendaftaran.TanggalDaftar,
			"status":            pendaftaran.Status,
			"payment_status":    paymentStatus,
			"document_status":   pendaftaran.DocumentStatus,
			"batas_waktu_dp":    pendaftaran.BatasWaktuDP,
			"total_orang":       totalOrangDash,
		},
		"invoice": gin.H{
			"nomor_invoice":     nomorInvoiceDash,
			"tanggal_invoice":   tanggalInvoiceDash,
			"status_pembayaran": paymentStatus,
			"total_orang":       totalOrangDash,
			"total_tagihan":     totalTagihanDash,
			"total_pembayaran":  totalPembayaranDash,
			"sisa_tagihan":      sisaTagihanDash,
		},
	})
}

func GetPembayaran(c *gin.Context) {
	nomor := c.MustGet("pendaftaran_id").(string)

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Paket").
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.NomorInvoice == "" {
		c.JSON(http.StatusOK, gin.H{
			"total_dibayar":  0,
			"harga_paket":    pendaftaran.Paket.Harga,
			"payment_status": models.InvoiceStatusBelumBayar,
			"nomor_invoice":  "",
			"riwayat":        []gin.H{},
		})
		return
	}

	var pembayaran []models.Pembayaran
	if err := config.DB.
		Where("nomor_invoice = ?", pendaftaran.NomorInvoice).
		Order("tanggal_bayar ASC").
		Find(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil pembayaran"})
		return
	}

	var result []gin.H
	var totalDibayar float64

	for _, p := range pembayaran {
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

	invoice := pendaftaran.Invoice

	c.JSON(http.StatusOK, gin.H{
		"total_dibayar":    totalDibayar,
		"harga_paket":      pendaftaran.Paket.Harga,
		"total_tagihan":    invoice.TotalTagihan,
		"total_orang":      invoice.TotalOrang,
		"payment_status":   invoice.StatusPembayaran,
		"nomor_invoice":    invoice.NomorInvoice,
		"riwayat":          result,
	})
}

func UploadBuktiPembayaran(c *gin.Context) {
	nomor := c.MustGet("pendaftaran_id").(string)

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.NomorInvoice == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tidak ada invoice untuk pendaftaran ini"})
		return
	}

	fileHeader, err := c.FormFile("bukti")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File bukti wajib diupload"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membaca file"})
		return
	}
	defer file.Close()

	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
	fileURL, err := helpers.UploadToSupabase(file, filename, "pembayaran")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal upload bukti pembayaran"})
		return
	}

	pembayaranID := c.Param("id")

	var pembayaran models.Pembayaran
	if err := config.DB.
		Where("id = ? AND nomor_invoice = ?", pembayaranID, pendaftaran.NomorInvoice).
		First(&pembayaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	if err := config.DB.
		Model(&pembayaran).
		Update("bukti_pembayaran", fileURL).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal update bukti pembayaran"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Bukti pembayaran berhasil diupload",
		"file":    fileURL,
	})
}
