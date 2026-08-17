package controllers

import (
	"net/http"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// jwtKey sudah dideklarasikan di auth_controller.go dalam package yang sama

// resolveRegistrationSource menentukan RegistrationSource dan RegisteredBy berdasarkan
// nilai registration_source dari request. Untuk source "admin", nama admin diambil
// dari JWT token pada header Authorization.
func resolveRegistrationSource(c *gin.Context, source string) (string, string) {
	switch strings.ToLower(source) {
	case helpers.SourceAdmin:
		// Coba ambil nama admin dari JWT
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				return jwtKey, nil
			})
			if err == nil && tok.Valid {
				if claims, ok := tok.Claims.(jwt.MapClaims); ok {
					if uidStr, ok := claims["user_id"].(string); ok {
						if uid, err := uuid.Parse(uidStr); err == nil {
							var u models.User
							if config.DB.First(&u, "id = ?", uid).Error == nil {
								return helpers.SourceAdmin, u.Nama
							}
						}
					}
				}
			}
		}
		return helpers.SourceAdmin, "Admin"
	case helpers.SourceChatbot:
		return helpers.SourceChatbot, "AI Chatbot"
	default:
		return helpers.SourceCustomer, "Self"
	}
}

// JamaahRequest adalah data satu jamaah dalam request pendaftaran.
type JamaahRequest struct {
	NIK           string `json:"nik"            binding:"required"`
	Nama          string `json:"nama"           binding:"required"`
	TempatLahir   string `json:"tempat_lahir"   binding:"required"`
	TanggalLahir  string `json:"tanggal_lahir"  binding:"required"` // YYYY-MM-DD
	JenisKelamin  string `json:"jenis_kelamin"  binding:"required"`
	NoHP          string `json:"no_hp"          binding:"required"`
	Email         string `json:"email"          binding:"required"`
	AlamatLengkap string `json:"alamat_lengkap" binding:"required"`
	Provinsi      string `json:"provinsi"       binding:"required"`
	KabupatenKota string `json:"kabupaten_kota" binding:"required"`
	Kecamatan     string `json:"kecamatan"      binding:"required"`
	KelurahanDesa string `json:"kelurahan_desa" binding:"required"`
	KodePos       string `json:"kode_pos"       binding:"required"`
}

// CreatePendaftaranRequest mendukung satu maupun banyak jamaah.
type CreatePendaftaranRequest struct {
	PaketID            string          `json:"paket_id" binding:"required"`
	Jamaah             []JamaahRequest `json:"jamaah"   binding:"required,min=1"`
	RegistrationSource string          `json:"registration_source"` // opsional: "customer" | "admin" | "chatbot"
}

// CreatePendaftaran — POST /pendaftaran
// Mendaftarkan satu atau banyak jamaah dalam satu Invoice.
// Mendukung tiga sumber: "customer" (default), "admin", "chatbot".
func CreatePendaftaran(c *gin.Context) {
	var req CreatePendaftaranRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data tidak lengkap atau format salah"})
		return
	}

	// Tentukan registration source sebelum proses data
	regSource, regBy := resolveRegistrationSource(c, req.RegistrationSource)

	// ── Validasi & trim setiap jamaah ────────────────────────────────────────
	for i := range req.Jamaah {
		j := &req.Jamaah[i]
		j.NIK           = strings.TrimSpace(j.NIK)
		j.Nama          = strings.TrimSpace(j.Nama)
		j.TempatLahir   = strings.TrimSpace(j.TempatLahir)
		j.JenisKelamin  = strings.TrimSpace(j.JenisKelamin)
		j.NoHP          = strings.TrimSpace(j.NoHP)
		j.Email         = strings.TrimSpace(j.Email)
		j.AlamatLengkap = strings.TrimSpace(j.AlamatLengkap)
		j.Provinsi      = strings.TrimSpace(j.Provinsi)
		j.KabupatenKota = strings.TrimSpace(j.KabupatenKota)
		j.Kecamatan     = strings.TrimSpace(j.Kecamatan)
		j.KelurahanDesa = strings.TrimSpace(j.KelurahanDesa)
		j.KodePos       = strings.TrimSpace(j.KodePos)

		// validasi NIK 16 digit angka
		if len(j.NIK) != 16 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "NIK jamaah ke-" + itoa(i+1) + " harus terdiri dari 16 digit"})
			return
		}
		for _, ch := range j.NIK {
			if ch < '0' || ch > '9' {
				c.JSON(http.StatusBadRequest, gin.H{"error": "NIK jamaah ke-" + itoa(i+1) + " hanya boleh berisi angka"})
				return
			}
		}

		// cek NIK duplikat dalam request
		for k := 0; k < i; k++ {
			if req.Jamaah[k].NIK == j.NIK {
				c.JSON(http.StatusBadRequest, gin.H{"error": "NIK jamaah ke-" + itoa(i+1) + " duplikat dalam request"})
				return
			}
		}

		// cek NIK sudah terdaftar di DB
		var existing models.Customer
		if err := config.DB.Where("nik = ?", j.NIK).First(&existing).Error; err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "NIK " + j.NIK + " sudah terdaftar dalam sistem"})
			return
		}

		// parse tanggal lahir
		tl, err := time.Parse("2006-01-02", j.TanggalLahir)
		if err != nil || tl.After(time.Now()) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Tanggal lahir jamaah ke-" + itoa(i+1) + " tidak valid"})
			return
		}
	}

	// ── Validasi paket ───────────────────────────────────────────────────────
	paketID, err := uuid.Parse(req.PaketID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "paket_id tidak valid"})
		return
	}

	var paket models.PaketUmroh
	if err := config.DB.First(&paket, "id = ?", paketID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket tidak ditemukan"})
		return
	}

	// cek paket selesai — tidak tersedia untuk pendaftaran baru
	if paket.IsFinished {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket umroh sudah selesai dan tidak tersedia untuk pendaftaran."})
		return
	}

	// cek paket nonaktif
	if !paket.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Paket umroh tidak aktif dan tidak tersedia untuk pendaftaran."})
		return
	}

	// cek batas pendaftaran
	batasDaftar := paket.TanggalBerangkat.AddDate(0, 0, -paket.BatasPendaftaran)
	if time.Now().After(batasDaftar) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pendaftaran untuk paket ini sudah ditutup"})
		return
	}

	// cek kuota — harus cukup untuk semua jamaah
	jumlahJamaah := len(req.Jamaah)
	if paket.KuotaTerpakai+jumlahJamaah > paket.KuotaMax {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Kuota paket tidak mencukupi untuk seluruh jamaah"})
		return
	}

	// ── Buat Invoice ─────────────────────────────────────────────────────────
	nomorInvoice, _ := helpers.GenerateNomorInvoice(config.DB)
	invoice := models.Invoice{
		NomorInvoice:     nomorInvoice,
		TotalOrang:       jumlahJamaah,                 // dihitung backend
		TotalTagihan:     paket.Harga * float64(jumlahJamaah),
		TotalPembayaran:  0,
		StatusPembayaran: models.InvoiceStatusBelumBayar,
	}
	if err := config.DB.Create(&invoice).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat invoice"})
		return
	}

	// ── Loop: buat Customer + Pendaftaran untuk setiap jamaah ────────────────
	type pendaftaranResult struct {
		NomorPendaftaran string `json:"nomor_pendaftaran"`
		Nama             string `json:"nama_customer"`
	}
	var results []pendaftaranResult

	for _, j := range req.Jamaah {
		tl, _ := time.Parse("2006-01-02", j.TanggalLahir)

		customer := models.Customer{
			NIK:           j.NIK,
			Nama:          j.Nama,
			TempatLahir:   j.TempatLahir,
			TanggalLahir:  tl,
			JenisKelamin:  j.JenisKelamin,
			NoHP:          j.NoHP,
			Email:         j.Email,
			AlamatLengkap: j.AlamatLengkap,
			Provinsi:      j.Provinsi,
			KabupatenKota: j.KabupatenKota,
			Kecamatan:     j.Kecamatan,
			KelurahanDesa: j.KelurahanDesa,
			KodePos:       j.KodePos,
			CreatedAt:     time.Now(),
		}
		if err := config.DB.Create(&customer).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat data customer: " + j.Nama})
			return
		}

		nomor := "UMR-" + time.Now().Format("20060102150405") + "-" + j.NIK[len(j.NIK)-4:]

		pendaftaran := models.Pendaftaran{
			CustomerID:         customer.ID,
			PaketID:            paketID,
			UserID:             nil,
			InvoiceID:          &invoice.ID,
			NomorPendaftaran:   nomor,
			DocumentStatus:     helpers.DocumentBelum,
			Status:             helpers.StatusProses,
			RegistrationSource: regSource,
			RegisteredBy:       regBy,
			TanggalDaftar:      time.Now(),
			BatasWaktuDP:       time.Now().Add(24 * time.Hour),
		}
		if err := config.DB.Create(&pendaftaran).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan pendaftaran: " + j.Nama})
			return
		}

		results = append(results, pendaftaranResult{
			NomorPendaftaran: nomor,
			Nama:             j.Nama,
		})
	}

	// ── Update kuota paket ───────────────────────────────────────────────────
	if err := config.DB.Model(&paket).Update("kuota_terpakai", paket.KuotaTerpakai+jumlahJamaah).Error; err != nil {
		// non-fatal
		_ = err
	}

	// ── Response ─────────────────────────────────────────────────────────────
	batasDP := time.Now().Add(24 * time.Hour)
	c.JSON(http.StatusCreated, gin.H{
		"message":        "Pendaftaran berhasil",
		"nomor_invoice":  nomorInvoice,
		"jumlah_jamaah":  jumlahJamaah,
		"total_tagihan":  invoice.TotalTagihan,
		"paket":          paket.NamaPaket,
		"pendaftaran":    results,
		"batas_waktu_dp": batasDP,
		// backward-compat: field lama tetap ada (diambil dari jamaah pertama)
		"data": gin.H{
			"nomor_pendaftaran": results[0].NomorPendaftaran,
			"nama_customer":     results[0].Nama,
			"paket":             paket.NamaPaket,
			"status":            helpers.StatusProses,
			"kuota_tersisa":     paket.KuotaMax - paket.KuotaTerpakai - jumlahJamaah,
			"batas_waktu_dp":    batasDP,
		},
	})
}

// itoa mengkonversi int ke string tanpa import strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte(n%10) + '0'
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func GetPendaftaranByNomor(c *gin.Context) {
	nomor := c.Param("nomor")

	otpInput := c.GetHeader("X-OTP")
	if otpInput == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OTP diperlukan"})
		return
	}

	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		First(&pendaftaran, "nomor_pendaftaran = ?", nomor).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	var otp models.VerifikasiOTP
	if err := config.DB.
		Where("pendaftaran_id = ?", pendaftaran.ID).
		Order("created_at DESC").
		First(&otp).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "OTP tidak ditemukan"})
		return
	}

	if otp.KodeOTP != otpInput {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OTP salah"})
		return
	}
	if otp.IsUsed {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OTP sudah digunakan"})
		return
	}
	if time.Now().After(otp.ExpiredAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OTP expired"})
		return
	}

	otp.IsUsed = true
	config.DB.Save(&otp)

	paymentStatus := models.InvoiceStatusBelumBayar
	if pendaftaran.InvoiceID != nil {
		paymentStatus = pendaftaran.Invoice.StatusPembayaran
	}

	c.JSON(http.StatusOK, gin.H{
		"nomor":           pendaftaran.NomorPendaftaran,
		"payment_status":  paymentStatus,
		"document_status": pendaftaran.DocumentStatus,
		"status":          pendaftaran.Status,
		"customer":        pendaftaran.Customer.Nama,
		"paket":           pendaftaran.Paket.NamaPaket,
		"tanggal":         pendaftaran.TanggalDaftar,
	})
}
