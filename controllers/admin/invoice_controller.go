package admin

import (
	"fmt"
	"net/http"
	"strings"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

// GetInvoiceAdmin — GET /admin/invoice?nomor=INV-xxx
// Diakses via admin token menggunakan query param nomor.
func GetInvoiceAdmin(c *gin.Context) {
	nomorInvoice := c.Query("nomor")
	if nomorInvoice == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nomor invoice wajib diisi"})
		return
	}

	// Ambil invoice
	var invoice models.Invoice
	if err := config.DB.Where("nomor_invoice = ?", nomorInvoice).First(&invoice).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invoice tidak ditemukan"})
		return
	}

	// Ambil semua pendaftaran dalam invoice ini
	var semuaPendaftaran []models.Pendaftaran
	config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Approver").
		Where("nomor_invoice = ?", nomorInvoice).
		Order("tanggal_daftar ASC").
		Find(&semuaPendaftaran)

	if len(semuaPendaftaran) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// Paket dari pendaftaran pertama (semua memiliki paket yang sama)
	firstPendaftaran := semuaPendaftaran[0]

	// Ambil semua pembayaran diterima
	var pembayarans []models.Pembayaran
	config.DB.
		Where("nomor_invoice = ? AND status = ?", nomorInvoice, helpers.PaymentVerificationDiterima).
		Order("tanggal_bayar ASC").
		Find(&pembayarans)

	var totalDibayar float64
	for _, p := range pembayarans {
		totalDibayar += p.Jumlah
	}

	statusBayar := "BELUM LUNAS"
	statusClass := "status-pending"
	if invoice.StatusPembayaran == models.InvoiceStatusLunas {
		statusBayar = "LUNAS"
		statusClass = "status-lunas"
	} else if totalDibayar > 0 {
		statusBayar = "DP / CICILAN"
		statusClass = "status-dp"
	}

	tanggalInvoice := firstPendaftaran.TanggalDaftar
	if len(pembayarans) > 0 {
		tanggalInvoice = pembayarans[0].TanggalBayar
	}

	var riwayatHTML strings.Builder
	for i, p := range pembayarans {
		label := "DP"
		if i > 0 {
			label = fmt.Sprintf("Pembayaran %d", i+1)
		}
		riwayatHTML.WriteString(fmt.Sprintf(
			"<tr><td class=\"tbl-date\">%s</td><td class=\"tbl-label\">%s</td><td class=\"tbl-amount\">Rp %s</td></tr>",
			p.TanggalBayar.Format("02 January 2006"), label, helpers.FormatRupiah(p.Jumlah),
		))
	}

	jamaahHTML, jumlahAmbilPerlengkapan := helpers.BuildDaftarJamaahHTML(semuaPendaftaran)

	hargaPaketTotal := firstPendaftaran.Paket.Harga * float64(invoice.TotalOrang)
	totalPerlengkapan := invoice.TotalPerlengkapan

	var perlengkapanHTML string
	if totalPerlengkapan > 0 && jumlahAmbilPerlengkapan > 0 {
		hargaPerlPerOrang := totalPerlengkapan / float64(jumlahAmbilPerlengkapan)
		perlengkapanHTML = fmt.Sprintf(
			"<div class=\"inv-total-card\" style=\"background:linear-gradient(135deg,#0a2e1c,#1a5c3d);margin-bottom:0.75rem\"><div><div class=\"inv-total-label\">🧳 Perlengkapan Tambahan Jamaah</div><div class=\"inv-total-meta\">%d jamaah × Rp %s</div></div><div class=\"inv-total-amount\">Rp %s</div></div>",
			jumlahAmbilPerlengkapan, helpers.FormatRupiah(hargaPerlPerOrang), helpers.FormatRupiah(totalPerlengkapan),
		)
	}

	var approvalHTML string
	if firstPendaftaran.ApprovedAt != nil && firstPendaftaran.Approver != nil {
		approvalHTML = helpers.BuildApprovalHTML(firstPendaftaran.Approver.Nama, firstPendaftaran.ApprovedAt)
	}

	html := helpers.BuildInvoiceHTML(
		invoice.NomorInvoice,
		helpers.FormatTanggal(tanggalInvoice),
		invoice.TotalOrang,
		jamaahHTML,
		firstPendaftaran.Paket.NamaPaket,
		firstPendaftaran.Paket.Durasi,
		helpers.FormatTanggal(firstPendaftaran.Paket.TanggalBerangkat),
		helpers.FormatRupiah(firstPendaftaran.Paket.Harga),
		invoice.TotalOrang,
		helpers.FormatRupiah(firstPendaftaran.Paket.Harga),
		helpers.FormatRupiah(hargaPaketTotal),
		perlengkapanHTML,
		helpers.FormatRupiah(invoice.TotalTagihan),
		riwayatHTML.String(),
		helpers.FormatRupiah(totalDibayar),
		statusClass,
		helpers.StatusIcon(statusBayar),
		statusBayar,
		approvalHTML,
	)

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"invoice-%s.html\"", invoice.NomorInvoice))
	c.String(http.StatusOK, html)
}
