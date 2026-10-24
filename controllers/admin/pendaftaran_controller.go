package admin

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// paymentStatusFromPendaftaran mengambil payment_status dari Invoice milik Pendaftaran.
func paymentStatusFromPendaftaran(p models.Pendaftaran) string {
	return helpers.PaymentStatusFromPendaftaran(p)
}

// sortPendaftaran mengurutkan pendaftaran secara deterministik.
func sortPendaftaran(list []models.Pendaftaran) {
	helpers.SortPendaftaran(list)
}

func GetAllPendaftaran(c *gin.Context) {
	// Sinkronisasi status pendaftaran aktif sebelum mengambil data
	helpers.SyncActivePendaftaran()

	query := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		Preload("Approver").
		Preload("Pembatal").
		Preload("User")

	adminIDParam := strings.TrimSpace(c.Query("admin_id"))
	if adminIDParam != "" {
		if parsedID, err := uuid.Parse(adminIDParam); err == nil {
			query = query.Where("user_id = ?", parsedID)
		}
	}

	statusParam := strings.TrimSpace(c.Query("status"))
	if statusParam != "" && strings.ToLower(statusParam) != "semua" {
		query = query.Where("LOWER(status) = ?", strings.ToLower(statusParam))
	}

	var pendaftaran []models.Pendaftaran
	if err := query.Find(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data pendaftaran"})
		return
	}

	searchParam := strings.TrimSpace(c.Query("search"))
	if searchParam != "" {
		s := strings.ToLower(searchParam)
		var filtered []models.Pendaftaran
		for _, p := range pendaftaran {
			namaCust := strings.ToLower(p.Customer.Nama)
			nomorP := strings.ToLower(p.NomorPendaftaran)
			namaPaket := strings.ToLower(p.Paket.NamaPaket)
			if strings.Contains(namaCust, s) || strings.Contains(nomorP, s) || strings.Contains(namaPaket, s) {
				filtered = append(filtered, p)
			}
		}
		pendaftaran = filtered
	}

	sortPendaftaran(pendaftaran)

	total := len(pendaftaran)

	pageStr := strings.TrimSpace(c.Query("page"))
	limitStr := strings.TrimSpace(c.Query("limit"))

	var paged []models.Pendaftaran
	var page, limit, totalPages int

	if pageStr != "" || limitStr != "" {
		page, _ = strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
		limit, _ = strconv.Atoi(limitStr)
		if limit < 1 {
			limit = 10
		}

		totalPages = int(math.Ceil(float64(total) / float64(limit)))
		if totalPages < 1 && total > 0 {
			totalPages = 1
		}

		start := (page - 1) * limit
		if start < total {
			end := start + limit
			if end > total {
				end = total
			}
			paged = pendaftaran[start:end]
		} else {
			paged = []models.Pendaftaran{}
		}
	} else {
		page = 1
		limit = total
		totalPages = 1
		paged = pendaftaran
	}

	var result []gin.H
	for _, p := range paged {
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
		var pembatalNama string
		if p.Pembatal != nil {
			pembatalNama = p.Pembatal.Nama
		}
		var adminPic string
		var adminID *uuid.UUID
		if p.UserID != nil {
			adminID = p.UserID
			if p.User.Nama != "" {
				adminPic = p.User.Nama
			}
		}
		result = append(result, gin.H{
			"nomor_pendaftaran":          p.NomorPendaftaran,
			"nomor_invoice":              nomorInvoice,
			"user_id":                    adminID,
			"admin_id":                   adminID,
			"admin_pic":                  adminPic,
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
			"alasan_pembatalan":          p.AlasanPembatalan,
			"dibatalkan_pada":            p.DibatalkanPada,
			"dibatalkan_oleh":            p.DibatalkanOleh,
			"dibatalkan_oleh_nama":       pembatalNama,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": total,
		"data":  result,
		"meta": gin.H{
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
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
	if current.Status != helpers.StatusPerluPerbaikan && current.Status != helpers.StatusBatal && current.Status != "batal" {
		// Pastikan status grup dan dokumen pendaftaran ini tersinkronisasi jika bukan dalam tahap perbaikan atau batal
		helpers.SyncPendaftaranStatusByNomor(nomor)
	}

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("User").
		Preload("Invoice").
		Preload("Approver").
		Preload("Pembatal").
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
		var tanggalLahir string
		if !gp.Customer.TanggalLahir.IsZero() {
			tanggalLahir = gp.Customer.TanggalLahir.Format("2006-01-02")
		}
		grupJamaah = append(grupJamaah, gin.H{
			"nomor_pendaftaran":  gp.NomorPendaftaran,
			"nama":               gp.Customer.Nama,
			"nik":                gp.Customer.NIK,
			"status":             gp.Status,
			"alasan_pembatalan":  gp.AlasanPembatalan,
			"ambil_perlengkapan": gp.AmbilPerlengkapan,
			"harga_perlengkapan": gp.HargaPerlengkapan,
			"no_hp":              gp.Customer.NoHP,
			"email":              gp.Customer.Email,
			"tempat_lahir":       gp.Customer.TempatLahir,
			"tanggal_lahir":      tanggalLahir,
			"jenis_kelamin":      gp.Customer.JenisKelamin,
			"alamat_lengkap":     gp.Customer.AlamatLengkap,
			"provinsi":           gp.Customer.Provinsi,
			"kabupaten_kota":     gp.Customer.KabupatenKota,
			"kecamatan":          gp.Customer.Kecamatan,
			"kelurahan_desa":     gp.Customer.KelurahanDesa,
			"kode_pos":           gp.Customer.KodePos,
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

	pembatalNama := ""
	if pendaftaran.Pembatal != nil {
		pembatalNama = pendaftaran.Pembatal.Nama
	}

	infoPerlengkapan := gin.H{
		"harga": helpers.DefaultHargaPerlengkapan,
		"nama":  "Paket Perlengkapan Tambahan Umroh",
		"items": []string{
			"Koper 22\"",
			"Koper 24\"",
			"Kain Ihram / Mukena",
			"Ikat Pinggang",
			"Buku Doa",
			"Tas Paspor / Tas Pinggang",
		},
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
			"alasan_pembatalan":          pendaftaran.AlasanPembatalan,
			"dibatalkan_pada":            pendaftaran.DibatalkanPada,
			"dibatalkan_oleh":            pendaftaran.DibatalkanOleh,
			"dibatalkan_oleh_nama":       pembatalNama,
		},
		"pembayaran":        pembayaran,
		"dokumen":           dokumen,
		"grup_jamaah":       grupJamaah,
		"info_perlengkapan": infoPerlengkapan,
	})
}

// AssignPendaftaran — PUT /admin/pendaftaran/:nomor/assign
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

	// Cek role pemanggil API
	userRole, _ := c.Get("role")
	roleStr, _ := userRole.(string)
	isManager := roleStr == "owner" || roleStr == "manager" || roleStr == "administration_manager"

	// Input opsional jika Administration Manager ingin menugaskan admin tertentu
	type AssignReq struct {
		AdminID string `json:"admin_id"`
	}
	var req AssignReq
	_ = c.ShouldBindJSON(&req)

	targetAdminID := userID

	if req.AdminID != "" && req.AdminID != "me" {
		if !isManager {
			c.JSON(http.StatusForbidden, gin.H{"error": "Hanya Administration Manager yang dapat menugaskan pendaftaran ke admin lain"})
			return
		}
		parsedTarget, err := uuid.Parse(req.AdminID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Admin ID tidak valid"})
			return
		}
		var targetUser models.User
		if err := config.DB.First(&targetUser, "id = ?", parsedTarget).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Admin yang dipilih tidak ditemukan"})
			return
		}
		targetAdminID = parsedTarget
	}

	if isManager {
		// Administration Manager berhak menugaskan atau mengganti PIC (reassign)
		pendaftaran.UserID = &targetAdminID
		if err := config.DB.Save(&pendaftaran).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan penugasan pendaftaran"})
			return
		}
	} else {
		// Admin biasa: hanya bisa mengambil pendaftaran yang BELUM memiliki PIC (user_id IS NULL)
		// Atomic update pada WHERE user_id IS NULL untuk mencegah race condition dua Admin mengambil bersamaan
		res := config.DB.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ? AND user_id IS NULL", nomor).
			Update("user_id", targetAdminID)

		if res.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil pendaftaran"})
			return
		}
		if res.RowsAffected == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Pendaftaran sudah ditangani admin"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Pendaftaran berhasil ditugaskan",
		"data": gin.H{
			"nomor_pendaftaran": pendaftaran.NomorPendaftaran,
			"admin_id":          targetAdminID,
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

	// 1. Validasi: pendaftaran tidak boleh berstatus batal
	if pendaftaran.Status == helpers.StatusBatal || pendaftaran.Status == "batal" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "Pendaftaran yang sudah dibatalkan tidak dapat diajukan untuk verifikasi",
		})
		return
	}

	// 2. Ambil seluruh pendaftaran dalam invoice grup atau individu
	var pendaftaranList []models.Pendaftaran
	if pendaftaran.NomorInvoice != "" {
		config.DB.Where("nomor_invoice = ?", pendaftaran.NomorInvoice).Find(&pendaftaranList)
	} else {
		pendaftaranList = []models.Pendaftaran{pendaftaran}
	}

	// 3. Validasi dokumen seluruh jamaah aktif
	for _, p := range pendaftaranList {
		if p.Status == helpers.StatusBatal || p.Status == "batal" || p.Status == helpers.StatusSelesai || p.Status == helpers.StatusKadaluarsa {
			continue
		}
		var docs []models.Dokumen
		config.DB.Where("nomor_pendaftaran = ?", p.NomorPendaftaran).Find(&docs)
		if helpers.CalculateDocumentStatus(docs) != helpers.DocumentLengkap {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": fmt.Sprintf("Dokumen untuk jamaah %s belum lengkap", p.NomorPendaftaran),
			})
			return
		}
	}

	// 4. Validasi pembayaran lunas
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

	// 5. Update status seluruh pendaftaran aktif menjadi menunggu_verifikasi_manager
	for _, p := range pendaftaranList {
		if p.Status == helpers.StatusBatal || p.Status == "batal" || p.Status == helpers.StatusSelesai || p.Status == helpers.StatusKadaluarsa {
			continue
		}
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

// GetPerlengkapanMaster mengembalikan informasi paket perlengkapan tambahan resmi
func GetPerlengkapanMaster(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"harga": helpers.DefaultHargaPerlengkapan,
		"nama":  "Paket Perlengkapan Tambahan Umroh",
		"items": []string{
			"Koper 22\"",
			"Koper 24\"",
			"Kain Ihram / Mukena",
			"Ikat Pinggang",
			"Buku Doa",
			"Tas Paspor / Tas Pinggang",
		},
	})
}

type UpdatePerlengkapanRequest struct {
	AmbilPerlengkapan bool `json:"ambil_perlengkapan"`
}

// AdminUpdatePerlengkapan memperbarui pilihan perlengkapan tambahan seorang jamaah
// dan menghitung ulang total perlengkapan, total tagihan, serta status pembayaran invoice.
func AdminUpdatePerlengkapan(c *gin.Context) {
	nomor := c.Param("nomor")
	var req UpdatePerlengkapanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format data tidak valid"})
		return
	}

	tx := config.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var pendaftaran models.Pendaftaran
	if err := tx.Preload("Paket").Preload("Invoice").Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// Tentukan harga perlengkapan snapshot dari harga master backend
	var hargaPerl float64 = 0
	if req.AmbilPerlengkapan {
		hargaPerl = float64(helpers.DefaultHargaPerlengkapan)
	}

	// Update pilihan perlengkapan pada pendaftaran jamaah ini saja
	if err := tx.Model(&models.Pendaftaran{}).
		Where("nomor_pendaftaran = ?", nomor).
		Updates(map[string]interface{}{
			"ambil_perlengkapan": req.AmbilPerlengkapan,
			"harga_perlengkapan": hargaPerl,
		}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan perubahan pilihan perlengkapan"})
		return
	}

	var newTotalPerlengkapan float64 = 0
	var newTotalTagihan float64 = 0
	var newTotalPembayaran float64 = 0
	var newInvoiceStatus string = models.InvoiceStatusBelumBayar

	if pendaftaran.NomorInvoice != "" {
		// Muat seluruh pendaftaran dalam invoice yang sama
		var semuaPendaftaran []models.Pendaftaran
		if err := tx.Where("nomor_invoice = ?", pendaftaran.NomorInvoice).Find(&semuaPendaftaran).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membaca grup pendaftaran"})
			return
		}

		for _, p := range semuaPendaftaran {
			if p.NomorPendaftaran == nomor {
				if req.AmbilPerlengkapan {
					newTotalPerlengkapan += hargaPerl
				}
			} else {
				if p.AmbilPerlengkapan {
					if p.HargaPerlengkapan > 0 {
						newTotalPerlengkapan += p.HargaPerlengkapan
					} else {
						newTotalPerlengkapan += float64(helpers.DefaultHargaPerlengkapan)
					}
				}
			}
		}

		totalOrang := len(semuaPendaftaran)
		if totalOrang == 0 {
			totalOrang = 1
		}
		newTotalTagihan = (pendaftaran.Paket.Harga * float64(totalOrang)) + newTotalPerlengkapan

		// Hitung total pembayaran yang valid dan sudah diverifikasi diterima
		tx.Model(&models.Pembayaran{}).
			Where("nomor_invoice = ? AND status = ?", pendaftaran.NomorInvoice, helpers.PaymentVerificationDiterima).
			Select("COALESCE(SUM(jumlah), 0)").
			Scan(&newTotalPembayaran)

		if newTotalPembayaran <= 0 {
			newInvoiceStatus = models.InvoiceStatusBelumBayar
		} else if newTotalTagihan > 0 && newTotalPembayaran >= newTotalTagihan {
			newInvoiceStatus = models.InvoiceStatusLunas
		} else {
			newInvoiceStatus = models.InvoiceStatusDP
		}

		if err := tx.Model(&models.Invoice{}).
			Where("nomor_invoice = ?", pendaftaran.NomorInvoice).
			Updates(map[string]interface{}{
				"total_perlengkapan": newTotalPerlengkapan,
				"total_tagihan":      newTotalTagihan,
				"total_pembayaran":   newTotalPembayaran,
				"status_pembayaran":  newInvoiceStatus,
				"total_orang":        totalOrang,
			}).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memperbarui total tagihan invoice"})
			return
		}
	} else {
		newTotalTagihan = pendaftaran.Paket.Harga + hargaPerl
		newTotalPerlengkapan = hargaPerl
	}

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal melakukan commit perubahan"})
		return
	}

	// Sinkronisasi status pendaftaran setelah invoice diperbarui
	if pendaftaran.NomorInvoice != "" {
		helpers.SyncPendaftaranStatus(pendaftaran.NomorInvoice)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":            "Pilihan perlengkapan berhasil diperbarui",
		"nomor_pendaftaran":  nomor,
		"ambil_perlengkapan": req.AmbilPerlengkapan,
		"harga_perlengkapan": hargaPerl,
		"total_perlengkapan": newTotalPerlengkapan,
		"total_tagihan":      newTotalTagihan,
		"total_pembayaran":   newTotalPembayaran,
		"status_pembayaran":  newInvoiceStatus,
	})
}

type BatalkanPendaftaranRequest struct {
	Alasan string `json:"alasan"`
}

// AdminBatalkanPendaftaran membatalkan pendaftaran jamaah secara permanen.
// Sesuai ketentuan bisnis Bonita Umroh:
// 1. TIDAK ADA REFUND. Invoice, pembayaran, dan nominal tetap utuh.
// 2. Transaksi atomic: status -> batal, catat alasan, waktu, admin ID, dan kurangi kuota terpakai paket.
// 3. Pada grup jamaah, hanya pendaftaran ini yang dibatalkan; anggota grup lain tetap aktif.
func AdminBatalkanPendaftaran(c *gin.Context) {
	nomor := c.Param("nomor")

	var req BatalkanPendaftaranRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format data tidak valid"})
		return
	}

	alasan := strings.TrimSpace(req.Alasan)
	if alasan == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Alasan pembatalan wajib diisi."})
		return
	}

	// Ambil Admin ID dari JWT token
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User tidak terautentikasi"})
		return
	}
	adminUUID, err := uuid.Parse(userIDVal.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID Admin tidak valid"})
		return
	}

	var adminUser models.User
	if err := config.DB.First(&adminUser, "id = ?", adminUUID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data Admin tidak ditemukan"})
		return
	}

	tx := config.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var pendaftaran models.Pendaftaran
	if err := tx.Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// Validasi aturan bisnis pembatalan:
	if pendaftaran.Status == helpers.StatusBatal || pendaftaran.Status == "batal" {
		tx.Rollback()
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pendaftaran sudah berstatus batal."})
		return
	}

	if pendaftaran.Status == helpers.StatusSelesai {
		tx.Rollback()
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pendaftaran yang sudah berstatus selesai tidak dapat dibatalkan."})
		return
	}

	if pendaftaran.Status == helpers.StatusKadaluarsa {
		tx.Rollback()
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pendaftaran sudah kadaluarsa dan tidak dapat dibatalkan."})
		return
	}

	now := time.Now()

	// 1. Update status dan metadata pembatalan pada pendaftaran jamaah ini saja
	if err := tx.Model(&models.Pendaftaran{}).
		Where("nomor_pendaftaran = ?", nomor).
		Updates(map[string]interface{}{
			"status":            helpers.StatusBatal,
			"alasan_pembatalan": alasan,
			"dibatalkan_pada":   now,
			"dibatalkan_oleh":   adminUUID,
		}).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan pembatalan pendaftaran"})
		return
	}

	// 2. Kembalikan kuota paket terpakai (kurangi 1 secara atomik, jaga agar tidak negatif)
	if err := tx.Model(&models.PaketUmroh{}).
		Where("id = ?", pendaftaran.PaketID).
		UpdateColumn("kuota_terpakai", gorm.Expr("GREATEST(kuota_terpakai - 1, 0)")).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengembalikan kuota paket"})
		return
	}

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal melakukan commit pembatalan"})
		return
	}

	// 3. Sinkronisasi status pendaftaran grup jika ada (jamaah lain tetap dievaluasi sesuai dokumen & pembayaran mereka)
	if pendaftaran.NomorInvoice != "" {
		helpers.SyncPendaftaranStatus(pendaftaran.NomorInvoice)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Pendaftaran berhasil dibatalkan",
		"data": gin.H{
			"nomor_pendaftaran":    pendaftaran.NomorPendaftaran,
			"status":               helpers.StatusBatal,
			"alasan_pembatalan":    alasan,
			"dibatalkan_pada":      now,
			"dibatalkan_oleh":      adminUUID,
			"dibatalkan_oleh_nama": adminUser.Nama,
		},
	})
}
