package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
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
		recalcInvoiceStatus(pembayaran.NomorInvoice)
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

	c.JSON(http.StatusOK, gin.H{
		"pembayaran": gin.H{
			"ID":              pembayaran.ID,
			"Jumlah":          pembayaran.Jumlah,
			"TanggalBayar":    pembayaran.TanggalBayar,
			"BuktiPembayaran": pembayaran.BuktiPembayaran,
			"Status":          pembayaran.Status,
			"AlasanPenolakan": pembayaran.AlasanPenolakan,
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

		result = append(result, gin.H{
			"id":                 p.ID,
			"nomor_pendaftaran":  nomorPendaftaran,
			"nomor_invoice":      p.NomorInvoice,
			"nama_customer":      namaCustomer,
			"jumlah":             p.Jumlah,
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
