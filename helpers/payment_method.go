package helpers

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Konstanta metode pembayaran, bank tujuan resmi, dan sumber pencatatan pembayaran.
const (
	MetodePembayaranTransfer = "transfer"
	MetodePembayaranTunai    = "tunai"

	BankBSI     = "BSI"
	BankMandiri = "MANDIRI"
	BankBCA     = "BCA"

	SumberPembayaranCustomer = "customer"
	SumberPembayaranAdmin    = "admin"
)

// IsValidBankTujuan memeriksa apakah nama bank termasuk rekening resmi Bonita Umroh
// (BSI, Mandiri, atau BCA) dan mengembalikan kode bank ternormalisasi (uppercase).
func IsValidBankTujuan(bank string) (string, bool) {
	cleaned := strings.ToUpper(strings.TrimSpace(bank))
	switch cleaned {
	case BankBSI, "BANK BSI", "TRANSFER BSI", "TRANSFER BANK BSI":
		return BankBSI, true
	case BankMandiri, "BANK MANDIRI", "TRANSFER MANDIRI", "TRANSFER BANK MANDIRI":
		return BankMandiri, true
	case BankBCA, "BANK BCA", "TRANSFER BCA", "TRANSFER BANK BCA":
		return BankBCA, true
	default:
		return "", false
	}
}

// ValidatePaymentMethodAndBank memvalidasi kombinasi metode pembayaran dan bank tujuan:
//   - Metode "transfer" wajib memiliki bank tujuan valid (BSI, Mandiri, atau BCA).
//   - Metode "tunai" tidak boleh memiliki bank tujuan.
//   - Metode selain "transfer" dan "tunai" ditolak.
func ValidatePaymentMethodAndBank(metode, bankTujuan string) (string, string, error) {
	m := strings.ToLower(strings.TrimSpace(metode))
	b := strings.TrimSpace(bankTujuan)

	switch m {
	case MetodePembayaranTransfer:
		if b == "" {
			return "", "", errors.New("Bank tujuan wajib diisi untuk metode pembayaran transfer")
		}
		normBank, ok := IsValidBankTujuan(b)
		if !ok {
			return "", "", errors.New("Bank tujuan tidak valid (hanya mendukung BSI, Mandiri, atau BCA)")
		}
		return MetodePembayaranTransfer, normBank, nil

	case MetodePembayaranTunai:
		if b != "" {
			return "", "", errors.New("Bank tujuan tidak boleh diisi untuk metode pembayaran tunai")
		}
		return MetodePembayaranTunai, "", nil

	default:
		return "", "", errors.New("Metode pembayaran tidak valid (hanya mendukung transfer atau tunai)")
	}
}

// ValidatePaymentNominal memastikan nominal pembayaran bernilai positif (> 0) dan bukan NaN/Inf.
func ValidatePaymentNominal(jumlah float64) error {
	if math.IsNaN(jumlah) || math.IsInf(jumlah, 0) || jumlah <= 0 {
		return errors.New("Jumlah pembayaran tidak valid")
	}
	return nil
}

// ValidatePaymentDate memvalidasi string tanggal pembayaran (format YYYY-MM-DD atau RFC3339).
func ValidatePaymentDate(tanggalStr string) (time.Time, error) {
	trimmed := strings.TrimSpace(tanggalStr)
	if trimmed == "" {
		return time.Time{}, errors.New("Tanggal pembayaran wajib diisi")
	}
	if t, err := time.Parse("2006-01-02", trimmed); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return t, nil
	}
	return time.Time{}, errors.New("Tanggal pembayaran tidak valid (gunakan format YYYY-MM-DD)")
}

// ValidateAdminManualPaymentInput memvalidasi seluruh input pencatatan pembayaran manual oleh Admin:
// nominal > 0, tanggal pembayaran valid, metode pembayaran ("transfer"/"tunai"), dan bank tujuan.
func ValidateAdminManualPaymentInput(jumlah float64, tanggalStr, metode, bankTujuan string) (time.Time, string, string, error) {
	if err := ValidatePaymentNominal(jumlah); err != nil {
		return time.Time{}, "", "", err
	}
	tgl, err := ValidatePaymentDate(tanggalStr)
	if err != nil {
		return time.Time{}, "", "", err
	}
	normMetode, normBank, err := ValidatePaymentMethodAndBank(metode, bankTujuan)
	if err != nil {
		return time.Time{}, "", "", err
	}
	return tgl, normMetode, normBank, nil
}

// ValidateCustomerPaymentRequest memvalidasi request pembuatan pembayaran dari portal customer.
// Customer:
//   - Hanya boleh menggunakan metode "transfer" (tidak boleh "tunai").
//   - Tidak boleh memanipulasi sumber_pembayaran menjadi "admin".
//   - Tidak boleh menetapkan status pembayaran menjadi "diterima".
//   - Jika mengirim bank_tujuan, harus termasuk BSI, Mandiri, atau BCA.
func ValidateCustomerPaymentRequest(jumlah float64, metode, bankTujuan, sumber, status string) (string, string, error) {
	if err := ValidatePaymentNominal(jumlah); err != nil {
		return "", "", errors.New("Jumlah wajib diisi")
	}

	s := strings.ToLower(strings.TrimSpace(sumber))
	if s != "" && s != SumberPembayaranCustomer {
		return "", "", errors.New("Customer tidak diizinkan menentukan sumber pembayaran admin")
	}

	st := strings.ToLower(strings.TrimSpace(status))
	if st != "" && st != PaymentVerificationPending {
		return "", "", errors.New("Customer tidak diizinkan menetapkan status pembayaran")
	}

	m := strings.ToLower(strings.TrimSpace(metode))
	if m != "" && m != MetodePembayaranTransfer {
		return "", "", errors.New("Pembayaran melalui portal customer hanya mendukung metode transfer")
	}

	b := strings.TrimSpace(bankTujuan)
	normBank := ""
	if b != "" {
		validBank, ok := IsValidBankTujuan(b)
		if !ok {
			return "", "", errors.New("Bank tujuan tidak valid (hanya mendukung BSI, Mandiri, atau BCA)")
		}
		normBank = validBank
	}

	return MetodePembayaranTransfer, normBank, nil
}

// FormatPaymentMethodLabel membentuk label metode pembayaran yang mudah dibaca untuk UI customer & admin:
//   - "Transfer Bank BSI"
//   - "Transfer Bank Mandiri"
//   - "Transfer Bank BCA"
//   - "Tunai (Pembayaran di Kantor)"
//   - Fallback data lama transfer tanpa bank: "Transfer Bank"
func FormatPaymentMethodLabel(metode, bankTujuan string) string {
	m := strings.ToLower(strings.TrimSpace(metode))
	if m == MetodePembayaranTunai {
		return "Tunai (Pembayaran di Kantor)"
	}

	normBank, ok := IsValidBankTujuan(bankTujuan)
	if ok {
		switch normBank {
		case BankBSI:
			return "Transfer Bank BSI"
		case BankMandiri:
			return "Transfer Bank Mandiri"
		case BankBCA:
			return "Transfer Bank BCA"
		}
	}

	cleanedBank := strings.ToUpper(strings.TrimSpace(bankTujuan))
	if cleanedBank != "" {
		return fmt.Sprintf("Transfer Bank %s", cleanedBank)
	}
	return "Transfer Bank"
}

// NormalizePembayaranMetadata menormalisasi nilai metode_pembayaran, bank_tujuan, sumber_pembayaran,
// serta menghasilkan label tampilan yang konsisten (termasuk untuk data pembayaran lama).
func NormalizePembayaranMetadata(metode, bankTujuan, sumber string) (normMetode, normBank, normSumber, label string) {
	m := strings.ToLower(strings.TrimSpace(metode))
	if m == MetodePembayaranTunai {
		normMetode = MetodePembayaranTunai
		normBank = ""
	} else {
		normMetode = MetodePembayaranTransfer
		if validBank, ok := IsValidBankTujuan(bankTujuan); ok {
			normBank = validBank
		} else {
			normBank = strings.ToUpper(strings.TrimSpace(bankTujuan))
		}
	}

	s := strings.ToLower(strings.TrimSpace(sumber))
	if s == SumberPembayaranAdmin {
		normSumber = SumberPembayaranAdmin
	} else {
		normSumber = SumberPembayaranCustomer
	}

	label = FormatPaymentMethodLabel(normMetode, normBank)
	return normMetode, normBank, normSumber, label
}

// CalculateAcceptedPaymentTotal menghitung total pembayaran yang berstatus "diterima"
// dari daftar transaksi pembayaran (baik transfer customer, transfer manual admin, maupun tunai).
// Pembayaran berstatus "pending" atau "ditolak" tidak dihitung.
func CalculateAcceptedPaymentTotal(pembayaranList []struct {
	Jumlah float64
	Status string
}) float64 {
	var total float64
	for _, p := range pembayaranList {
		if strings.EqualFold(strings.TrimSpace(p.Status), PaymentVerificationDiterima) {
			if p.Jumlah > 0 && !math.IsNaN(p.Jumlah) && !math.IsInf(p.Jumlah, 0) {
				total += p.Jumlah
			}
		}
	}
	return total
}

