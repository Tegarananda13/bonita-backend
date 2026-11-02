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
)

func VerifikasiPembayaran(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Status string `json:"status" binding:"required"`
		Alasan string `json:"alasan"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status wajib diisi"})
		return
	}

	if req.Status != helpers.PaymentVerificationDiterima && req.Status != helpers.PaymentVerificationDitolak {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Status hanya boleh diterima atau ditolak"})
		return
	}

	var pembayaran models.Pembayaran
	if err := config.DB.First(&pembayaran, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	if pembayaran.BuktiPembayaran == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Bukti pembayaran belum diupload"})
		return
	}

	// alasan wajib saat menolak; dikosongkan saat diterima
	alasan := strings.TrimSpace(req.Alasan)
	if req.Status == helpers.PaymentVerificationDitolak {
		if alasan == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Alasan penolakan wajib diisi"})
			return
		}
	} else {
		alasan = ""
	}

	if err := config.DB.
		Model(&pembayaran).
		Updates(map[string]interface{}{
			"status":           req.Status,
			"alasan_penolakan": alasan,
		}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal update status pembayaran"})
		return
	}
	pembayaran.Status = req.Status
	pembayaran.AlasanPenolakan = alasan

	// Recalc Invoice dan status pendaftaran
	if pembayaran.Status == helpers.PaymentVerificationDiterima ||
		pembayaran.Status == helpers.PaymentVerificationDitolak {
		helpers.SyncPendaftaranStatus(pembayaran.NomorInvoice)
	}

	var countMenunggu int64
	config.DB.Model(&models.Pembayaran{}).
		Where("status = ?", helpers.PaymentVerificationPending).
		Count(&countMenunggu)

	c.JSON(http.StatusOK, gin.H{
		"message": "Status pembayaran berhasil diupdate",
		"data": gin.H{
			"id":               pembayaran.ID,
			"status":           pembayaran.Status,
			"alasan_penolakan": pembayaran.AlasanPenolakan,
			"pending_count":    countMenunggu,
		},
	})
}

func GetDetailPembayaran(c *gin.Context) {
	id := c.Param("id")

	var pembayaran models.Pembayaran
	if err := config.DB.
		Preload("Invoice.Pendaftaran.Customer").
		Preload("Invoice.Pendaftaran.Paket").
		Preload("Invoice.Pendaftaran.User").
		First(&pembayaran, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	// Bangun daftar jamaah dari Invoice.Pendaftaran[]
	var jamaahList []gin.H
	var namaPaket string
	for i, p := range pembayaran.Invoice.Pendaftaran {
		jamaahList = append(jamaahList, gin.H{
			"Nama": p.Customer.Nama,
			"NIK":  p.Customer.NIK,
		})
		if i == 0 {
			namaPaket = p.Paket.NamaPaket
		}
	}
	if jamaahList == nil {
		jamaahList = []gin.H{}
	}

	normMetode, normBank, normSumber, metodeLabel := helpers.NormalizePembayaranMetadata(
		pembayaran.MetodePembayaran,
		pembayaran.BankTujuan,
		pembayaran.SumberPembayaran,
	)

	c.JSON(http.StatusOK, gin.H{
		"pembayaran": gin.H{
			"ID":                pembayaran.ID,
			"Jumlah":            pembayaran.Jumlah,
			"TanggalBayar":      pembayaran.TanggalBayar,
			"MetodePembayaran":  normMetode,
			"metode_pembayaran": normMetode,
			"BankTujuan":        normBank,
			"bank_tujuan":       normBank,
			"SumberPembayaran":  normSumber,
			"sumber_pembayaran": normSumber,
			"MetodeLabel":       metodeLabel,
			"metode_label":      metodeLabel,
			"BuktiPembayaran":   pembayaran.BuktiPembayaran,
			"Status":            pembayaran.Status,
			"AlasanPenolakan":   pembayaran.AlasanPenolakan,
			"Invoice": gin.H{
				"NomorInvoice":     pembayaran.Invoice.NomorInvoice,
				"TotalTagihan":     pembayaran.Invoice.TotalTagihan,
				"TotalPembayaran":  pembayaran.Invoice.TotalPembayaran,
				"StatusPembayaran": pembayaran.Invoice.StatusPembayaran,
				"TotalOrang":       pembayaran.Invoice.TotalOrang,
				"NamaPaket":        namaPaket,
				"Pendaftaran":      jamaahList,
			},
		},
	})
}

func GetPendingPembayaran(c *gin.Context) {
	var pembayaran []models.Pembayaran

	if err := config.DB.
		Preload("Invoice.Pendaftaran.Customer").
		Where("status = ?", helpers.PaymentVerificationPending).
		Order("tanggal_bayar DESC").
		Find(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil pembayaran"})
		return
	}

	var result []gin.H
	searchParam := strings.ToLower(strings.TrimSpace(c.Query("search")))

	for _, p := range pembayaran {
		// Ambil pendaftaran pertama dari invoice (untuk backward compat tampilkan info jamaah)
		nomorPendaftaran := ""
		namaCustomer := ""
		if len(p.Invoice.Pendaftaran) > 0 {
			nomorPendaftaran = p.Invoice.Pendaftaran[0].NomorPendaftaran
			namaCustomer = p.Invoice.Pendaftaran[0].Customer.Nama
		}

		if searchParam != "" {
			nomorP := strings.ToLower(nomorPendaftaran)
			namaC := strings.ToLower(namaCustomer)
			inv := strings.ToLower(p.NomorInvoice)
			if !strings.Contains(nomorP, searchParam) && !strings.Contains(namaC, searchParam) && !strings.Contains(inv, searchParam) {
				continue
			}
		}

		normMetode, normBank, normSumber, metodeLabel := helpers.NormalizePembayaranMetadata(
			p.MetodePembayaran,
			p.BankTujuan,
			p.SumberPembayaran,
		)

		result = append(result, gin.H{
			"id":                 p.ID,
			"nomor_pendaftaran":  nomorPendaftaran,
			"nomor_invoice":      p.NomorInvoice,
			"nama_customer":      namaCustomer,
			"jumlah":             p.Jumlah,
			"metode_pembayaran":  normMetode,
			"bank_tujuan":        normBank,
			"sumber_pembayaran":  normSumber,
			"metode_label":       metodeLabel,
			"status":             p.Status,
			"tanggal_pembayaran": p.TanggalBayar,
		})
	}

	var countMenunggu int64
	config.DB.Model(&models.Pembayaran{}).
		Where("status = ?", helpers.PaymentVerificationPending).
		Count(&countMenunggu)

	total := len(result)
	pageStr := strings.TrimSpace(c.Query("page"))
	limitStr := strings.TrimSpace(c.Query("limit"))

	var paged []gin.H
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
			paged = result[start:end]
		} else {
			paged = []gin.H{}
		}
	} else {
		page = 1
		limit = total
		totalPages = 1
		paged = result
	}

	c.JSON(http.StatusOK, gin.H{
		"total":          total,
		"count_menunggu": countMenunggu,
		"data":           paged,
		"meta": gin.H{
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": totalPages,
		},
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// ADMIN DETAIL PEMBAYARAN ACTIONS
// ─────────────────────────────────────────────────────────────────────────────

// isAuthorizedAdminRole memastikan pemanggil endpoint pembayaran manual adalah Admin/Manager yang berwenang
// dan bukan session customer atau role yang tidak memiliki izin.
func isAuthorizedAdminRole(c *gin.Context) bool {
	if _, isCustomer := c.Get("pendaftaran_id"); isCustomer {
		return false
	}
	if roleVal, exists := c.Get("role"); exists {
		roleStr, ok := roleVal.(string)
		if !ok {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(roleStr)) {
		case "admin", "owner", "manager", "administration_manager":
			return true
		default:
			return false
		}
	}
	return true
}

// extractAdminPaymentFields mengekstrak field input pembayaran manual Admin baik dari multipart/form-data,
// application/x-www-form-urlencoded, maupun application/json.
func extractAdminPaymentFields(c *gin.Context) (jumlahStr, tanggalStr, metodeStr, bankStr string, hasTanggal, hasMetode, hasBank bool) {
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.Contains(contentType, "application/json") {
		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err == nil {
			if v, ok := body["jumlah"]; ok && v != nil {
				jumlahStr = fmt.Sprintf("%v", v)
			}
			if v, ok := body["tanggal_bayar"]; ok {
				hasTanggal = true
				if v != nil {
					tanggalStr = fmt.Sprintf("%v", v)
				}
			}
			if v, ok := body["metode_pembayaran"]; ok {
				hasMetode = true
				if v != nil {
					metodeStr = fmt.Sprintf("%v", v)
				}
			}
			if v, ok := body["bank_tujuan"]; ok {
				hasBank = true
				if v != nil {
					bankStr = fmt.Sprintf("%v", v)
				}
			}
		}
		return
	}

	jumlahStr = c.PostForm("jumlah")
	tanggalStr, hasTanggal = c.GetPostForm("tanggal_bayar")
	metodeStr, hasMetode = c.GetPostForm("metode_pembayaran")
	bankStr, hasBank = c.GetPostForm("bank_tujuan")
	return
}

// AdminCreatePembayaran — POST /admin/pendaftaran/:nomor/pembayaran
// Admin mencatat pembayaran manual (Transfer BSI/Mandiri/BCA atau Tunai);
// status otomatis "diterima" dan sumber_pembayaran = "admin" tanpa antrean verifikasi.
func AdminCreatePembayaran(c *gin.Context) {
	if !isAuthorizedAdminRole(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Akses ditolak: hanya Admin yang dapat mencatat pembayaran manual"})
		return
	}

	nomor := c.Param("nomor")

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Paket").
		Preload("Invoice").
		Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.NomorInvoice == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invoice untuk pendaftaran ini belum tersedia"})
		return
	}

	invoice := pendaftaran.Invoice

	jumlahStr, tanggalStr, metodeStr, bankStr, hasTanggal, _, _ := extractAdminPaymentFields(c)

	var jumlah float64
	if _, err := fmt.Sscanf(strings.TrimSpace(jumlahStr), "%f", &jumlah); err != nil || helpers.ValidatePaymentNominal(jumlah) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Jumlah pembayaran tidak valid"})
		return
	}

	tanggalBayar := time.Now()
	if hasTanggal || strings.TrimSpace(tanggalStr) != "" {
		parsedDate, err := helpers.ValidatePaymentDate(tanggalStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		tanggalBayar = parsedDate
	}

	normMetode, normBank, err := helpers.ValidatePaymentMethodAndBank(metodeStr, bankStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var totalDiterima float64
	config.DB.
		Model(&models.Pembayaran{}).
		Where("nomor_invoice = ? AND status = ?", invoice.NomorInvoice, helpers.PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&totalDiterima)

	if totalDiterima == 0 && jumlah < 5_000_000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "DP pertama minimal Rp 5.000.000"})
		return
	}

	if totalDiterima+jumlah > invoice.TotalTagihan {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pembayaran melebihi total harga paket"})
		return
	}

	pembayaran := models.Pembayaran{
		NomorInvoice:     invoice.NomorInvoice,
		Jumlah:           jumlah,
		TanggalBayar:     tanggalBayar,
		MetodePembayaran: normMetode,
		BankTujuan:       normBank,
		SumberPembayaran: helpers.SumberPembayaranAdmin,
		Status:           helpers.PaymentVerificationDiterima,
	}

	if err := config.DB.Create(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan pembayaran"})
		return
	}

	// Upload bukti (opsional, hanya relevan untuk metode transfer)
	if normMetode == helpers.MetodePembayaranTransfer && !strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		fileHeader, fileErr := c.FormFile("bukti")
		if fileErr == nil {
			if file, err := fileHeader.Open(); err == nil {
				defer file.Close()
				filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
				if fileURL, err := helpers.UploadToSupabase(file, filename, "pembayaran"); err == nil {
					config.DB.Model(&pembayaran).Update("bukti_pembayaran", fileURL)
					pembayaran.BuktiPembayaran = fileURL
				}
			}
		}
	}

	helpers.SyncPendaftaranStatus(invoice.NomorInvoice)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Pembayaran berhasil ditambahkan",
		"data": gin.H{
			"id":                pembayaran.ID,
			"nomor_invoice":     pembayaran.NomorInvoice,
			"jumlah":            pembayaran.Jumlah,
			"tanggal_bayar":     pembayaran.TanggalBayar,
			"metode_pembayaran": pembayaran.MetodePembayaran,
			"bank_tujuan":       pembayaran.BankTujuan,
			"sumber_pembayaran": pembayaran.SumberPembayaran,
			"metode_label":      helpers.FormatPaymentMethodLabel(pembayaran.MetodePembayaran, pembayaran.BankTujuan),
			"status":            pembayaran.Status,
			"bukti":             pembayaran.BuktiPembayaran,
		},
	})
}

// AdminUpdatePembayaran — PUT /admin/pembayaran/:id/admin
func AdminUpdatePembayaran(c *gin.Context) {
	if !isAuthorizedAdminRole(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Akses ditolak: hanya Admin yang dapat mengubah pembayaran"})
		return
	}

	pembayaranIDStr := c.Param("id")
	pembayaranID, err := uuid.Parse(pembayaranIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID pembayaran tidak valid"})
		return
	}

	var pembayaran models.Pembayaran
	if err := config.DB.First(&pembayaran, "id = ?", pembayaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	jumlahStr, tanggalStr, metodeStr, bankStr, hasTanggal, hasMetode, hasBank := extractAdminPaymentFields(c)

	if strings.TrimSpace(jumlahStr) != "" {
		var jumlah float64
		if _, err := fmt.Sscanf(strings.TrimSpace(jumlahStr), "%f", &jumlah); err != nil || helpers.ValidatePaymentNominal(jumlah) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Jumlah pembayaran tidak valid"})
			return
		}
		pembayaran.Jumlah = jumlah
	}

	if hasTanggal || strings.TrimSpace(tanggalStr) != "" {
		t, err := helpers.ValidatePaymentDate(tanggalStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		pembayaran.TanggalBayar = t
	}

	if hasMetode || hasBank {
		targetMetode := metodeStr
		if !hasMetode {
			targetMetode = pembayaran.MetodePembayaran
		}
		targetBank := bankStr
		if !hasBank && strings.ToLower(strings.TrimSpace(targetMetode)) != helpers.MetodePembayaranTunai {
			targetBank = pembayaran.BankTujuan
		}
		normMetode, normBank, err := helpers.ValidatePaymentMethodAndBank(targetMetode, targetBank)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		pembayaran.MetodePembayaran = normMetode
		pembayaran.BankTujuan = normBank
	}

	if !strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		fileHeader, fileErr := c.FormFile("bukti")
		if fileErr == nil {
			if file, err := fileHeader.Open(); err == nil {
				defer file.Close()
				filename := fmt.Sprintf("%d_%s", time.Now().Unix(), fileHeader.Filename)
				if fileURL, err := helpers.UploadToSupabase(file, filename, "pembayaran"); err == nil {
					pembayaran.BuktiPembayaran = fileURL
				}
			}
		}
	}

	if err := config.DB.Save(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengupdate pembayaran"})
		return
	}

	helpers.SyncPendaftaranStatus(pembayaran.NomorInvoice)

	normMetode, normBank, normSumber, metodeLabel := helpers.NormalizePembayaranMetadata(
		pembayaran.MetodePembayaran,
		pembayaran.BankTujuan,
		pembayaran.SumberPembayaran,
	)

	c.JSON(http.StatusOK, gin.H{
		"message": "Pembayaran berhasil diupdate",
		"data": gin.H{
			"id":                pembayaran.ID,
			"jumlah":            pembayaran.Jumlah,
			"metode_pembayaran": normMetode,
			"bank_tujuan":       normBank,
			"sumber_pembayaran": normSumber,
			"metode_label":      metodeLabel,
			"status":            pembayaran.Status,
			"bukti":             pembayaran.BuktiPembayaran,
		},
	})
}

// AdminDeletePembayaran — DELETE /admin/pembayaran/:id/admin
func AdminDeletePembayaran(c *gin.Context) {
	pembayaranIDStr := c.Param("id")
	pembayaranID, err := uuid.Parse(pembayaranIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID pembayaran tidak valid"})
		return
	}

	var pembayaran models.Pembayaran
	if err := config.DB.First(&pembayaran, "id = ?", pembayaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pembayaran tidak ditemukan"})
		return
	}

	nomorInvoice := pembayaran.NomorInvoice
	_ = helpers.DeleteFromSupabase(pembayaran.BuktiPembayaran, "pembayaran")

	if err := config.DB.Delete(&pembayaran).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus pembayaran"})
		return
	}

	helpers.SyncPendaftaranStatus(nomorInvoice)
	c.JSON(http.StatusOK, gin.H{"message": "Pembayaran berhasil dihapus"})
}
