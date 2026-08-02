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
)

// GetInvoice — generate dan serve HTML invoice yang bisa di-print/save-as-PDF
func GetInvoice(c *gin.Context) {
	pendaftaranID := c.MustGet("pendaftaran_id")

	// Ambil pendaftaran aktif (yang login)
	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		Preload("Invoice").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	if pendaftaran.InvoiceID == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invoice belum tersedia."})
		return
	}
	invoice := pendaftaran.Invoice

	// Muat SEMUA pendaftaran dalam invoice yang sama
	var semuaPendaftaran []models.Pendaftaran
	config.DB.
		Preload("Customer").
		Where("invoice_id = ?", invoice.ID).
		Order("tanggal_daftar ASC").
		Find(&semuaPendaftaran)

	// Ambil semua pembayaran diterima via invoice_id
	var pembayarans []models.Pembayaran
	config.DB.
		Where("invoice_id = ? AND status = ?", invoice.ID, helpers.PaymentVerificationDiterima).
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
			p.TanggalBayar.Format("02 January 2006"), label, formatRupiah(p.Jumlah),
		))
	}

	var jamaahHTML strings.Builder
	for i, pd := range semuaPendaftaran {
		jamaahHTML.WriteString(fmt.Sprintf(
			"<div class=\"inv-info-item\"><div class=\"inv-info-label\">Jamaah %d</div><div class=\"inv-info-val\">%s</div></div>"+
			"<div class=\"inv-info-item\"><div class=\"inv-info-label\">Nomor UMR %d</div><div class=\"inv-info-val\" style=\"font-family:'Courier New',monospace\">%s</div></div>",
			i+1, pd.Customer.Nama, i+1, pd.NomorPendaftaran,
		))
	}

	html := buildInvoiceHTML(
		invoice.NomorInvoice,
		formatTanggal(tanggalInvoice),
		invoice.TotalOrang,
		jamaahHTML.String(),
		pendaftaran.Paket.NamaPaket,
		pendaftaran.Paket.Durasi,
		formatTanggal(pendaftaran.Paket.TanggalBerangkat),
		formatRupiah(pendaftaran.Paket.Harga),
		invoice.TotalOrang,
		formatRupiah(pendaftaran.Paket.Harga),
		formatRupiah(invoice.TotalTagihan),
		riwayatHTML.String(),
		formatRupiah(totalDibayar),
		statusClass,
		statusIcon(statusBayar),
		statusBayar,
	)

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"invoice-%s.html\"", invoice.NomorInvoice))
	c.String(http.StatusOK, html)
}


// ── helpers lokal ──

func formatRupiah(amount float64) string {
	// Format angka dengan titik ribuan
	intPart := int64(amount)
	s := fmt.Sprintf("%d", intPart)
	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += "."
		}
		result += string(c)
	}
	return result
}

func formatTanggal(t time.Time) string {
	months := []string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
		"Juli", "Agustus", "September", "Oktober", "November", "Desember"}
	return fmt.Sprintf("%02d %s %d", t.Day(), months[t.Month()], t.Year())
}

func statusIcon(s string) string {
	switch s {
	case "LUNAS":
		return "✅"
	case "DP / CICILAN":
		return "🔄"
	default:
		return "⏳"
	}
}

// buildInvoiceHTML membangun HTML invoice dengan template lengkap.
func buildInvoiceHTML(
	nomorInvoice string,
	tanggal string,
	totalOrang int,
	jamaahHTML string,
	namaPaket string,
	durasi int,
	tanggalBerangkat string,
	hargaPerOrang string,
	jumlahOrang int,
	hargaPerOrangAlt string,
	totalTagihan string,
	riwayatHTML string,
	totalDibayar string,
	statusClass string,
	statusIco string,
	statusLabel string,
) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="id"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>Invoice %s — Bonita Umroh</title>
<style>
@import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700;800&display=swap');
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'Inter',sans-serif;background:#f0f4f8;display:flex;justify-content:center;padding:2rem 1rem;min-height:100vh}
.invoice-wrapper{background:#fff;width:100%%;max-width:680px;border-radius:16px;overflow:hidden;box-shadow:0 8px 40px rgba(0,0,0,.1)}
.inv-header{background:linear-gradient(135deg,#0a2e1c,#1a5c3d);padding:2rem 2.5rem 1.5rem}
.inv-brand{display:flex;align-items:center;gap:.75rem;margin-bottom:1.5rem}
.inv-brand-icon{font-size:1.8rem}
.inv-brand-name{font-size:1.3rem;font-weight:800;color:#fff}
.inv-brand-tagline{font-size:.75rem;color:rgba(255,255,255,.6)}
.inv-title-row{display:flex;justify-content:space-between;align-items:flex-end}
.inv-title{font-size:.75rem;font-weight:600;color:rgba(255,255,255,.6);text-transform:uppercase;letter-spacing:.1em}
.inv-nomor{font-size:1.2rem;font-weight:800;color:#e8c97e;font-family:'Courier New',monospace;margin-top:.25rem}
.inv-tanggal{text-align:right;font-size:.78rem;color:rgba(255,255,255,.6);line-height:1.5}
.inv-body{padding:2rem 2.5rem}
.inv-section{margin-bottom:1.75rem}
.inv-section-title{font-size:.72rem;font-weight:700;color:#94a3b8;text-transform:uppercase;letter-spacing:.1em;margin-bottom:1rem;padding-bottom:.5rem;border-bottom:1.5px solid #f1f5f9}
.inv-info-grid{display:grid;grid-template-columns:1fr 1fr;gap:.75rem 2rem}
.inv-info-label{font-size:.72rem;color:#94a3b8;font-weight:500;margin-bottom:.2rem}
.inv-info-val{font-size:.88rem;color:#1e293b;font-weight:600}
.inv-total-card{background:linear-gradient(135deg,#0a2e1c,#1a5c3d);border-radius:12px;padding:1.25rem 1.5rem;margin-bottom:1.75rem;display:flex;justify-content:space-between;align-items:center}
.inv-total-label{font-size:.85rem;font-weight:600;color:rgba(255,255,255,.75)}
.inv-total-meta{font-size:.72rem;color:rgba(255,255,255,.5);margin-top:.2rem}
.inv-total-amount{font-size:1.4rem;font-weight:800;color:#e8c97e;font-family:'Courier New',monospace}
.inv-table{width:100%%;border-collapse:collapse;font-size:.85rem}
.inv-table th{font-size:.68rem;font-weight:700;color:#94a3b8;text-transform:uppercase;letter-spacing:.08em;padding:.5rem .75rem;text-align:left;border-bottom:2px solid #f1f5f9}
.inv-table th:last-child{text-align:right}
.inv-table td{padding:.875rem .75rem;border-bottom:1px solid #f8fafc}
.tbl-date{color:#64748b;font-size:.8rem}
.tbl-label{font-weight:600;color:#1e293b}
.tbl-amount{text-align:right;font-weight:700;color:#1e293b;font-family:'Courier New',monospace}
.inv-paid-row{display:flex;justify-content:space-between;align-items:center;padding:1rem .75rem;background:#f8fafc;border-radius:10px;margin-top:.5rem}
.inv-paid-label{font-size:.82rem;font-weight:700;color:#374151}
.inv-paid-amount{font-size:1.1rem;font-weight:800;color:#1e293b;font-family:'Courier New',monospace}
.inv-status-row{display:flex;justify-content:flex-end;margin-top:.75rem}
.status-badge{display:inline-flex;align-items:center;gap:.4rem;padding:.5rem 1.25rem;border-radius:999px;font-size:.78rem;font-weight:800;letter-spacing:.08em;text-transform:uppercase}
.status-lunas{background:#d1fae5;color:#065f46}
.status-dp{background:#dbeafe;color:#1e40af}
.status-pending{background:#fef3c7;color:#92400e}
.inv-footer{background:linear-gradient(135deg,#0a2e1c,#1a5c3d);padding:1.5rem 2.5rem;text-align:center}
.inv-footer-text{font-size:.82rem;color:rgba(255,255,255,.65);line-height:1.6}
.inv-footer-brand{font-size:.78rem;color:#e8c97e;font-weight:600;margin-top:.5rem}
.print-btn-row{display:flex;justify-content:center;gap:1rem;padding:1.5rem;background:#f8fafc}
.print-btn{display:inline-flex;align-items:center;gap:.4rem;padding:.75rem 1.75rem;background:linear-gradient(135deg,#0a2e1c,#1a5c3d);color:#fff;border:none;border-radius:10px;font-size:.9rem;font-weight:700;font-family:'Inter',sans-serif;cursor:pointer}
.close-btn{display:inline-flex;align-items:center;gap:.4rem;padding:.75rem 1.5rem;background:#f1f5f9;color:#475569;border:1.5px solid #e2e8f0;border-radius:10px;font-size:.9rem;font-weight:600;font-family:'Inter',sans-serif;cursor:pointer;text-decoration:none}
@media print{body{background:#fff;padding:0}.invoice-wrapper{box-shadow:none;border-radius:0;max-width:100%%}.print-btn-row{display:none}}
</style></head><body><div class="invoice-wrapper">
<div class="inv-header">
  <div class="inv-brand"><div class="inv-brand-icon">🕌</div><div><div class="inv-brand-name">Bonita</div><div class="inv-brand-tagline">Umrah • Haji • Muslim Tours</div></div></div>
  <div class="inv-title-row"><div><div class="inv-title">Invoice</div><div class="inv-nomor">%s</div></div><div class="inv-tanggal">Tanggal<br><strong style="color:#fff">%s</strong></div></div>
</div>
<div class="inv-body">
  <div class="inv-section"><div class="inv-section-title">Daftar Jamaah (%d Orang)</div><div class="inv-info-grid">%s</div></div>
  <div class="inv-section"><div class="inv-section-title">Detail Paket</div><div class="inv-info-grid">
    <div class="inv-info-item"><div class="inv-info-label">Nama Paket</div><div class="inv-info-val">%s</div></div>
    <div class="inv-info-item"><div class="inv-info-label">Durasi</div><div class="inv-info-val">%d Hari</div></div>
    <div class="inv-info-item"><div class="inv-info-label">Tanggal Berangkat</div><div class="inv-info-val">%s</div></div>
    <div class="inv-info-item"><div class="inv-info-label">Harga per Orang</div><div class="inv-info-val">Rp %s</div></div>
  </div></div>
  <div class="inv-total-card">
    <div><div class="inv-total-label">💰 Total Tagihan</div><div class="inv-total-meta">%d orang × Rp %s</div></div>
    <div class="inv-total-amount">Rp %s</div>
  </div>
  <div class="inv-section"><div class="inv-section-title">Riwayat Pembayaran</div>
    <table class="inv-table"><thead><tr><th>Tanggal</th><th>Keterangan</th><th>Jumlah</th></tr></thead><tbody>%s</tbody></table>
    <div class="inv-paid-row"><div class="inv-paid-label">Total Dibayar</div><div class="inv-paid-amount">Rp %s</div></div>
    <div class="inv-status-row"><span class="status-badge %s">%s %s</span></div>
  </div>
</div>
<div class="inv-footer"><div class="inv-footer-text">Terima kasih telah mempercayakan perjalanan ibadah Anda kepada<br><strong style="color:#e8c97e">Bonita Umroh</strong> — Melayani dengan hati, memberangkatkan dengan amanah.</div><div class="inv-footer-brand">📞 +62 823-1888-3430 &nbsp;|&nbsp; ✉️ info@bonitaumroh.com</div></div>
<div class="print-btn-row"><button class="print-btn" onclick="window.print()">🖨️ Print / Save PDF</button><a class="close-btn" onclick="window.close()">✕ Tutup</a></div>
</div></body></html>`,
		nomorInvoice,
		nomorInvoice, tanggal,
		totalOrang, jamaahHTML,
		namaPaket, durasi, tanggalBerangkat, hargaPerOrang,
		jumlahOrang, hargaPerOrangAlt, totalTagihan,
		riwayatHTML, totalDibayar,
		statusClass, statusIco, statusLabel,
	)
}
