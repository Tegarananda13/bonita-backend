package controllers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GetManagerQueue — GET /manager/verifikasi
// Mengembalikan pendaftaran untuk Manager berdasarkan status:
// - status="menunggu_verifikasi_manager" (default antrean)
// - status="siap_berangkat" (pendaftaran yang sudah disahkan Manager, approved_at IS NOT NULL)
// - status="perlu_perbaikan" (pendaftaran yang diminta perbaikan oleh Manager)
func GetManagerQueue(c *gin.Context) {
	statusFilter := strings.TrimSpace(c.Query("status"))
	if statusFilter == "" {
		statusFilter = helpers.StatusMenungguVerifikasiManager
	}

	query := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		Preload("Approver")

	switch statusFilter {
	case helpers.StatusSiapBerangkat:
		query = query.Where("status = ? AND approved_at IS NOT NULL", helpers.StatusSiapBerangkat).
			Order("approved_at DESC")
	case helpers.StatusPerluPerbaikan:
		query = query.Where("status = ?", helpers.StatusPerluPerbaikan).
			Order("tanggal_daftar DESC")
	default: // helpers.StatusMenungguVerifikasiManager
		query = query.Where("status = ?", helpers.StatusMenungguVerifikasiManager).
			Order("tanggal_daftar ASC")
	}

	var pendaftaranList []models.Pendaftaran
	if err := query.Find(&pendaftaranList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data antrean verifikasi"})
		return
	}

	result := []gin.H{}
	seenInvoices := make(map[string]bool)

	for _, p := range pendaftaranList {
		nomorInvoice := p.NomorInvoice
		if nomorInvoice != "" {
			if seenInvoices[nomorInvoice] {
				continue
			}
			seenInvoices[nomorInvoice] = true
		}

		totalTagihan := p.Paket.Harga
		totalPembayaran := 0.0
		paymentStatus := helpers.PaymentBelum
		totalJamaah := 1

		if p.NomorInvoice != "" {
			totalTagihan = p.Invoice.TotalTagihan
			totalPembayaran = p.Invoice.TotalPembayaran
			paymentStatus = p.Invoice.StatusPembayaran

			var count int64
			config.DB.Model(&models.Pendaftaran{}).
				Where("nomor_invoice = ?", p.NomorInvoice).
				Count(&count)
			if count > 0 {
				totalJamaah = int(count)
			}
		}

		approverNama := ""
		if p.Approver != nil {
			approverNama = p.Approver.Nama
		}

		result = append(result, gin.H{
			"nomor_pendaftaran":          p.NomorPendaftaran,
			"nomor_invoice":              nomorInvoice,
			"nama_customer":              p.Customer.Nama,
			"nik_customer":               p.Customer.NIK,
			"total_jamaah":               totalJamaah,
			"paket":                      p.Paket.NamaPaket,
			"tanggal_berangkat":          p.Paket.TanggalBerangkat,
			"total_tagihan":              totalTagihan,
			"total_pembayaran":           totalPembayaran,
			"payment_status":             paymentStatus,
			"document_status":            p.DocumentStatus,
			"tanggal_daftar":             p.TanggalDaftar,
			"status":                     p.Status,
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

// GetManagerDetailVerifikasi — GET /manager/verifikasi/:nomor
// Mengembalikan detail lengkap pendaftaran untuk diperiksa oleh Administration Manager
func GetManagerDetailVerifikasi(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("User").
		Preload("Invoice").
		Preload("Approver").
		Where("nomor_pendaftaran = ?", nomor).
		First(&pendaftaran).Error

	if err != nil {
		// Fallback cari via nomor invoice
		err = config.DB.
			Preload("Customer").
			Preload("Paket").
			Preload("User").
			Preload("Invoice").
			Preload("Approver").
			Where("nomor_invoice = ?", nomor).
			First(&pendaftaran).Error
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
			return
		}
	}

	// 1. Ambil seluruh anggota grup dalam invoice yang sama
	var semuaPendaftaran []models.Pendaftaran
	if pendaftaran.NomorInvoice != "" {
		config.DB.
			Preload("Customer").
			Where("nomor_invoice = ?", pendaftaran.NomorInvoice).
			Order("tanggal_daftar ASC").
			Find(&semuaPendaftaran)
	} else {
		semuaPendaftaran = []models.Pendaftaran{pendaftaran}
	}

	// 2. Format data seluruh jamaah beserta dokumen masing-masing
	jamaahList := []gin.H{}
	for _, jp := range semuaPendaftaran {
		var docs []models.Dokumen
		config.DB.
			Where("nomor_pendaftaran = ?", jp.NomorPendaftaran).
			Order("created_at ASC").
			Find(&docs)

		docsPersyaratan := []gin.H{}
		docsPerjalanan := []gin.H{}

		travelMap := map[string]bool{
			"visa":          true,
			"tiket_pesawat": true,
			"nusuk":         true,
		}

		for _, d := range docs {
			docItem := gin.H{
				"id":               d.ID,
				"jenis":            d.JenisDokumen,
				"status":           d.StatusValidasi,
				"file":             d.FilePath,
				"alasan_penolakan": d.AlasanPenolakan,
				"created_at":       d.CreatedAt,
			}
			if travelMap[strings.ToLower(d.JenisDokumen)] {
				docsPerjalanan = append(docsPerjalanan, docItem)
			} else {
				docsPersyaratan = append(docsPersyaratan, docItem)
			}
		}

		jamaahList = append(jamaahList, gin.H{
			"nomor_pendaftaran":   jp.NomorPendaftaran,
			"nama":                jp.Customer.Nama,
			"nik":                 jp.Customer.NIK,
			"tempat_lahir":        jp.Customer.TempatLahir,
			"tanggal_lahir":       jp.Customer.TanggalLahir,
			"jenis_kelamin":       jp.Customer.JenisKelamin,
			"no_hp":               jp.Customer.NoHP,
			"email":               jp.Customer.Email,
			"alamat_lengkap":      jp.Customer.AlamatLengkap,
			"ambil_perlengkapan":  jp.AmbilPerlengkapan,
			"harga_perlengkapan":  jp.HargaPerlengkapan,
			"document_status":     jp.DocumentStatus,
			"dokumen_persyaratan": docsPersyaratan,
			"dokumen_perjalanan":  docsPerjalanan,
		})
	}

	// 3. Ambil riwayat pembayaran
	pembayaranList := []models.Pembayaran{}
	if pendaftaran.NomorInvoice != "" {
		config.DB.
			Where("nomor_invoice = ?", pendaftaran.NomorInvoice).
			Order("tanggal_bayar DESC").
			Find(&pembayaranList)
	}

	// 4. Hitung ringkasan invoice
	paymentStatus := helpers.PaymentBelum
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
			"nomor_pendaftaran":          pendaftaran.NomorPendaftaran,
			"nomor_invoice":              nomorInvoice,
			"tanggal_daftar":             pendaftaran.TanggalDaftar,
			"status":                     pendaftaran.Status,
			"document_status":            pendaftaran.DocumentStatus,
			"payment_status":             paymentStatus,
			"total_tagihan":              totalTagihan,
			"total_pembayaran":           totalPembayaran,
			"total_perlengkapan":         totalPerlengkapan,
			"catatan_verifikasi_manager": pendaftaran.CatatanVerifikasiManager,
			"approved_by":                pendaftaran.ApprovedBy,
			"approved_at":                pendaftaran.ApprovedAt,
			"approver":                   approverObj,
			"approver_nama":              approverNama,
		},
		"paket": gin.H{
			"id":                pendaftaran.Paket.ID,
			"nama_paket":        pendaftaran.Paket.NamaPaket,
			"jenis_paket":       pendaftaran.Paket.JenisPaket,
			"harga":             pendaftaran.Paket.Harga,
			"tanggal_berangkat": pendaftaran.Paket.TanggalBerangkat,
			"durasi":            pendaftaran.Paket.Durasi,
			"deskripsi":         pendaftaran.Paket.Deskripsi,
		},
		"jamaah":     jamaahList,
		"pembayaran": pembayaranList,
	})
}

// ManagerApprovePendaftaran — POST /manager/verifikasi/:nomor/approve
// Menyetujui pendaftaran yang valid dan mengubah status menjadi "siap_berangkat"
func ManagerApprovePendaftaran(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).
		First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// 1. Validasi status: harus menunggu_verifikasi_manager
	if pendaftaran.Status != helpers.StatusMenungguVerifikasiManager {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": fmt.Sprintf("Tidak dapat menyetujui pendaftaran. Status saat ini '%s', bukan menunggu verifikasi manager.", pendaftaran.Status),
		})
		return
	}

	// 2. Ambil seluruh pendaftaran dalam grup (invoice yang sama)
	var pendaftaranList []models.Pendaftaran
	if pendaftaran.NomorInvoice != "" {
		config.DB.Where("nomor_invoice = ?", pendaftaran.NomorInvoice).Find(&pendaftaranList)
	} else {
		pendaftaranList = []models.Pendaftaran{pendaftaran}
	}

	// 3. Validasi pembayaran lunas
	if pendaftaran.NomorInvoice != "" {
		var invoice models.Invoice
		config.DB.Where("nomor_invoice = ?", pendaftaran.NomorInvoice).First(&invoice)
		if !helpers.IsInvoiceLunas(invoice.TotalPembayaran, invoice.TotalTagihan, invoice.StatusPembayaran) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": "Tidak dapat menyetujui pendaftaran. Pembayaran belum lunas.",
			})
			return
		}
	}

	// 4. Validasi dokumen seluruh jamaah lengkap
	for _, p := range pendaftaranList {
		var docs []models.Dokumen
		config.DB.Where("nomor_pendaftaran = ?", p.NomorPendaftaran).Find(&docs)
		if helpers.CalculateDocumentStatus(docs) != helpers.DocumentLengkap {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": fmt.Sprintf("Tidak dapat menyetujui pendaftaran. Dokumen jamaah (%s) belum lengkap.", p.NomorPendaftaran),
			})
			return
		}
	}

	// 5. Ambil ID Administration Manager dari konteks auth
	userIDStr, ok := c.Get("user_id")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User tidak terautentikasi"})
		return
	}
	managerID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID manager tidak valid"})
		return
	}

	now := time.Now()

	// 6. Update seluruh pendaftaran dalam invoice grup ke siap_berangkat
	for _, p := range pendaftaranList {
		config.DB.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", p.NomorPendaftaran).
			Updates(map[string]interface{}{
				"status":      helpers.StatusSiapBerangkat,
				"approved_by": managerID,
				"approved_at": now,
			})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "Pendaftaran berhasil disetujui dan disahkan.",
		"status":      helpers.StatusSiapBerangkat,
		"approved_by": managerID,
		"approved_at": now,
	})
}

// MintaPerbaikanRequest payload
type MintaPerbaikanRequest struct {
	Catatan string `json:"catatan"`
}

// ManagerMintaPerbaikan — POST /manager/verifikasi/:nomor/perbaikan
// Mengembalikan pendaftaran untuk diperbaiki oleh Admin (status: perlu_perbaikan)
func ManagerMintaPerbaikan(c *gin.Context) {
	nomor := c.Param("nomor")

	var req MintaPerbaikanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Catatan perbaikan wajib diisi."})
		return
	}

	catatan := strings.TrimSpace(req.Catatan)
	if catatan == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Catatan perbaikan wajib diisi."})
		return
	}

	var pendaftaran models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// 1. Validasi status: harus menunggu_verifikasi_manager
	if pendaftaran.Status != helpers.StatusMenungguVerifikasiManager {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": fmt.Sprintf("Tidak dapat meminta perbaikan. Status saat ini '%s', bukan menunggu verifikasi manager.", pendaftaran.Status),
		})
		return
	}

	// 2. Ambil seluruh anggota grup dalam invoice yang sama
	var pendaftaranList []models.Pendaftaran
	if pendaftaran.NomorInvoice != "" {
		config.DB.Where("nomor_invoice = ?", pendaftaran.NomorInvoice).Find(&pendaftaranList)
	} else {
		pendaftaranList = []models.Pendaftaran{pendaftaran}
	}

	// 3. Update status seluruh pendaftaran ke perlu_perbaikan dan simpan catatan
	for _, p := range pendaftaranList {
		config.DB.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", p.NomorPendaftaran).
			Updates(map[string]interface{}{
				"status":                     helpers.StatusPerluPerbaikan,
				"catatan_verifikasi_manager": catatan,
				"approved_by":                nil,
				"approved_at":                nil,
			})
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Permintaan perbaikan berhasil dikirim ke Admin.",
		"status":  helpers.StatusPerluPerbaikan,
		"catatan": catatan,
	})
}

// ManagerUploadDokumenPerjalanan — POST /manager/pendaftaran/:nomor/dokumen-perjalanan
// Upload dokumen perjalanan (visa, tiket_pesawat, nusuk) oleh Administration Manager
func ManagerUploadDokumenPerjalanan(c *gin.Context) {
	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	jenis := strings.ToLower(strings.TrimSpace(c.PostForm("jenis")))
	travelTypes := map[string]bool{
		"visa":          true,
		"tiket_pesawat": true,
		"nusuk":         true,
	}
	if !travelTypes[jenis] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Jenis dokumen perjalanan tidak valid. Gunakan: visa, tiket_pesawat, atau nusuk.",
		})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File dokumen wajib diupload."})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membaca file"})
		return
	}
	defer file.Close()

	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
	fileURL, err := helpers.UploadToSupabase(file, filename, "dokumen")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal upload file ke Supabase"})
		return
	}

	var existing models.Dokumen
	if err := config.DB.
		Where("nomor_pendaftaran = ? AND LOWER(jenis_dokumen) = ?", pendaftaran.NomorPendaftaran, jenis).
		First(&existing).Error; err == nil {
		// Update existing
		oldFile := existing.FilePath
		existing.FilePath = fileURL
		existing.StatusValidasi = helpers.PaymentVerificationDiterima
		existing.AlasanPenolakan = ""
		existing.CreatedAt = time.Now()
		if err := config.DB.Save(&existing).Error; err != nil {
			_ = helpers.DeleteFromSupabase(fileURL, "dokumen")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memperbarui dokumen"})
			return
		}
		if oldFile != "" {
			_ = helpers.DeleteFromSupabase(oldFile, "dokumen")
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Dokumen perjalanan berhasil diperbarui.",
			"data": gin.H{
				"id":     existing.ID,
				"jenis":  existing.JenisDokumen,
				"status": existing.StatusValidasi,
				"file":   existing.FilePath,
			},
		})
		return
	}

	// Create new
	dokumen := models.Dokumen{
		NomorPendaftaran: pendaftaran.NomorPendaftaran,
		JenisDokumen:     jenis,
		FilePath:         fileURL,
		StatusValidasi:   helpers.PaymentVerificationDiterima,
		CreatedAt:        time.Now(),
	}

	if err := config.DB.Create(&dokumen).Error; err != nil {
		_ = helpers.DeleteFromSupabase(fileURL, "dokumen")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan dokumen"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Dokumen perjalanan berhasil diupload.",
		"data": gin.H{
			"id":     dokumen.ID,
			"jenis":  dokumen.JenisDokumen,
			"status": dokumen.StatusValidasi,
			"file":   dokumen.FilePath,
		},
	})
}

// ManagerDeleteDokumenPerjalanan — DELETE /manager/dokumen-perjalanan/:id
func ManagerDeleteDokumenPerjalanan(c *gin.Context) {
	idStr := c.Param("id")
	docID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID dokumen tidak valid"})
		return
	}

	var dokumen models.Dokumen
	if err := config.DB.Where("id = ?", docID).First(&dokumen).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Dokumen tidak ditemukan"})
		return
	}

	travelMap := map[string]bool{
		"visa":          true,
		"tiket_pesawat": true,
		"nusuk":         true,
	}
	if !travelMap[strings.ToLower(dokumen.JenisDokumen)] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Hanya dokumen perjalanan yang dapat dihapus melalui endpoint ini."})
		return
	}

	_ = helpers.DeleteFromSupabase(dokumen.FilePath, "dokumen")
	config.DB.Delete(&dokumen)

	c.JSON(http.StatusOK, gin.H{
		"message": "Dokumen perjalanan berhasil dihapus.",
	})
}

// GetDokumenPerjalanan — GET /manager/dokumen-perjalanan/:nomor
func GetDokumenPerjalanan(c *gin.Context) {
	nomor := c.Param("nomor")

	var docs []models.Dokumen
	config.DB.
		Where("nomor_pendaftaran = ? AND LOWER(jenis_dokumen) IN (?)", nomor, []string{"visa", "tiket_pesawat", "nusuk"}).
		Order("created_at DESC").
		Find(&docs)

	var result []gin.H
	for _, d := range docs {
		result = append(result, gin.H{
			"id":         d.ID,
			"jenis":      d.JenisDokumen,
			"status":     d.StatusValidasi,
			"file":       d.FilePath,
			"created_at": d.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data": result,
	})
}

// GetManagerBadges — GET /manager/badges
func GetManagerBadges(c *gin.Context) {
	var countMenunggu int64
	var countPerluPerbaikan int64
	var countSiapBerangkat int64

	config.DB.Model(&models.Pendaftaran{}).
		Where("status = ?", helpers.StatusMenungguVerifikasiManager).
		Select("COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END)").
		Scan(&countMenunggu)

	config.DB.Model(&models.Pendaftaran{}).
		Where("status = ?", helpers.StatusPerluPerbaikan).
		Select("COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END)").
		Scan(&countPerluPerbaikan)

	config.DB.Model(&models.Pendaftaran{}).
		Where("status = ? AND approved_at IS NOT NULL", helpers.StatusSiapBerangkat).
		Select("COUNT(DISTINCT CASE WHEN nomor_invoice IS NOT NULL AND nomor_invoice <> '' THEN nomor_invoice ELSE nomor_pendaftaran END)").
		Scan(&countSiapBerangkat)

	c.JSON(http.StatusOK, gin.H{
		"menunggu_verifikasi": countMenunggu,
		"perlu_perbaikan":     countPerluPerbaikan,
		"siap_berangkat":      countSiapBerangkat,
	})
}
