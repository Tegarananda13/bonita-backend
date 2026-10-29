package customer

import (
	"fmt"
	"net/http"
	"strings"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
)

// GetInvoice — generate dan serve HTML invoice yang bisa di-print/save-as-PDF untuk customer portal.
func GetInvoice(c *gin.Context) {
	nomor := c.MustGet("pendaftaran_id").(string)

	// Ambil pendaftaran aktif (yang login)
	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		Preload("Approver").
		Where("nomor_pendaftaran = ?", nomor).First(&pendaftaran).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.NomorInvoice == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invoice belum tersedia."})
		return
	}
	invoice := pendaftaran.Invoice

	// Muat SEMUA pendaftaran dalam invoice yang sama
	var semuaPendaftaran []models.Pendaftaran
	config.DB.
		Preload("Customer").
		Where("nomor_invoice = ?", invoice.NomorInvoice).
		Order("tanggal_daftar ASC").
		Find(&semuaPendaftaran)

	// Ambil semua pembayaran diterima via nomor_invoice
	var pembayarans []models.Pembayaran
	config.DB.
		Where("nomor_invoice = ? AND status = ?", invoice.NomorInvoice, helpers.PaymentVerificationDiterima).
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

	tanggalInvoice := pendaftaran.TanggalDaftar
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

	// Hitung subtotal paket (tanpa perlengkapan)
	hargaPaketTotal := pendaftaran.Paket.Harga * float64(invoice.TotalOrang)
	totalPerlengkapan := invoice.TotalPerlengkapan

	// Bangun HTML perlengkapan untuk invoice
	var perlengkapanHTML string
	if totalPerlengkapan > 0 {
		perlengkapanHTML = fmt.Sprintf(
			"<div class=\"inv-total-card\" style=\"background:linear-gradient(135deg,#0a2e1c,#1a5c3d);margin-bottom:0.75rem\"><div><div class=\"inv-total-label\">🧳 Perlengkapan Tambahan Jamaah</div><div class=\"inv-total-meta\">%d jamaah × Rp %s</div></div><div class=\"inv-total-amount\">Rp %s</div></div>",
			jumlahAmbilPerlengkapan, helpers.FormatRupiah(pendaftaran.Paket.Harga), helpers.FormatRupiah(totalPerlengkapan),
		)
		// Fix: use actual harga perlengkapan per orang, not paket harga
		if jumlahAmbilPerlengkapan > 0 {
			hargaPerlPerOrang := totalPerlengkapan / float64(jumlahAmbilPerlengkapan)
			perlengkapanHTML = fmt.Sprintf(
				"<div class=\"inv-total-card\" style=\"background:linear-gradient(135deg,#0a2e1c,#1a5c3d);margin-bottom:0.75rem\"><div><div class=\"inv-total-label\">🧳 Perlengkapan Tambahan Jamaah</div><div class=\"inv-total-meta\">%d jamaah × Rp %s</div></div><div class=\"inv-total-amount\">Rp %s</div></div>",
				jumlahAmbilPerlengkapan, helpers.FormatRupiah(hargaPerlPerOrang), helpers.FormatRupiah(totalPerlengkapan),
			)
		}
	}

	var approvalHTML string
	if pendaftaran.ApprovedAt != nil && pendaftaran.Approver != nil {
		approvalHTML = helpers.BuildApprovalHTML(pendaftaran.Approver.Nama, pendaftaran.ApprovedAt)
	}

	html := helpers.BuildInvoiceHTML(
		invoice.NomorInvoice,
		helpers.FormatTanggal(tanggalInvoice),
		invoice.TotalOrang,
		jamaahHTML,
		pendaftaran.Paket.NamaPaket,
		pendaftaran.Paket.Durasi,
		helpers.FormatTanggal(pendaftaran.Paket.TanggalBerangkat),
		helpers.FormatRupiah(pendaftaran.Paket.Harga),
		invoice.TotalOrang,
		helpers.FormatRupiah(pendaftaran.Paket.Harga),
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
