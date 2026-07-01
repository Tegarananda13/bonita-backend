package helpers

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// GenerateNomorInvoice membuat nomor invoice unik format INV-BNT-YYYY-XXXXXX
// Dipanggil saat pembayaran pertama berhasil dibuat.
// Contoh: INV-BNT-2026-000001
func GenerateNomorInvoice(db *gorm.DB) (string, error) {
	year := time.Now().Year()

	// Hitung berapa pendaftaran yang sudah punya invoice di tahun ini
	var count int64
	db.Model(&struct{ NomorInvoice string }{}).
		Table("pendaftarans").
		Where("nomor_invoice LIKE ?", fmt.Sprintf("INV-BNT-%d-%%", year)).
		Count(&count)

	seq := count + 1
	nomor := fmt.Sprintf("INV-BNT-%d-%06d", year, seq)
	return nomor, nil
}
