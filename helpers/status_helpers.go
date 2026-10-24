package helpers

import (
	"log"
	"sort"
	"strings"

	"bonita-backend/config"
	"bonita-backend/models"
)

// DetermineGroupMainStatus menentukan status utama pendaftaran berdasarkan status lunas invoice
// dan kelengkapan dokumen seluruh jamaah dalam grup (atau individu).
// Aturan:
// - Lunas + Seluruh Dokumen Lengkap -> menunggu_verifikasi_manager
// - Lunas + Dokumen Belum Lengkap -> menunggu_dokumen
// - Belum Lunas + Seluruh Dokumen Lengkap -> menunggu_pembayaran
// - Belum Lunas + Dokumen Belum Lengkap -> proses
func DetermineGroupMainStatus(paymentLunas bool, allDocsLengkap bool) string {
	if paymentLunas && allDocsLengkap {
		return StatusMenungguVerifikasiManager
	} else if paymentLunas {
		return StatusMenungguDokumen
	} else if allDocsLengkap {
		return StatusMenungguPembayaran
	}
	return StatusProses
}

// IsInvoiceLunas mengecek apakah invoice sudah lunas berdasarkan status atau total pembayaran vs total tagihan.
func IsInvoiceLunas(totalPembayaran, totalTagihan float64, statusPembayaran string) bool {
	if statusPembayaran == models.InvoiceStatusLunas {
		return true
	}
	if totalTagihan > 0 && totalPembayaran >= totalTagihan {
		return true
	}
	return false
}

// SyncPendaftaranStatus menghitung ulang dan menyinkronkan status pembayaran invoice
// serta status utama seluruh Pendaftaran dalam satu invoice grup / individu.
func SyncPendaftaranStatus(nomorInvoice string) {
	if nomorInvoice == "" {
		return
	}

	var invoice models.Invoice
	if err := config.DB.Where("nomor_invoice = ?", nomorInvoice).First(&invoice).Error; err != nil {
		return
	}

	// 1. Hitung total pembayaran diterima untuk invoice ini
	var totalDiterima float64
	config.DB.
		Model(&models.Pembayaran{}).
		Where("nomor_invoice = ? AND status = ?", nomorInvoice, PaymentVerificationDiterima).
		Select("COALESCE(SUM(jumlah),0)").
		Scan(&totalDiterima)

	invoice.TotalPembayaran = totalDiterima

	var newInvoiceStatus string
	if totalDiterima <= 0 {
		newInvoiceStatus = models.InvoiceStatusBelumBayar
	} else if invoice.TotalTagihan > 0 && totalDiterima >= invoice.TotalTagihan {
		newInvoiceStatus = models.InvoiceStatusLunas
	} else {
		newInvoiceStatus = models.InvoiceStatusDP
	}
	invoice.StatusPembayaran = newInvoiceStatus

	config.DB.Model(&invoice).Updates(map[string]interface{}{
		"total_pembayaran":  invoice.TotalPembayaran,
		"status_pembayaran": invoice.StatusPembayaran,
	})

	paymentLunas := IsInvoiceLunas(invoice.TotalPembayaran, invoice.TotalTagihan, invoice.StatusPembayaran)

	// 2. Ambil seluruh pendaftaran dalam invoice ini
	var pendaftaranList []models.Pendaftaran
	if err := config.DB.Where("nomor_invoice = ?", nomorInvoice).Find(&pendaftaranList).Error; err != nil || len(pendaftaranList) == 0 {
		return
	}

	// 3. Periksa dan sinkronisasi kelengkapan dokumen masing-masing jamaah aktif
	allDocsLengkap := true
	activeJamaahCount := 0
	for i := range pendaftaranList {
		p := &pendaftaranList[i]
		if p.Status == StatusSelesai || p.Status == StatusKadaluarsa || p.Status == StatusBatal || p.Status == "batal" {
			continue
		}
		activeJamaahCount++

		var docs []models.Dokumen
		config.DB.Where("nomor_pendaftaran = ?", p.NomorPendaftaran).Order("created_at ASC").Find(&docs)
		docStatus := CalculateDocumentStatus(docs)

		if p.DocumentStatus != docStatus {
			p.DocumentStatus = docStatus
			config.DB.Model(&models.Pendaftaran{}).
				Where("nomor_pendaftaran = ?", p.NomorPendaftaran).
				Update("document_status", docStatus)
		}

		if p.DocumentStatus != DocumentLengkap {
			allDocsLengkap = false
		}
	}
	if activeJamaahCount == 0 {
		allDocsLengkap = false
	}

	// 4. Tentukan status utama grup (menunggu_verifikasi_manager jika lunas + semua jamaah lengkap)
	defaultTargetStatus := DetermineGroupMainStatus(paymentLunas, allDocsLengkap)

	// 5. Update seluruh pendaftaran dalam invoice yang tidak berstatus terminal
	statusUpdated := false
	for _, p := range pendaftaranList {
		// Pertahankan status terminal (selesai, kadaluarsa, batal)
		if p.Status == StatusSelesai || p.Status == StatusKadaluarsa || p.Status == StatusBatal || p.Status == "batal" {
			continue
		}

		targetStatus := defaultTargetStatus

		// Aturan 1: Hormati approval Manager.
		// Jika sudah disetujui Manager (ApprovedAt != nil) dan status saat ini siap_berangkat,
		// serta masih lunas + dokumen seluruhnya lengkap, pertahankan siap_berangkat.
		if p.ApprovedAt != nil && p.Status == StatusSiapBerangkat && paymentLunas && allDocsLengkap {
			targetStatus = StatusSiapBerangkat
		}

		// Aturan 2: Jika status saat ini perlu_perbaikan dan perbaikan belum selesai (belum lunas atau belum lengkap),
		// pertahankan status perlu_perbaikan agar tetap di antrean perbaikan Admin.
		if p.Status == StatusPerluPerbaikan && !(paymentLunas && allDocsLengkap) {
			targetStatus = StatusPerluPerbaikan
		}

		if p.Status != targetStatus {
			oldStatus := p.Status
			config.DB.Model(&models.Pendaftaran{}).
				Where("nomor_pendaftaran = ?", p.NomorPendaftaran).
				Update("status", targetStatus)

			log.Printf("[SYNC PENDAFTARAN] Invoice: %s | Jamaah %s: status %s -> %s (Docs: %s)",
				nomorInvoice, p.NomorPendaftaran, oldStatus, targetStatus, p.DocumentStatus)
			statusUpdated = true
		}
	}

	if statusUpdated {
		log.Printf("[SYNC PENDAFTARAN] Selesai sinkronisasi grup %s (Jumlah Jamaah: %d, Lunas: %t, AllDocsLengkap: %t, Target: %s)",
			nomorInvoice, len(pendaftaranList), paymentLunas, allDocsLengkap, defaultTargetStatus)
	}
}

// SyncPendaftaranStatusByNomor menyinkronkan status satu pendaftaran dan seluruh grupnya (jika bagian dari grup invoice).
func SyncPendaftaranStatusByNomor(nomorPendaftaran string) {
	if nomorPendaftaran == "" {
		return
	}

	var p models.Pendaftaran
	if err := config.DB.Where("nomor_pendaftaran = ?", nomorPendaftaran).First(&p).Error; err != nil {
		return
	}

	if p.NomorInvoice != "" {
		SyncPendaftaranStatus(p.NomorInvoice)
		return
	}

	// Pendaftaran tanpa invoice
	if p.Status == StatusSelesai || p.Status == StatusKadaluarsa || p.Status == StatusBatal || p.Status == "batal" {
		return
	}

	var docs []models.Dokumen
	config.DB.Where("nomor_pendaftaran = ?", p.NomorPendaftaran).Order("created_at ASC").Find(&docs)
	docStatus := CalculateDocumentStatus(docs)

	if p.DocumentStatus != docStatus {
		p.DocumentStatus = docStatus
		config.DB.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", p.NomorPendaftaran).
			Update("document_status", docStatus)
	}

	targetStatus := DetermineGroupMainStatus(false, p.DocumentStatus == DocumentLengkap)
	if p.ApprovedAt != nil && p.Status == StatusSiapBerangkat && p.DocumentStatus == DocumentLengkap {
		targetStatus = StatusSiapBerangkat
	}
	if p.Status == StatusPerluPerbaikan && p.DocumentStatus != DocumentLengkap {
		targetStatus = StatusPerluPerbaikan
	}

	if p.Status != targetStatus {
		config.DB.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", p.NomorPendaftaran).
			Update("status", targetStatus)
	}
}

// UpdateStatusPendaftaran adalah alias / wrapper untuk SyncPendaftaranStatusByNomor (backward compatibility).
func UpdateStatusPendaftaran(nomorPendaftaran string) {
	SyncPendaftaranStatusByNomor(nomorPendaftaran)
}

// SyncActivePendaftaran menyinkronkan seluruh pendaftaran aktif (misal sebelum menampilkan daftar pendaftaran admin).
func SyncActivePendaftaran() {
	var activeInvoices []string
	config.DB.Model(&models.Pendaftaran{}).
		Where("status NOT IN (?, ?, ?, ?) AND nomor_invoice != ''", StatusSelesai, StatusKadaluarsa, StatusPerluPerbaikan, StatusBatal).
		Distinct("nomor_invoice").
		Pluck("nomor_invoice", &activeInvoices)

	for _, inv := range activeInvoices {
		SyncPendaftaranStatus(inv)
	}

	var noInvoiceList []models.Pendaftaran
	config.DB.Where("status NOT IN (?, ?, ?, ?) AND (nomor_invoice IS NULL OR nomor_invoice = '')", StatusSelesai, StatusKadaluarsa, StatusPerluPerbaikan, StatusBatal).
		Find(&noInvoiceList)

	for _, p := range noInvoiceList {
		SyncPendaftaranStatusByNomor(p.NomorPendaftaran)
	}
}

// PaymentStatusFromPendaftaran mengambil payment_status dari Invoice milik Pendaftaran.
// Mengembalikan string kosong jika Invoice belum ada.
func PaymentStatusFromPendaftaran(p models.Pendaftaran) string {
	if p.NomorInvoice == "" {
		return PaymentBelum
	}
	return p.Invoice.StatusPembayaran
}

// GetStatusPriority menentukan nomor kelompok prioritas status (1..8).
func GetStatusPriority(status string) int {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "perlu_perbaikan":
		return 1
	case "menunggu_pembayaran", "menunggu_dokumen", "menunggu":
		return 2
	case "proses", "diproses":
		return 3
	case "menunggu_verifikasi_manager":
		return 4
	case "siap_berangkat":
		return 5
	case "selesai":
		return 6
	case "kadaluarsa", "batal":
		return 7
	default:
		return 8
	}
}

// SortPendaftaran mengurutkan pendaftaran secara deterministik berdasarkan prioritas status dan tanggal.
func SortPendaftaran(list []models.Pendaftaran) {
	sort.SliceStable(list, func(i, j int) bool {
		a := list[i]
		b := list[j]

		prioA := GetStatusPriority(a.Status)
		prioB := GetStatusPriority(b.Status)

		if prioA != prioB {
			return prioA < prioB
		}

		switch prioA {
		case 1, 2:
			// Perlu perbaikan / Menunggu pembayaran / dokumen: paling lama menunggu terlebih dahulu (ASC)
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.Before(b.TanggalDaftar)
			}
			return a.NomorPendaftaran < b.NomorPendaftaran

		case 3:
			// Sedang proses: tanggal pendaftaran paling lama terlebih dahulu (ASC)
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.Before(b.TanggalDaftar)
			}
			return a.NomorPendaftaran < b.NomorPendaftaran

		case 4, 5:
			// Menunggu verifikasi manager / Siap berangkat: tanggal keberangkatan paling dekat terlebih dahulu (ASC)
			if !a.Paket.TanggalBerangkat.Equal(b.Paket.TanggalBerangkat) {
				return a.Paket.TanggalBerangkat.Before(b.Paket.TanggalBerangkat)
			}
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.Before(b.TanggalDaftar)
			}
			return a.NomorPendaftaran < b.NomorPendaftaran

		case 6:
			// Selesai: tanggal penyelesaian terbaru terlebih dahulu (DESC)
			if !a.Paket.TanggalBerangkat.Equal(b.Paket.TanggalBerangkat) {
				return a.Paket.TanggalBerangkat.After(b.Paket.TanggalBerangkat)
			}
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.After(b.TanggalDaftar)
			}
			return a.NomorPendaftaran > b.NomorPendaftaran

		case 7:
			// Kedaluwarsa / dibatalkan: tanggal perubahan status terbaru terlebih dahulu (DESC)
			dateA := a.BatasWaktuDP
			if dateA.IsZero() {
				dateA = a.TanggalDaftar
			}
			dateB := b.BatasWaktuDP
			if dateB.IsZero() {
				dateB = b.TanggalDaftar
			}
			if !dateA.Equal(dateB) {
				return dateA.After(dateB)
			}
			if !a.TanggalDaftar.Equal(b.TanggalDaftar) {
				return a.TanggalDaftar.After(b.TanggalDaftar)
			}
			return a.NomorPendaftaran > b.NomorPendaftaran

		default:
			return a.TanggalDaftar.Before(b.TanggalDaftar)
		}
	})
}
