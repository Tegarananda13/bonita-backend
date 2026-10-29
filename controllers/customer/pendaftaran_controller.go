package customer

import (
	"net/http"
	"strings"
	"time"

	"bonita-backend/config"
	"bonita-backend/controllers/shared"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

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
				return shared.JWTKey, nil
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
	NIK               string `json:"nik"                 binding:"required"`
	Nama              string `json:"nama"                binding:"required"`
	TempatLahir       string `json:"tempat_lahir"        binding:"required"`
	TanggalLahir      string `json:"tanggal_lahir"       binding:"required"` // YYYY-MM-DD
	JenisKelamin      string `json:"jenis_kelamin"       binding:"required"`
	NoHP              string `json:"no_hp"               binding:"required"`
	Email             string `json:"email"               binding:"required"`
	AlamatLengkap     string `json:"alamat_lengkap"      binding:"required"`
	Provinsi          string `json:"provinsi"            binding:"required"`
	KabupatenKota     string `json:"kabupaten_kota"      binding:"required"`
	Kecamatan         string `json:"kecamatan"           binding:"required"`
	KelurahanDesa     string `json:"kelurahan_desa"      binding:"required"`
	KodePos           string `json:"kode_pos"            binding:"required"`
	AmbilPerlengkapan bool   `json:"ambil_perlengkapan"`
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
		j.NIK = strings.TrimSpace(j.NIK)
		j.Nama = strings.TrimSpace(j.Nama)
		j.TempatLahir = strings.TrimSpace(j.TempatLahir)
		j.JenisKelamin = strings.TrimSpace(j.JenisKelamin)
		j.NoHP = strings.TrimSpace(j.NoHP)
		j.Email = strings.TrimSpace(j.Email)
		j.AlamatLengkap = strings.TrimSpace(j.AlamatLengkap)
		j.Provinsi = strings.TrimSpace(j.Provinsi)
		j.KabupatenKota = strings.TrimSpace(j.KabupatenKota)
		j.Kecamatan = strings.TrimSpace(j.Kecamatan)
		j.KelurahanDesa = strings.TrimSpace(j.KelurahanDesa)
		j.KodePos = strings.TrimSpace(j.KodePos)

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

	_ = helpers.SyncFinishedPaket()

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

	// ── Hitung biaya perlengkapan ────────────────────────────────────────────
	hargaPerlengkapan := float64(helpers.DefaultHargaPerlengkapan)
	var totalPerlengkapan float64
	var jumlahAmbilPerlengkapan int
	for _, j := range req.Jamaah {
		if j.AmbilPerlengkapan {
			totalPerlengkapan += hargaPerlengkapan
			jumlahAmbilPerlengkapan++
		}
	}

	// ── Buat Invoice ─────────────────────────────────────────────────────────
	nomorInvoice, _ := helpers.GenerateNomorInvoice(config.DB)
	hargaPaketTotal := paket.Harga * float64(jumlahJamaah)
	invoice := models.Invoice{
		NomorInvoice:      nomorInvoice,
		TotalOrang:        jumlahJamaah,
		TotalTagihan:      hargaPaketTotal + totalPerlengkapan,
		TotalPerlengkapan: totalPerlengkapan,
		TotalPembayaran:   0,
		StatusPembayaran:  models.InvoiceStatusBelumBayar,
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

		// Snapshot harga perlengkapan jika jamaah memilih
		var hargaPerl float64
		if j.AmbilPerlengkapan {
			hargaPerl = hargaPerlengkapan
		}

		pendaftaran := models.Pendaftaran{
			CustomerNIK:        customer.NIK, // FK ke customer.nik
			PaketID:            paketID,
			UserID:             nil,
			NomorInvoice:       invoice.NomorInvoice,
			DocumentStatus:     helpers.DocumentBelum,
			Status:             helpers.StatusProses,
			RegistrationSource: regSource,
			RegisteredBy:       regBy,
			AmbilPerlengkapan:  j.AmbilPerlengkapan,
			HargaPerlengkapan:  hargaPerl,
			TanggalDaftar:      time.Now(),
			BatasWaktuDP:       time.Now().Add(24 * time.Hour),
		}
		if err := helpers.CreatePendaftaranWithRetry(config.DB, &pendaftaran, helpers.MaxGenerateRetries); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan pendaftaran: " + j.Nama})
			return
		}

		results = append(results, pendaftaranResult{
			NomorPendaftaran: pendaftaran.NomorPendaftaran,
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
		"message":             "Pendaftaran berhasil",
		"nomor_invoice":       nomorInvoice,
		"jumlah_jamaah":       jumlahJamaah,
		"total_tagihan":       invoice.TotalTagihan,
		"total_perlengkapan":  totalPerlengkapan,
		"jumlah_perlengkapan": jumlahAmbilPerlengkapan,
		"paket":               paket.NamaPaket,
		"pendaftaran":         results,
		"batas_waktu_dp":      batasDP,
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
		Where("nomor_pendaftaran = ?", pendaftaran.NomorPendaftaran).
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
	if pendaftaran.NomorInvoice != "" {
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

// UpdateCustomerJamaahRequest adalah payload untuk memperbarui biodata jamaah dari portal customer.
// Field internal seperti status, payment_status, document_status, user_id, role, approved_by
// sengaja tidak disertakan pada query update untuk mencegah mass assignment.
type UpdateCustomerJamaahRequest struct {
	Nama          string `json:"nama"`
	TempatLahir   string `json:"tempat_lahir"`
	TanggalLahir  string `json:"tanggal_lahir"` // YYYY-MM-DD
	JenisKelamin  string `json:"jenis_kelamin"`
	NoHP          string `json:"no_hp"`
	Email         string `json:"email"`
	AlamatLengkap string `json:"alamat_lengkap"`
	Provinsi      string `json:"provinsi"`
	KabupatenKota string `json:"kabupaten_kota"`
	Kecamatan     string `json:"kecamatan"`
	KelurahanDesa string `json:"kelurahan_desa"`
	KodePos       string `json:"kode_pos"`

	// Field identitas yang hanya diperiksa untuk mencegah manipulasi otorisasi (IDOR),
	// tidak pernah digunakan sebagai dasar penentuan target update.
	NIK              string `json:"nik"`
	CustomerNIK      string `json:"customer_nik"`
	CustomerID       string `json:"customer_id"`
	NomorPendaftaran string `json:"nomor_pendaftaran"`
}

func isValidPhoneCustomer(hp string) bool {
	if len(hp) == 0 {
		return false
	}
	digits := 0
	for i, ch := range hp {
		if i == 0 && ch == '+' {
			continue
		}
		if ch < '0' || ch > '9' {
			return false
		}
		digits++
	}
	return digits >= 9 && digits <= 15
}

func isValidEmailCustomer(email string) bool {
	if strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	atIdx := strings.IndexByte(email, '@')
	if atIdx <= 0 || atIdx != strings.LastIndexByte(email, '@') {
		return false
	}
	domain := email[atIdx+1:]
	dotIdx := strings.LastIndexByte(domain, '.')
	return dotIdx > 0 && dotIdx < len(domain)-1
}

func normalizeJenisKelaminCustomer(jk string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(jk)) {
	case "laki-laki":
		return "Laki-laki", true
	case "perempuan":
		return "Perempuan", true
	case "l":
		return "L", true
	case "p":
		return "P", true
	default:
		return "", false
	}
}

// UpdateCustomerJamaah — PUT /customer/jamaah
// Memperbarui biodata jamaah berdasarkan sesi portal customer yang sedang aktif.
func UpdateCustomerJamaah(c *gin.Context) {
	nomorSession := strings.TrimSpace(c.GetString("pendaftaran_id"))
	if nomorSession == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Sesi tidak valid atau telah berakhir"})
		return
	}

	var req UpdateCustomerJamaahRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data tidak lengkap atau format salah"})
		return
	}

	// Tolak jika frontend mencoba mengirimkan nomor_pendaftaran milik jamaah lain
	if reqNomor := strings.TrimSpace(req.NomorPendaftaran); reqNomor != "" && reqNomor != nomorSession {
		c.JSON(http.StatusForbidden, gin.H{"error": "Anda tidak memiliki izin untuk mengubah data pendaftaran jamaah lain"})
		return
	}

	// Cari pendaftaran berdasarkan sesi aktif
	var pendaftaran models.Pendaftaran
	if err := config.DB.First(&pendaftaran, "nomor_pendaftaran = ?", nomorSession).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// Cari data customer berdasarkan relasi pendaftaran.CustomerNIK
	var customer models.Customer
	if err := config.DB.First(&customer, "nik = ?", pendaftaran.CustomerNIK).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Data jamaah tidak ditemukan"})
		return
	}

	// Tolak jika frontend mencoba mengirimkan NIK / customer_id milik jamaah lain
	if reqNIK := strings.TrimSpace(req.NIK); reqNIK != "" && reqNIK != pendaftaran.CustomerNIK {
		c.JSON(http.StatusForbidden, gin.H{"error": "Anda tidak memiliki izin untuk mengubah data jamaah lain"})
		return
	}
	if reqCustNIK := strings.TrimSpace(req.CustomerNIK); reqCustNIK != "" && reqCustNIK != pendaftaran.CustomerNIK {
		c.JSON(http.StatusForbidden, gin.H{"error": "Anda tidak memiliki izin untuk mengubah data jamaah lain"})
		return
	}
	if reqCustID := strings.TrimSpace(req.CustomerID); reqCustID != "" && reqCustID != pendaftaran.CustomerNIK {
		c.JSON(http.StatusForbidden, gin.H{"error": "Anda tidak memiliki izin untuk mengubah data jamaah lain"})
		return
	}

	// Validasi field wilayah jika dikirim hanya berisi spasi
	if (len(req.Provinsi) > 0 && strings.TrimSpace(req.Provinsi) == "") ||
		(len(req.KabupatenKota) > 0 && strings.TrimSpace(req.KabupatenKota) == "") ||
		(len(req.Kecamatan) > 0 && strings.TrimSpace(req.Kecamatan) == "") ||
		(len(req.KelurahanDesa) > 0 && strings.TrimSpace(req.KelurahanDesa) == "") ||
		(len(req.KodePos) > 0 && strings.TrimSpace(req.KodePos) == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Field alamat wilayah tidak boleh hanya berisi spasi"})
		return
	}

	nama := strings.TrimSpace(req.Nama)
	tempatLahir := strings.TrimSpace(req.TempatLahir)
	tanggalLahirStr := strings.TrimSpace(req.TanggalLahir)
	jenisKelaminRaw := strings.TrimSpace(req.JenisKelamin)
	noHP := strings.TrimSpace(req.NoHP)
	email := strings.TrimSpace(req.Email)
	alamatLengkap := strings.TrimSpace(req.AlamatLengkap)

	if nama == "" || tempatLahir == "" || tanggalLahirStr == "" ||
		jenisKelaminRaw == "" || noHP == "" || email == "" || alamatLengkap == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Semua field biodata wajib diisi"})
		return
	}

	if len([]rune(nama)) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nama lengkap minimal 2 karakter"})
		return
	}

	tl, err := time.Parse("2006-01-02", tanggalLahirStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format tanggal lahir tidak valid (gunakan YYYY-MM-DD)"})
		return
	}
	if tl.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tanggal lahir tidak boleh melebihi tanggal hari ini"})
		return
	}

	jenisKelamin, ok := normalizeJenisKelaminCustomer(jenisKelaminRaw)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Jenis kelamin tidak valid (pilih Laki-laki atau Perempuan)"})
		return
	}

	if !isValidPhoneCustomer(noHP) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nomor HP tidak valid (gunakan 10-15 digit angka)"})
		return
	}

	if !isValidEmailCustomer(email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format email tidak valid"})
		return
	}

	provinsi := strings.TrimSpace(req.Provinsi)
	if provinsi == "" {
		provinsi = customer.Provinsi
	}
	kabupatenKota := strings.TrimSpace(req.KabupatenKota)
	if kabupatenKota == "" {
		kabupatenKota = customer.KabupatenKota
	}
	kecamatan := strings.TrimSpace(req.Kecamatan)
	if kecamatan == "" {
		kecamatan = customer.Kecamatan
	}
	kelurahanDesa := strings.TrimSpace(req.KelurahanDesa)
	if kelurahanDesa == "" {
		kelurahanDesa = customer.KelurahanDesa
	}
	kodePos := strings.TrimSpace(req.KodePos)
	if kodePos != "" {
		if len(kodePos) != 5 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Kode pos harus terdiri dari 5 digit angka"})
			return
		}
		for _, ch := range kodePos {
			if ch < '0' || ch > '9' {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Kode pos harus terdiri dari 5 digit angka"})
				return
			}
		}
	} else {
		kodePos = customer.KodePos
	}

	tx := config.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memulai transaksi database"})
		return
	}

	updates := map[string]interface{}{
		"nama":           nama,
		"tempat_lahir":   tempatLahir,
		"tanggal_lahir":  tl,
		"jenis_kelamin":  jenisKelamin,
		"no_hp":          noHP,
		"email":          email,
		"alamat_lengkap": alamatLengkap,
		"provinsi":       provinsi,
		"kabupaten_kota": kabupatenKota,
		"kecamatan":      kecamatan,
		"kelurahan_desa": kelurahanDesa,
		"kode_pos":       kodePos,
	}

	if err := tx.Model(&models.Customer{}).
		Where("nik = ?", pendaftaran.CustomerNIK).
		Updates(updates).Error; err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan perubahan data jamaah"})
		return
	}

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan perubahan data jamaah"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":           "Data jamaah berhasil diperbarui",
		"nomor_pendaftaran": pendaftaran.NomorPendaftaran,
		"nomor_invoice":     pendaftaran.NomorInvoice,
		"status":            pendaftaran.Status,
		"document_status":   pendaftaran.DocumentStatus,
		"customer": gin.H{
			"nik":            customer.NIK,
			"nama":           nama,
			"tempat_lahir":   tempatLahir,
			"tanggal_lahir":  tl,
			"jenis_kelamin":  jenisKelamin,
			"no_hp":          noHP,
			"email":          email,
			"alamat_lengkap": alamatLengkap,
			"provinsi":       provinsi,
			"kabupaten_kota": kabupatenKota,
			"kecamatan":      kecamatan,
			"kelurahan_desa": kelurahanDesa,
			"kode_pos":       kodePos,
		},
	})
}
