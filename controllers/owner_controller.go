package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ── CreateAdmin — POST /owner/admin ──────────────────────────────────────────
func CreateAdmin(c *gin.Context) {

	var req struct {
		Nama     string `json:"nama"     binding:"required"`
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		NoHP     string `json:"no_hp"`
		Email    string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data tidak valid"})
		return
	}

	// Sanitasi
	req.Nama     = strings.TrimSpace(req.Nama)
	req.Username = strings.TrimSpace(req.Username)
	req.NoHP     = strings.TrimSpace(req.NoHP)
	req.Email    = strings.TrimSpace(req.Email)

	if req.Nama == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nama lengkap wajib diisi"})
		return
	}
	if req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username wajib diisi"})
		return
	}
	if len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password minimal 6 karakter"})
		return
	}

	// Cek duplikat username
	var existing models.User
	if err := config.DB.Where("username = ?", req.Username).First(&existing).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username sudah digunakan"})
		return
	}

	// Hash password
	hashedPassword, err := helpers.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal memproses password"})
		return
	}

	admin := models.User{
		Nama:      req.Nama,
		Username:  req.Username,
		Password:  hashedPassword,
		Role:      "admin",
		NoHP:      req.NoHP,
		Email:     req.Email,
		IsActive:  true,
		CreatedAt: time.Now(),
	}

	if err := config.DB.Create(&admin).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal membuat admin"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Admin berhasil dibuat",
		"data": gin.H{
			"id":         admin.ID,
			"nama":       admin.Nama,
			"username":   admin.Username,
			"role":       admin.Role,
			"no_hp":      admin.NoHP,
			"email":      admin.Email,
			"is_active":  admin.IsActive,
			"created_at": admin.CreatedAt,
		},
	})
}

// ── GetAdminList — GET /owner/admin ──────────────────────────────────────────
func GetAdminList(c *gin.Context) {

	var admins []models.User

	if err := config.DB.
		Where("role = ?", "admin").
		Order("created_at DESC").
		Find(&admins).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data admin"})
		return
	}

	var result []gin.H
	for _, admin := range admins {
		result = append(result, gin.H{
			"id":         admin.ID,
			"nama":       admin.Nama,
			"username":   admin.Username,
			"role":       admin.Role,
			"no_hp":      admin.NoHP,
			"email":      admin.Email,
			"is_active":  admin.IsActive,
			"created_at": admin.CreatedAt,
		})
	}

	if result == nil {
		result = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{"admins": result})
}

// ── GetAdminDetail — GET /owner/admin/:id ────────────────────────────────────
func GetAdminDetail(c *gin.Context) {

	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":         admin.ID,
			"nama":       admin.Nama,
			"username":   admin.Username,
			"role":       admin.Role,
			"no_hp":      admin.NoHP,
			"email":      admin.Email,
			"is_active":  admin.IsActive,
			"created_at": admin.CreatedAt,
		},
	})
}

// ── UpdateAdmin — PUT /owner/admin/:id ───────────────────────────────────────
func UpdateAdmin(c *gin.Context) {

	id := c.Param("id")

	var req struct {
		Nama     string `json:"nama"`
		Username string `json:"username"`
		NoHP     string `json:"no_hp"`
		Email    string `json:"email"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data tidak valid"})
		return
	}

	req.Nama     = strings.TrimSpace(req.Nama)
	req.Username = strings.TrimSpace(req.Username)
	req.NoHP     = strings.TrimSpace(req.NoHP)
	req.Email    = strings.TrimSpace(req.Email)

	if req.Nama == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nama lengkap wajib diisi"})
		return
	}
	if req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username wajib diisi"})
		return
	}

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	// Cek duplikat username (kecuali untuk dirinya sendiri)
	if req.Username != admin.Username {
		var existing models.User
		if err := config.DB.Where("username = ? AND id != ?", req.Username, id).First(&existing).Error; err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username sudah digunakan"})
			return
		}
	}

	if err := config.DB.Model(&admin).Updates(map[string]interface{}{
		"nama":     req.Nama,
		"username": req.Username,
		"no_hp":    req.NoHP,
		"email":    req.Email,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengupdate data admin"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Data admin berhasil diperbarui",
		"data": gin.H{
			"id":         admin.ID,
			"nama":       req.Nama,
			"username":   req.Username,
			"role":       admin.Role,
			"no_hp":      req.NoHP,
			"email":      req.Email,
			"is_active":  admin.IsActive,
			"created_at": admin.CreatedAt,
		},
	})
}

// ── DeactivateAdmin — PATCH /owner/admin/:id/deactivate ──────────────────────
func DeactivateAdmin(c *gin.Context) {

	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	if !admin.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Admin sudah nonaktif"})
		return
	}

	if err := config.DB.Model(&admin).Update("is_active", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menonaktifkan admin"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Admin berhasil dinonaktifkan"})
}

// ── ReactivateAdmin — PATCH /owner/admin/:id/reactivate ──────────────────────
func ReactivateAdmin(c *gin.Context) {

	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ? AND role = ?", id, "admin").Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	if admin.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Admin sudah aktif"})
		return
	}

	if err := config.DB.Model(&admin).Update("is_active", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengaktifkan kembali admin"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Admin berhasil diaktifkan kembali"})
}

// ── DeleteAdmin — DELETE /owner/admin/:id (hard delete, tetap tersedia) ──────
func DeleteAdmin(c *gin.Context) {

	id := c.Param("id")

	var admin models.User
	if err := config.DB.First(&admin, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Admin tidak ditemukan"})
		return
	}

	if admin.Role != "admin" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ini bukan admin"})
		return
	}

	if err := config.DB.Delete(&admin).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus admin"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Admin berhasil dihapus"})
}

// ── GetLaporan — GET /owner/laporan ──────────────────────────────────────────
// Query params: ?start_date=2026-09-01&end_date=2026-09-30
func GetLaporan(c *gin.Context) {

	startStr := c.Query("start_date")
	endStr   := c.Query("end_date")

	if startStr == "" || endStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_date dan end_date wajib diisi"})
		return
	}

	// Parse ke time.Time — format "2006-01-02"
	const layout = "2006-01-02"
	startDate, err := time.ParseInLocation(layout, startStr, time.Local)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format start_date tidak valid (gunakan YYYY-MM-DD)"})
		return
	}
	endDate, err := time.ParseInLocation(layout, endStr, time.Local)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format end_date tidak valid (gunakan YYYY-MM-DD)"})
		return
	}
	if endDate.Before(startDate) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tanggal akhir tidak boleh sebelum tanggal mulai"})
		return
	}
	// End of day
	endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	// ── 1. Total Pendaftaran pada periode ──
	var totalPendaftaran int64
	config.DB.Model(&models.Pendaftaran{}).
		Where("tanggal_daftar BETWEEN ? AND ?", startDate, endDate).
		Count(&totalPendaftaran)

	// ── 2. Total Pembayaran (hanya yang diterima) pada periode ──
	var totalPembayaranDiterima float64
	config.DB.Model(&models.Pembayaran{}).
		Select("COALESCE(SUM(jumlah), 0)").
		Where("tanggal_bayar BETWEEN ? AND ? AND status = ?", startDate, endDate, helpers.PaymentVerificationDiterima).
		Scan(&totalPembayaranDiterima)

	// ── 3. Total Tagihan (dari invoice yang terhubung ke pendaftaran periode) ──
	var totalTagihan float64
	config.DB.Model(&models.Invoice{}).
		Select("COALESCE(SUM(i.total_tagihan), 0)").
		Joins("i").
		Where("EXISTS (SELECT 1 FROM pendaftaran p WHERE p.nomor_invoice = invoice.nomor_invoice AND p.tanggal_daftar BETWEEN ? AND ?)", startDate, endDate).
		Scan(&totalTagihan)

	// Alternatif query tagihan yang lebih straightforward
	if totalTagihan == 0 {
		config.DB.Raw(`
			SELECT COALESCE(SUM(i.total_tagihan), 0)
			FROM invoice i
			WHERE EXISTS (
				SELECT 1 FROM pendaftaran p
				WHERE p.nomor_invoice = i.nomor_invoice
				  AND p.tanggal_daftar BETWEEN ? AND ?
			)
		`, startDate, endDate).Scan(&totalTagihan)
	}

	// ── 4. Total Pembayaran Semua Status dalam periode ──
	var totalPembayaranAllPeriod float64
	config.DB.Raw(`
		SELECT COALESCE(SUM(jumlah), 0) FROM pembayaran
		WHERE tanggal_bayar BETWEEN ? AND ? AND status = ?
	`, startDate, endDate, helpers.PaymentVerificationDiterima).Scan(&totalPembayaranAllPeriod)

	// ── 5. Sisa Tagihan (tagihan - pembayaran diterima; tidak negatif) ──
	sisaTagihan := totalTagihan - totalPembayaranDiterima
	if sisaTagihan < 0 {
		sisaTagihan = 0
	}

	// ── 6. Pembayaran Menunggu Verifikasi dalam periode ──
	var pembayaranMenunggu int64
	config.DB.Model(&models.Pembayaran{}).
		Where("tanggal_bayar BETWEEN ? AND ? AND status = ?", startDate, endDate, helpers.PaymentVerificationPending).
		Count(&pembayaranMenunggu)

	// ── 7. Rekapitulasi Pendaftaran per Paket ──
	type PendaftaranPerPaket struct {
		PaketID      string  `json:"paket_id"`
		NamaPaket    string  `json:"nama_paket"`
		JumlahJamaah int64   `json:"jumlah_jamaah"`
		KuotaMax     int     `json:"kuota_max"`
		KuotaTerpakai int    `json:"kuota_terpakai"`
		IsActive     bool    `json:"is_active"`
		IsFinished   bool    `json:"is_finished"`
	}
	var rekap []PendaftaranPerPaket
	config.DB.Raw(`
		SELECT
			pu.id          AS paket_id,
			pu.nama_paket,
			COUNT(p.nomor_pendaftaran) AS jumlah_jamaah,
			pu.kuota_max,
			pu.kuota_terpakai,
			pu.is_active,
			pu.is_finished
		FROM paket_umroh pu
		LEFT JOIN pendaftaran p
			ON p.paket_id = pu.id
			AND p.tanggal_daftar BETWEEN ? AND ?
		GROUP BY pu.id, pu.nama_paket, pu.kuota_max, pu.kuota_terpakai, pu.is_active, pu.is_finished
		ORDER BY jumlah_jamaah DESC, pu.nama_paket ASC
	`, startDate, endDate).Scan(&rekap)
	if rekap == nil {
		rekap = []PendaftaranPerPaket{}
	}

	// ── 8. Rekapitulasi Pembayaran per Status ──
	type PembayaranPerStatus struct {
		Status           string  `json:"status"`
		JumlahTransaksi  int64   `json:"jumlah_transaksi"`
		TotalNominal     float64 `json:"total_nominal"`
	}
	var rekapPembayaran []PembayaranPerStatus
	config.DB.Raw(`
		SELECT
			status,
			COUNT(*) AS jumlah_transaksi,
			COALESCE(SUM(jumlah), 0) AS total_nominal
		FROM pembayaran
		WHERE tanggal_bayar BETWEEN ? AND ?
		GROUP BY status
		ORDER BY status
	`, startDate, endDate).Scan(&rekapPembayaran)
	if rekapPembayaran == nil {
		rekapPembayaran = []PembayaranPerStatus{}
	}

	// ── 9. Tren Pendaftaran per Bulan dalam periode ──
	type TrenBulan struct {
		Periode  string `json:"periode"`  // format "YYYY-MM"
		Label    string `json:"label"`    // format "Jan 2026"
		Jumlah   int64  `json:"jumlah"`
	}
	var tren []TrenBulan
	config.DB.Raw(`
		SELECT
			TO_CHAR(tanggal_daftar, 'YYYY-MM') AS periode,
			TO_CHAR(tanggal_daftar, 'Mon YYYY') AS label,
			COUNT(*) AS jumlah
		FROM pendaftaran
		WHERE tanggal_daftar BETWEEN ? AND ?
		GROUP BY TO_CHAR(tanggal_daftar, 'YYYY-MM'), TO_CHAR(tanggal_daftar, 'Mon YYYY')
		ORDER BY periode ASC
	`, startDate, endDate).Scan(&tren)
	if tren == nil {
		tren = []TrenBulan{}
	}

	c.JSON(http.StatusOK, gin.H{
		"periode": gin.H{
			"start_date": startStr,
			"end_date":   endStr,
		},
		"ringkasan": gin.H{
			"total_pendaftaran":    totalPendaftaran,
			"total_pembayaran":     totalPembayaranDiterima,
			"total_tagihan":        totalTagihan,
			"sisa_tagihan":         sisaTagihan,
			"pembayaran_menunggu":  pembayaranMenunggu,
		},
		"pendaftaran":     rekap,
		"pembayaran":      rekapPembayaran,
		"tren_pendaftaran": tren,
	})
}
