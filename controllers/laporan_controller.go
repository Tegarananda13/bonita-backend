package controllers

import (
	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

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
