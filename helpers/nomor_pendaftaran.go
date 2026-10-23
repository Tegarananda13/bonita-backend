package helpers

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"bonita-backend/models"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	// PendaftaranCharset karakter alfanumerik kapital A-Z dan 0-9
	PendaftaranCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// PendaftaranRandomLength panjang kode acak XXXX (4 karakter)
	PendaftaranRandomLength = 4
	// PendaftaranPrefix prefix nomor pendaftaran
	PendaftaranPrefix = "UMR"
	// MaxGenerateRetries batas percobaan jika terjadi konflik nomor di DB
	MaxGenerateRetries = 10
)

// GenerateRandomPendaftaranCode menghasilkan 4 karakter alfanumerik acak (A-Z, 0-9)
// menggunakan crypto/rand yang aman secara kriptografis.
func GenerateRandomPendaftaranCode() (string, error) {
	code := make([]byte, PendaftaranRandomLength)
	charsetLen := big.NewInt(int64(len(PendaftaranCharset)))

	for i := range code {
		n, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("crypto rand error: %w", err)
		}
		code[i] = PendaftaranCharset[n.Int64()]
	}

	return string(code), nil
}

// FormatNomorPendaftaran menyusun string format UMR-YYYYMMDD-XXXX
func FormatNomorPendaftaran(t time.Time, code string) string {
	return fmt.Sprintf("%s-%s-%s", PendaftaranPrefix, t.Format("20060102"), code)
}

// GenerateNomorPendaftaran menghasilkan nomor pendaftaran berformat UMR-YYYYMMDD-XXXX.
// Fungsi ini memeriksa keunikan nomor ke database pendaftaran dan mengulangi jika terjadi konflik
// hingga batas percobaan yang wajar (MaxGenerateRetries).
func GenerateNomorPendaftaran(db *gorm.DB) (string, error) {
	return GenerateNomorPendaftaranAt(db, time.Now())
}

// GenerateNomorPendaftaranAt menghasilkan nomor pendaftaran pada waktu t tertentu
func GenerateNomorPendaftaranAt(db *gorm.DB, t time.Time) (string, error) {
	for attempt := 0; attempt < MaxGenerateRetries; attempt++ {
		code, err := GenerateRandomPendaftaranCode()
		if err != nil {
			return "", err
		}

		nomor := FormatNomorPendaftaran(t, code)

		// Periksa apakah nomor sudah pernah ada di database
		var count int64
		if err := db.Model(&models.Pendaftaran{}).
			Where("nomor_pendaftaran = ?", nomor).
			Count(&count).Error; err != nil {
			return "", fmt.Errorf("gagal memeriksa keunikan nomor pendaftaran: %w", err)
		}

		if count == 0 {
			return nomor, nil
		}
	}

	return "", errors.New("gagal menghasilkan nomor pendaftaran unik setelah batas percobaan tercapai")
}

// IsDuplicateKeyError memeriksa apakah error yang terjadi merupakan pelanggaran unique constraint
// di PostgreSQL (SQLSTATE 23505) atau error constraint unik lainnya.
func IsDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "pendaftaran_pkey")
}

// CreatePendaftaranWithRetry menyimpan record pendaftaran ke database dengan penanganan
// kemungkinan konflik constraint unik (race condition). Jika nomor pendaftaran kosong atau
// terjadi konflik duplicate key saat insert, fungsi ini akan membuat nomor pendaftaran baru
// dan mencoba kembali hingga maxRetries.
func CreatePendaftaranWithRetry(db *gorm.DB, p *models.Pendaftaran, maxRetries int) error {
	if maxRetries <= 0 {
		maxRetries = 5
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Jika nomor pendaftaran belum ada atau ini percobaan ulang setelah konflik
		if p.NomorPendaftaran == "" || attempt > 0 {
			t := p.TanggalDaftar
			if t.IsZero() {
				t = time.Now()
			}
			nomor, err := GenerateNomorPendaftaranAt(db, t)
			if err != nil {
				return err
			}
			p.NomorPendaftaran = nomor
		}

		err := db.Create(p).Error
		if err == nil {
			return nil
		}

		lastErr = err
		if !IsDuplicateKeyError(err) {
			// Error lain (bukan duplicate key), hentikan dan kembalikan error
			return err
		}
		// Konflik duplicate key terdeteksi pada database: loop berlanjut untuk retry
	}

	return fmt.Errorf("gagal menyimpan pendaftaran setelah %d percobaan: %w", maxRetries, lastErr)
}
