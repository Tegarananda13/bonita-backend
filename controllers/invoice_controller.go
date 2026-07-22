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

	// Ambil pendaftaran + relasi
	var pendaftaran models.Pendaftaran
	if err := config.DB.
		Preload("Customer").
		Preload("Paket").
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pendaftaran tidak ditemukan"})
		return
	}

	// Pastikan ada nomor invoice
	if pendaftaran.NomorInvoice == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invoice belum tersedia. Lakukan pembayaran terlebih dahulu."})
		return
	}

	// Ambil semua pembayaran yang diterima
	var pembayarans []models.Pembayaran
	config.DB.
		Where("pendaftaran_id = ? AND status = ?", pendaftaran.ID, helpers.PaymentVerificationDiterima).
		Order("tanggal_bayar ASC").
		Find(&pembayarans)

	// Hitung total dibayar
	var totalDibayar float64
	for _, p := range pembayarans {
		totalDibayar += p.Jumlah
	}

	// Tentukan status pembayaran
	statusBayar := "BELUM LUNAS"
	statusClass := "status-pending"
	if pendaftaran.PaymentStatus == helpers.PaymentLunas {
		statusBayar = "LUNAS"
		statusClass = "status-lunas"
	} else if totalDibayar > 0 {
		statusBayar = "DP / CICILAN"
		statusClass = "status-dp"
	}

	// Tanggal invoice
	tanggalInvoice := pendaftaran.TanggalDaftar
	if len(pembayarans) > 0 {
		tanggalInvoice = pembayarans[0].TanggalBayar
	}

	// Build tabel riwayat pembayaran
	var riwayatHTML strings.Builder
	for i, p := range pembayarans {
		label := "Pembayaran"
		if i == 0 {
			label = "DP"
		} else {
			label = fmt.Sprintf("Pembayaran %d", i+1)
		}
		tgl := p.TanggalBayar.Format("02 January 2006")
		jumlah := "Rp " + formatRupiah(p.Jumlah)

		riwayatHTML.WriteString(fmt.Sprintf(`
		<tr>
			<td class="tbl-date">%s</td>
			<td class="tbl-label">%s</td>
			<td class="tbl-amount">%s</td>
		</tr>`, tgl, label, jumlah))
	}

	// Build HTML invoice
	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="id">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Invoice %s — Bonita Umroh</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700;800&display=swap');

  * { margin: 0; padding: 0; box-sizing: border-box; }

  body {
    font-family: 'Inter', sans-serif;
    background: #f0f4f8;
    display: flex;
    justify-content: center;
    padding: 2rem 1rem;
    min-height: 100vh;
  }

  .invoice-wrapper {
    background: #fff;
    width: 100%%;
    max-width: 680px;
    border-radius: 16px;
    overflow: hidden;
    box-shadow: 0 8px 40px rgba(0,0,0,0.1);
  }

  /* ── Header ── */
  .inv-header {
    background: linear-gradient(135deg, #0a2e1c 0%%, #1a5c3d 100%%);
    padding: 2rem 2.5rem 1.5rem;
    position: relative;
    overflow: hidden;
  }

  .inv-header::before {
    content: '';
    position: absolute;
    top: -30px; right: -30px;
    width: 180px; height: 180px;
    background: rgba(201,168,76,0.08);
    border-radius: 50%%;
  }

  .inv-brand {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 1.5rem;
  }

  .inv-brand-icon {
    width: 44px; height: 44px;
    background: linear-gradient(135deg, #c9a84c, #a07c2e);
    border-radius: 12px;
    display: flex; align-items: center; justify-content: center;
    font-size: 1.4rem;
  }

  .inv-brand-name {
    font-size: 1.4rem;
    font-weight: 800;
    color: #fff;
    letter-spacing: -0.02em;
  }

  .inv-brand-tagline {
    font-size: 0.72rem;
    color: rgba(255,255,255,0.6);
    letter-spacing: 0.06em;
  }

  .inv-title-row {
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
  }

  .inv-title {
    font-size: 0.72rem;
    font-weight: 700;
    color: rgba(255,255,255,0.5);
    text-transform: uppercase;
    letter-spacing: 0.12em;
    margin-bottom: 0.3rem;
  }

  .inv-nomor {
    font-size: 1.1rem;
    font-weight: 700;
    color: #e8c97e;
    font-family: 'Courier New', monospace;
    letter-spacing: 0.04em;
  }

  .inv-tanggal {
    text-align: right;
    font-size: 0.8rem;
    color: rgba(255,255,255,0.6);
  }

  /* ── Body ── */
  .inv-body {
    padding: 2rem 2.5rem;
  }

  /* Customer info section */
  .inv-section {
    margin-bottom: 1.75rem;
  }

  .inv-section-title {
    font-size: 0.68rem;
    font-weight: 700;
    color: #94a3b8;
    text-transform: uppercase;
    letter-spacing: 0.1em;
    margin-bottom: 0.875rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid #f1f5f9;
  }

  .inv-info-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;
  }

  .inv-info-item { }
  .inv-info-label { font-size: 0.72rem; color: #94a3b8; margin-bottom: 0.2rem; font-weight: 500; }
  .inv-info-val { font-size: 0.9rem; font-weight: 600; color: #1e293b; }

  /* Total paket highlight */
  .inv-total-card {
    background: linear-gradient(135deg, #f0fdf4, #ecfdf5);
    border: 1.5px solid #86efac;
    border-radius: 14px;
    padding: 1.25rem 1.5rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 1.75rem;
  }

  .inv-total-label { font-size: 0.8rem; font-weight: 700; color: #059669; text-transform: uppercase; letter-spacing: 0.06em; }
  .inv-total-amount { font-size: 1.5rem; font-weight: 800; color: #065f46; letter-spacing: -0.02em; }

  /* Riwayat tabel */
  .inv-table {
    width: 100%%;
    border-collapse: collapse;
    font-size: 0.875rem;
  }

  .inv-table th {
    font-size: 0.68rem;
    font-weight: 700;
    color: #94a3b8;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding: 0.5rem 0.75rem;
    text-align: left;
    border-bottom: 2px solid #f1f5f9;
  }

  .inv-table th:last-child { text-align: right; }

  .inv-table td {
    padding: 0.875rem 0.75rem;
    border-bottom: 1px solid #f8fafc;
  }

  .tbl-date { color: #64748b; font-size: 0.8rem; }
  .tbl-label { font-weight: 600; color: #1e293b; }
  .tbl-amount { text-align: right; font-weight: 700; color: #1e293b; font-family: 'Courier New', monospace; }

  /* Total dibayar */
  .inv-paid-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1rem 0.75rem;
    background: #f8fafc;
    border-radius: 10px;
    margin-top: 0.5rem;
  }

  .inv-paid-label { font-size: 0.82rem; font-weight: 700; color: #374151; }
  .inv-paid-amount { font-size: 1.1rem; font-weight: 800; color: #1e293b; font-family: 'Courier New', monospace; }

  /* Status badge */
  .inv-status-row {
    display: flex;
    justify-content: flex-end;
    margin-top: 0.75rem;
  }

  .status-badge {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.5rem 1.25rem;
    border-radius: 999px;
    font-size: 0.78rem;
    font-weight: 800;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .status-lunas  { background: #d1fae5; color: #065f46; }
  .status-dp     { background: #dbeafe; color: #1e40af; }
  .status-pending { background: #fef3c7; color: #92400e; }

  /* Footer */
  .inv-footer {
    background: linear-gradient(135deg, #0a2e1c, #1a5c3d);
    padding: 1.5rem 2.5rem;
    text-align: center;
  }

  .inv-footer-text {
    font-size: 0.82rem;
    color: rgba(255,255,255,0.65);
    line-height: 1.6;
  }

  .inv-footer-brand {
    font-size: 0.78rem;
    color: #e8c97e;
    font-weight: 600;
    margin-top: 0.5rem;
  }

  /* Print button */
  .print-btn-row {
    display: flex;
    justify-content: center;
    gap: 1rem;
    padding: 1.5rem;
    background: #f8fafc;
  }

  .print-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.75rem 1.75rem;
    background: linear-gradient(135deg, #0a2e1c, #1a5c3d);
    color: #fff;
    border: none;
    border-radius: 10px;
    font-size: 0.9rem;
    font-weight: 700;
    font-family: 'Inter', sans-serif;
    cursor: pointer;
    transition: all 0.2s;
  }

  .print-btn:hover { opacity: 0.9; transform: translateY(-1px); }

  .close-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.75rem 1.5rem;
    background: #f1f5f9;
    color: #475569;
    border: 1.5px solid #e2e8f0;
    border-radius: 10px;
    font-size: 0.9rem;
    font-weight: 600;
    font-family: 'Inter', sans-serif;
    cursor: pointer;
    transition: all 0.2s;
    text-decoration: none;
  }

  @media print {
    body { background: #fff; padding: 0; }
    .invoice-wrapper { box-shadow: none; border-radius: 0; max-width: 100%%; }
    .print-btn-row { display: none; }
    .inv-header::before { display: none; }
  }
</style>
</head>
<body>
<div class="invoice-wrapper">

  <!-- Header -->
  <div class="inv-header">
    <div class="inv-brand">
      <div class="inv-brand-icon">🕌</div>
      <div>
        <div class="inv-brand-name">Bonita</div>
        <div class="inv-brand-tagline">Umrah • Haji • Muslim Tours</div>
      </div>
    </div>
    <div class="inv-title-row">
      <div>
        <div class="inv-title">Invoice</div>
        <div class="inv-nomor">%s</div>
      </div>
      <div class="inv-tanggal">
        Tanggal<br><strong style="color:#fff">%s</strong>
      </div>
    </div>
  </div>

  <!-- Body -->
  <div class="inv-body">

    <!-- Info Jamaah & Paket -->
    <div class="inv-section">
      <div class="inv-section-title">Informasi Jamaah</div>
      <div class="inv-info-grid">
        <div class="inv-info-item">
          <div class="inv-info-label">Nama Jamaah</div>
          <div class="inv-info-val">%s</div>
        </div>
        <div class="inv-info-item">
          <div class="inv-info-label">No. HP</div>
          <div class="inv-info-val">%s</div>
        </div>
        <div class="inv-info-item">
          <div class="inv-info-label">Nomor Pendaftaran</div>
          <div class="inv-info-val" style="font-family:'Courier New',monospace">%s</div>
        </div>
        <div class="inv-info-item">
          <div class="inv-info-label">Tanggal Daftar</div>
          <div class="inv-info-val">%s</div>
        </div>
      </div>
    </div>

    <!-- Info Paket -->
    <div class="inv-section">
      <div class="inv-section-title">Detail Paket</div>
      <div class="inv-info-grid">
        <div class="inv-info-item">
          <div class="inv-info-label">Nama Paket</div>
          <div class="inv-info-val">%s</div>
        </div>
        <div class="inv-info-item">
          <div class="inv-info-label">Durasi</div>
          <div class="inv-info-val">%d Hari</div>
        </div>
        <div class="inv-info-item">
          <div class="inv-info-label">Tanggal Berangkat</div>
          <div class="inv-info-val">%s</div>
        </div>
      </div>
    </div>

    <!-- Total Paket -->
    <div class="inv-total-card">
      <div class="inv-total-label">💰 Total Harga Paket</div>
      <div class="inv-total-amount">Rp %s</div>
    </div>

    <!-- Riwayat Pembayaran -->
    <div class="inv-section">
      <div class="inv-section-title">Riwayat Pembayaran</div>
      <table class="inv-table">
        <thead>
          <tr>
            <th>Tanggal</th>
            <th>Keterangan</th>
            <th>Jumlah</th>
          </tr>
        </thead>
        <tbody>
          %s
        </tbody>
      </table>

      <div class="inv-paid-row">
        <div class="inv-paid-label">Total Dibayar</div>
        <div class="inv-paid-amount">Rp %s</div>
      </div>

      <div class="inv-status-row">
        <span class="status-badge %s">
          %s %s
        </span>
      </div>
    </div>

  </div>

  <!-- Footer -->
  <div class="inv-footer">
    <div class="inv-footer-text">
      Terima kasih telah mempercayakan perjalanan ibadah Anda kepada<br>
      <strong style="color:#e8c97e">Bonita Umroh</strong> — Melayani dengan hati, memberangkatkan dengan amanah.
    </div>
    <div class="inv-footer-brand">📞 +62 823-1888-3430 &nbsp;|&nbsp; ✉️ info@bonitaumroh.com</div>
  </div>

  <!-- Print Button -->
  <div class="print-btn-row">
    <button class="print-btn" onclick="window.print()">🖨️ Print / Save PDF</button>
    <a class="close-btn" onclick="window.close()">✕ Tutup</a>
  </div>

</div>
</body>
</html>`,
		pendaftaran.NomorInvoice,
		// header
		pendaftaran.NomorInvoice,
		formatTanggal(tanggalInvoice),
		// info jamaah
		pendaftaran.Customer.Nama,
		pendaftaran.Customer.NoHP,
		pendaftaran.NomorPendaftaran,
		formatTanggal(pendaftaran.TanggalDaftar),
		// info paket
		pendaftaran.Paket.NamaPaket,
		pendaftaran.Paket.Durasi,
		formatTanggal(pendaftaran.Paket.TanggalBerangkat),
		// total paket
		formatRupiah(pendaftaran.Paket.Harga),
		// riwayat
		riwayatHTML.String(),
		formatRupiah(totalDibayar),
		statusClass,
		statusIcon(statusBayar),
		statusBayar,
	)

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"invoice-%s.html\"", pendaftaran.NomorInvoice))
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
