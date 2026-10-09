package controllers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// paymentStatusFromPendaftaran mengambil payment_status dari Invoice milik Pendaftaran.
// Mengembalikan string kosong jika Invoice belum ada.
func paymentStatusFromPendaftaran(p models.Pendaftaran) string {
	if p.NomorInvoice == "" {
		return helpers.PaymentBelum
	}
	return p.Invoice.StatusPembayaran
}

// getStatusPriority menentukan nomor kelompok prioritas status (1..5)
// 1. Menunggu pembayaran / dokumen
// 2. Sedang proses
// 3. Siap berangkat
// 4. Selesai
// 5. Kedaluwarsa / dibatalkan
func getStatusPriority(status string) int {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "perlu_perbaikan":
		return 1
	case "menunggu_pembayaran", "menunggu_dokumen", "menunggu":
		return 2
	case "proses", "diproses":
		return 3
	case "menunggu_verifikasi_manager":
		return 4
	case "siap_berangkat":
		return 5
	case "selesai":
		return 6
	case "kadaluarsa", "batal":
		return 7
	default:
		return 8
	}
}

// sortPendaftaran mengurutkan pendaftaran secara deterministik berdasarkan
// 1. Kelompok prioritas status (1..5)
// 2. Sub-urutan per kelompok sesuai spesifikasi:
//   - Menunggu pembayaran / dokumen: paling lama menunggu lebih dulu (TanggalDaftar ASC)
//   - Sedang proses: tanggal pendaftaran paling lama lebih dulu (TanggalDaftar ASC)
//   - Siap berangkat: tanggal keberangkatan paling dekat lebih dulu (Paket.TanggalBerangkat ASC)
//   - Selesai: tanggal penyelesaian terbaru lebih dulu (Paket.TanggalBerangkat/TanggalDaftar DESC)
//   - Kedaluwarsa / dibatalkan: tanggal perubahan status terbaru lebih dulu (BatasWaktuDP/TanggalDaftar DESC)
func sortPendaftaran(list []models.Pendaftaran) {
	sort.SliceStable(list, func(i, j int) bool {
		a := list[i]
		b := list[j]

		prioA := getStatusPriority(a.Status)
		prioB := getStatusPriority(b.Status)

		if prioA != prioB {
			return prioA < prioB
		}

		switch prioA {
		case 1, 2:
			// Perlu perbaikan / Menunggu pembayaran / dokumen: paling lama menunggu terlebih dahulu (ASC)
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.Before(b.TanggalDaftar)
			}
			return a.NomorPendaftaran < b.NomorPendaftaran

		case 3:
			// Sedang proses: tanggal pendaftaran paling lama terlebih dahulu (ASC)
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.Before(b.TanggalDaftar)
			}
			return a.NomorPendaftaran < b.NomorPendaftaran

		case 4, 5:
			// Menunggu verifikasi manager / Siap berangkat: tanggal keberangkatan paling dekat terlebih dahulu (ASC)
			if !a.Paket.TanggalBerangkat.Equal(b.Paket.TanggalBerangkat) {
				return a.Paket.TanggalBerangkat.Before(b.Paket.TanggalBerangkat)
			}
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.Before(b.TanggalDaftar)
			}
			return a.NomorPendaftaran < b.NomorPendaftaran

		case 6:
			// Selesai: tanggal penyelesaian terbaru terlebih dahulu (DESC)
			if !a.Paket.TanggalBerangkat.Equal(b.Paket.TanggalBerangkat) {
				return a.Paket.TanggalBerangkat.After(b.Paket.TanggalBerangkat)
			}
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.After(b.TanggalDaftar)
			}
			return a.NomorPendaftaran > b.NomorPendaftaran

		case 7:
			// Kedaluwarsa / dibatalkan: tanggal perubahan status terbaru terlebih dahulu (DESC)
			dateA := a.BatasWaktuDP
			if dateA.IsZero() {
				dateA = a.TanggalDaftar
			}
			dateB := b.BatasWaktuDP
			if dateB.IsZero() {
				dateB = b.TanggalDaftar
			}
			if !dateA.Equal(dateB) {
				return dateA.After(dateB)
			}
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.After(b.TanggalDaftar)
			}
			return a.NomorPendaftaran > b.NomorPendaftaran

		default:
			return a.TanggalDaftar.Before(b.TanggalDaftar)
		}
	})
}

func GetAllPendaftaran(c *gin.Context) {
	// Sinkronisasi status pendaftaran aktif sebelum mengambil data
	helpers.SyncActivePendaftaran()

	var pendaftaran []models.Pendaftaran

	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		Preload("Approver").
		Find(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data pendaftaran"})
		return
	}

	sortPendaftaran(pendaftaran)

	var result []gin.H
	for _, p := range pendaftaran {
		nomorInvoice := p.NomorInvoice
		totalTagihan := p.Paket.Harga
		totalPembayaran := 0.0
		if p.NomorInvoice != "" {
			totalTagihan = p.Invoice.TotalTagihan
			totalPembayaran = p.Invoice.TotalPembayaran
		}
		var approverNama string
		if p.Approver != nil {
			approverNama = p.Approver.Nama
		}
		result = append(result, gin.H{
			"nomor_pendaftaran":          p.NomorPendaftaran,
			"nomor_invoice":              nomorInvoice,
			"nama_customer":              p.Customer.Nama,
			"paket":                      p.Paket.NamaPaket,
			"tanggal_berangkat":          p.Paket.TanggalBerangkat,
			"payment_status":             paymentStatusFromPendaftaran(p),
			"document_status":            p.DocumentStatus,
			"status":                     p.Status,
			"tanggal_daftar":             p.TanggalDaftar,
			"total_tagihan":              totalTagihan,
			"total_pembayaran":           totalPembayaran,
			"registration_source":        p.RegistrationSource,
			"registered_by":              p.RegisteredBy,
			"registered_by_label":        helpers.GetRegistrationLabel(p.RegistrationSource, p.RegisteredBy),
			"catatan_verifikasi_manager": p.CatatanVerifikasiManager,
			"approved_by":                p.ApprovedBy,
			"approved_at":                p.ApprovedAt,
			"approver_nama":              approverNama,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(result),
		"data":  result,
	})
}

// GetPendaftaranSaya - mengambil pendaftaran yang di-assign ke admin login
func GetPendaftaranSaya(c *gin.Context) {
	userIDString := c.MustGet("user_id").(string)

	userID, err := uuid.Parse(userIDString)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID tidak valid"})
		return
	}

	// Sinkronisasi status pendaftaran aktif sebelum mengambil data
	helpers.SyncActivePendaftaran()

	var pendaftaran []models.Pendaftaran

	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		Where("user_id = ?", userID).
		Find(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data"})
		return
	}

	sortPendaftaran(pendaftaran)

	var result []gin.H
	for _, p := range pendaftaran {
		result = append(result, gin.H{
			"nomor_pendaftaran": p.NomorPendaftaran,
			"nama_customer":     p.Customer.Nama,
			"paket":             p.Paket.NamaPaket,
			"tanggal_berangkat": p.Paket.TanggalBerangkat,
			"payment_status":    paymentStatusFromPendaftaran(p),
			"document_status":   p.DocumentStatus,
			"status":            p.Status,
			"tanggal_daftar":    p.TanggalDaftar,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(result),
		"data":  result,
	})
}

func GetDetailPendaftaran(c *gin.Context) {
	nomor := c.Param("nomor")

	var current models.Pendaftaran
	config.DB.Select("status").Where("nomor_pendaftaran = ?", nomor).First(&current)
	if current.Status != helpers.StatusPerluPerbaikan {
		// Pastikan status grup dan dokumen pendaftaran ini tersinkronisasi jika bukan dalam tahap perbaikan
		helpers.SyncPendaftaranStatusByNomor(nomor)
	}

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("User").
		Preload("Invoice").
		Preload("Approver").
		Where("nomor_pendaftaran = ?", nomor).
		First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// Ambil pembayaran via NomorInvoice
	var pembayaran []models.Pembayaran
	if pendaftaran.NomorInvoice != "" {
		config.DB.
			Where("nomor_invoice = ?", pendaftaran.NomorInvoice).
			Order("tanggal_bayar DESC").
			Find(&pembayaran)
	}

	var dokumen []models.Dokumen
	config.DB.
		Where("nomor_pendaftaran = ?", pendaftaran.NomorPendaftaran).
		Order("created_at DESC").
		Find(&dokumen)

	// Bangun payment_status dari Invoice
	paymentStatus := models.InvoiceStatusBelumBayar
	totalTagihan := pendaftaran.Paket.Harga
	totalPembayaran := 0.0
	totalPerlengkapan := 0.0
	nomorInvoice := pendaftaran.NomorInvoice
	if pendaftaran.NomorInvoice != "" {
		paymentStatus = pendaftaran.Invoice.StatusPembayaran
		totalTagihan = pendaftaran.Invoice.TotalTagihan
		totalPembayaran = pendaftaran.Invoice.TotalPembayaran
		totalPerlengkapan = pendaftaran.Invoice.TotalPerlengkapan
	}

	// Muat SEMUA pendaftaran dalam invoice yang sama (grup jamaah)
	var semuaPendaftaran []models.Pendaftaran
	if pendaftaran.NomorInvoice != "" {
		config.DB.
			Preload("Customer").
			Where("nomor_invoice = ?", pendaftaran.NomorInvoice).
			Order("tanggal_daftar ASC").
			Find(&semuaPendaftaran)
	}
	var grupJamaah []gin.H
	for _, gp := range semuaPendaftaran {
		grupJamaah = append(grupJamaah, gin.H{
			"nomor_pendaftaran":  gp.NomorPendaftaran,
			"nama":               gp.Customer.Nama,
			"nik":                gp.Customer.NIK,
			"ambil_perlengkapan": gp.AmbilPerlengkapan,
			"harga_perlengkapan": gp.HargaPerlengkapan,
		})
	}

	approverNama := ""
	var approverObj gin.H
	if pendaftaran.Approver != nil {
		approverNama = pendaftaran.Approver.Nama
		approverObj = gin.H{
			"id":       pendaftaran.Approver.ID,
			"nama":     pendaftaran.Approver.Nama,
			"username": pendaftaran.Approver.Username,
			"role":     pendaftaran.Approver.Role,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"pendaftaran": gin.H{
			"NomorPendaftaran":           pendaftaran.NomorPendaftaran,
			"Customer":                   pendaftaran.Customer,
			"Paket":                      pendaftaran.Paket,
			"User":                       pendaftaran.User,
			"nomor_invoice":              nomorInvoice,
			"payment_status":             paymentStatus,
			"total_tagihan":              totalTagihan,
			"total_pembayaran":           totalPembayaran,
			"total_perlengkapan":         totalPerlengkapan,
			"ambil_perlengkapan":         pendaftaran.AmbilPerlengkapan,
			"harga_perlengkapan":         pendaftaran.HargaPerlengkapan,
			"DocumentStatus":             pendaftaran.DocumentStatus,
			"Status":                     pendaftaran.Status,
			"TanggalDaftar":              pendaftaran.TanggalDaftar,
			"BatasWaktuDP":               pendaftaran.BatasWaktuDP,
			"registration_source":        pendaftaran.RegistrationSource,
			"registered_by":              pendaftaran.RegisteredBy,
			"registered_by_label":        helpers.GetRegistrationLabel(pendaftaran.RegistrationSource, pendaftaran.RegisteredBy),
			"catatan_verifikasi_manager": pendaftaran.CatatanVerifikasiManager,
			"approved_by":                pendaftaran.ApprovedBy,
			"approved_at":                pendaftaran.ApprovedAt,
			"approver":                   approverObj,
			"approver_nama":              approverNama,
		},
		"pembayaran":  pembayaran,
		"dokumen":     dokumen,
		"grup_jamaah": grupJamaah,
	})
}

// AssignPendaftaran — PUT /pic/pendaftaran/:nomor/assign
func AssignPendaftaran(c *gin.Context) {
	nomor := c.Param("nomor")

	userIDString := c.MustGet("user_id").(string)
	userID, err := uuid.Parse(userIDString)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID tidak valid"})
		return
	}

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Where("nomor_pendaftaran = ?", nomor).
		First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.UserID != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pendaftaran sudah ditangani admin"})
		return
	}

	pendaftaran.UserID = &userID
	if err := config.DB.Save(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil pendaftaran"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Pendaftaran berhasil diambil",
		"data": gin.H{
			"nomor_pendaftaran": pendaftaran.NomorPendaftaran,
			"admin_id":          userID,
		},
	})
}

// TandaiSelesai — PUT /pic/pendaftaran/:nomor/selesai
func TandaiSelesai(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Where("nomor_pendaftaran = ?", nomor).
		First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.Status != helpers.StatusSiapBerangkat {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Hanya jamaah dengan status 'Siap Berangkat' yang dapat ditandai selesai",
		})
		return
	}

	if err := config.DB.
		Model(&pendaftaran).
		Update("status", helpers.StatusSelesai).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menandai jamaah selesai"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Jamaah berhasil ditandai selesai.",
		"data":    gin.H{"nomor_pendaftaran": pendaftaran.NomorPendaftaran},
	})
}

// GetPendaftaranStatus — GET /admin/pendaftaran/:nomor/status
// Mengembalikan status terkini pendaftaran setelah sinkronisasi otomatis
func GetPendaftaranStatus(c *gin.Context) {
	nomor := c.Param("nomor")
	helpers.SyncPendaftaranStatusByNomor(nomor)

	var p models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).
		First(&p).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"nomor_pendaftaran": p.NomorPendaftaran,
		"nomor_invoice":     p.NomorInvoice,
		"status":            p.Status,
		"document_status":   p.DocumentStatus,
		"payment_status":    paymentStatusFromPendaftaran(p),
	})
}

// AdminAjukanVerifikasi — POST /admin/pendaftaran/:nomor/ajukan-verifikasi
// Digunakan admin setelah memperbaiki data/dokumen untuk mengajukan ulang pendaftaran ke antrean Manager
func AdminAjukanVerifikasi(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).
		First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// 1. Ambil seluruh pendaftaran dalam invoice grup atau individu
	var pendaftaranList []models.Pendaftaran
	if pendaftaran.NomorInvoice != "" {
		config.DB.Where("nomor_invoice = ?", pendaftaran.NomorInvoice).Find(&pendaftaranList)
	} else {
		pendaftaranList = []models.Pendaftaran{pendaftaran}
	}

	// 2. Validasi dokumen seluruh jamaah
	for _, p := range pendaftaranList {
		var docs []models.Dokumen
		config.DB.Where("nomor_pendaftaran = ?", p.NomorPendaftaran).Find(&docs)
		if helpers.CalculateDocumentStatus(docs) != helpers.DocumentLengkap {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": fmt.Sprintf("Dokumen untuk jamaah %s belum lengkap", p.NomorPendaftaran),
			})
			return
		}
	}

	// 3. Validasi pembayaran lunas
	if pendaftaran.NomorInvoice != "" {
		var invoice models.Invoice
		config.DB.Where("nomor_invoice = ?", pendaftaran.NomorInvoice).First(&invoice)
		if !helpers.IsInvoiceLunas(invoice.TotalPembayaran, invoice.TotalTagihan, invoice.StatusPembayaran) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": "Pembayaran belum lunas",
			})
			return
		}
	}

	// 4. Update status seluruh pendaftaran menjadi menunggu_verifikasi_manager
	for _, p := range pendaftaranList {
		config.DB.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", p.NomorPendaftaran).
			Updates(map[string]interface{}{
				"status": helpers.StatusMenungguVerifikasiManager,
			})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Pendaftaran berhasil diajukan untuk verifikasi Administration Manager",
		"status":  helpers.StatusMenungguVerifikasiManager,
	})
}
