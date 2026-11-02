package helpers

import (
	"math"
	"strings"
	"testing"
)

func TestIsValidBankTujuan(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantBank string
		wantOK   bool
	}{
		{"BSI uppercase", "BSI", BankBSI, true},
		{"BSI lowercase with spaces", "  bsi  ", BankBSI, true},
		{"Bank BSI prefix", "Bank BSI", BankBSI, true},
		{"Transfer BSI prefix", "Transfer BSI", BankBSI, true},
		{"Transfer Bank BSI prefix", "Transfer Bank BSI", BankBSI, true},
		{"Mandiri uppercase", "MANDIRI", BankMandiri, true},
		{"Mandiri mixed case", "Mandiri", BankMandiri, true},
		{"Bank Mandiri prefix", "bank mandiri", BankMandiri, true},
		{"Transfer Mandiri prefix", "Transfer Mandiri", BankMandiri, true},
		{"Transfer Bank Mandiri prefix", "Transfer Bank Mandiri", BankMandiri, true},
		{"BCA uppercase", "BCA", BankBCA, true},
		{"BCA lowercase", "bca", BankBCA, true},
		{"Bank BCA prefix", "Bank BCA", BankBCA, true},
		{"Transfer BCA prefix", "Transfer BCA", BankBCA, true},
		{"Transfer Bank BCA prefix", "Transfer Bank BCA", BankBCA, true},
		{"Invalid bank BRI", "BRI", "", false},
		{"Invalid bank BNI", "BNI", "", false},
		{"Invalid bank Permata", "Permata", "", false},
		{"Empty bank", "", "", false},
		{"Whitespace only", "   ", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotBank, gotOK := IsValidBankTujuan(tc.input)
			if gotOK != tc.wantOK {
				t.Errorf("IsValidBankTujuan(%q) ok = %v, want %v", tc.input, gotOK, tc.wantOK)
			}
			if gotBank != tc.wantBank {
				t.Errorf("IsValidBankTujuan(%q) bank = %q, want %q", tc.input, gotBank, tc.wantBank)
			}
		})
	}
}

func TestValidatePaymentMethodAndBank(t *testing.T) {
	tests := []struct {
		name       string
		metode     string
		bankTujuan string
		wantMetode string
		wantBank   string
		wantErr    string
	}{
		{
			name:       "Valid Transfer BSI",
			metode:     "transfer",
			bankTujuan: "BSI",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankBSI,
		},
		{
			name:       "Valid Transfer Mandiri mixed case",
			metode:     "  Transfer ",
			bankTujuan: " Mandiri ",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankMandiri,
		},
		{
			name:       "Valid Transfer BCA",
			metode:     "TRANSFER",
			bankTujuan: "bca",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankBCA,
		},
		{
			name:       "Valid Tunai without bank",
			metode:     "tunai",
			bankTujuan: "",
			wantMetode: MetodePembayaranTunai,
			wantBank:   "",
		},
		{
			name:       "Valid Tunai uppercase with whitespace bank",
			metode:     "  TUNAI ",
			bankTujuan: "   ",
			wantMetode: MetodePembayaranTunai,
			wantBank:   "",
		},
		{
			name:       "Invalid Transfer without bank_tujuan",
			metode:     "transfer",
			bankTujuan: "",
			wantErr:    "Bank tujuan wajib diisi untuk metode pembayaran transfer",
		},
		{
			name:       "Invalid Transfer with unsupported bank BRI",
			metode:     "transfer",
			bankTujuan: "BRI",
			wantErr:    "Bank tujuan tidak valid (hanya mendukung BSI, Mandiri, atau BCA)",
		},
		{
			name:       "Invalid Transfer with unsupported bank BNI",
			metode:     "transfer",
			bankTujuan: "BNI",
			wantErr:    "Bank tujuan tidak valid (hanya mendukung BSI, Mandiri, atau BCA)",
		},
		{
			name:       "Invalid Tunai with bank_tujuan filled",
			metode:     "tunai",
			bankTujuan: "BSI",
			wantErr:    "Bank tujuan tidak boleh diisi untuk metode pembayaran tunai",
		},
		{
			name:       "Invalid Metode empty",
			metode:     "",
			bankTujuan: "BSI",
			wantErr:    "Metode pembayaran tidak valid (hanya mendukung transfer atau tunai)",
		},
		{
			name:       "Invalid Metode qris",
			metode:     "qris",
			bankTujuan: "",
			wantErr:    "Metode pembayaran tidak valid (hanya mendukung transfer atau tunai)",
		},
		{
			name:       "Invalid Metode kartu_kredit",
			metode:     "kartu_kredit",
			bankTujuan: "BCA",
			wantErr:    "Metode pembayaran tidak valid (hanya mendukung transfer atau tunai)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotMetode, gotBank, err := ValidatePaymentMethodAndBank(tc.metode, tc.bankTujuan)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMetode != tc.wantMetode {
				t.Errorf("got metode %q, want %q", gotMetode, tc.wantMetode)
			}
			if gotBank != tc.wantBank {
				t.Errorf("got bank %q, want %q", gotBank, tc.wantBank)
			}
		})
	}
}

func TestValidatePaymentNominal(t *testing.T) {
	tests := []struct {
		name    string
		jumlah  float64
		wantErr bool
	}{
		{"Valid positive DP 5 million", 5000000, false},
		{"Valid small positive", 0.01, false},
		{"Invalid zero", 0, true},
		{"Invalid negative", -5000000, true},
		{"Invalid NaN", math.NaN(), true},
		{"Invalid +Inf", math.Inf(1), true},
		{"Invalid -Inf", math.Inf(-1), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePaymentNominal(tc.jumlah)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidatePaymentNominal(%v) err = %v, wantErr %v", tc.jumlah, err, tc.wantErr)
			}
		})
	}
}

func TestValidatePaymentDate(t *testing.T) {
	tests := []struct {
		name       string
		tanggalStr string
		wantYear   int
		wantErr    string
	}{
		{
			name:       "Valid YYYY-MM-DD",
			tanggalStr: "2026-10-30",
			wantYear:   2026,
		},
		{
			name:       "Valid YYYY-MM-DD with whitespace",
			tanggalStr: "  2026-11-15  ",
			wantYear:   2026,
		},
		{
			name:       "Valid RFC3339",
			tanggalStr: "2026-10-30T14:30:00Z",
			wantYear:   2026,
		},
		{
			name:       "Empty string",
			tanggalStr: "",
			wantErr:    "Tanggal pembayaran wajib diisi",
		},
		{
			name:       "Whitespace only",
			tanggalStr: "   ",
			wantErr:    "Tanggal pembayaran wajib diisi",
		},
		{
			name:       "Invalid date format",
			tanggalStr: "30-10-2026",
			wantErr:    "Tanggal pembayaran tidak valid",
		},
		{
			name:       "Invalid calendar date",
			tanggalStr: "2026-13-45",
			wantErr:    "Tanggal pembayaran tidak valid",
		},
		{
			name:       "Non-date string",
			tanggalStr: "bukan-tanggal",
			wantErr:    "Tanggal pembayaran tidak valid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTime, err := ValidatePaymentDate(tc.tanggalStr)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotTime.Year() != tc.wantYear {
				t.Errorf("got year %d, want %d", gotTime.Year(), tc.wantYear)
			}
		})
	}
}

func TestValidateAdminManualPaymentInput(t *testing.T) {
	tests := []struct {
		name       string
		jumlah     float64
		tanggalStr string
		metode     string
		bankTujuan string
		wantMetode string
		wantBank   string
		wantErr    string
	}{
		{
			name:       "Valid Admin Transfer BSI",
			jumlah:     10000000,
			tanggalStr: "2026-10-30",
			metode:     "transfer",
			bankTujuan: "BSI",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankBSI,
		},
		{
			name:       "Valid Admin Transfer Mandiri",
			jumlah:     7500000,
			tanggalStr: "2026-10-30",
			metode:     "transfer",
			bankTujuan: "Mandiri",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankMandiri,
		},
		{
			name:       "Valid Admin Transfer BCA",
			jumlah:     15000000,
			tanggalStr: "2026-10-30",
			metode:     "transfer",
			bankTujuan: "BCA",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankBCA,
		},
		{
			name:       "Valid Admin Tunai",
			jumlah:     5000000,
			tanggalStr: "2026-10-30",
			metode:     "tunai",
			bankTujuan: "",
			wantMetode: MetodePembayaranTunai,
			wantBank:   "",
		},
		{
			name:       "Invalid Nominal zero",
			jumlah:     0,
			tanggalStr: "2026-10-30",
			metode:     "tunai",
			bankTujuan: "",
			wantErr:    "Jumlah pembayaran tidak valid",
		},
		{
			name:       "Invalid Nominal negative",
			jumlah:     -100000,
			tanggalStr: "2026-10-30",
			metode:     "tunai",
			bankTujuan: "",
			wantErr:    "Jumlah pembayaran tidak valid",
		},
		{
			name:       "Invalid Date empty",
			jumlah:     5000000,
			tanggalStr: "",
			metode:     "tunai",
			bankTujuan: "",
			wantErr:    "Tanggal pembayaran wajib diisi",
		},
		{
			name:       "Invalid Date malformed",
			jumlah:     5000000,
			tanggalStr: "invalid-date",
			metode:     "tunai",
			bankTujuan: "",
			wantErr:    "Tanggal pembayaran tidak valid",
		},
		{
			name:       "Invalid Transfer missing bank",
			jumlah:     5000000,
			tanggalStr: "2026-10-30",
			metode:     "transfer",
			bankTujuan: "",
			wantErr:    "Bank tujuan wajib diisi untuk metode pembayaran transfer",
		},
		{
			name:       "Invalid Tunai with bank",
			jumlah:     5000000,
			tanggalStr: "2026-10-30",
			metode:     "tunai",
			bankTujuan: "BCA",
			wantErr:    "Bank tujuan tidak boleh diisi untuk metode pembayaran tunai",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTgl, gotMetode, gotBank, err := ValidateAdminManualPaymentInput(tc.jumlah, tc.tanggalStr, tc.metode, tc.bankTujuan)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotTgl.IsZero() {
				t.Errorf("expected non-zero parsed date")
			}
			if gotMetode != tc.wantMetode {
				t.Errorf("got metode %q, want %q", gotMetode, tc.wantMetode)
			}
			if gotBank != tc.wantBank {
				t.Errorf("got bank %q, want %q", gotBank, tc.wantBank)
			}
		})
	}
}

func TestValidateCustomerPaymentRequest(t *testing.T) {
	tests := []struct {
		name       string
		jumlah     float64
		metode     string
		bankTujuan string
		sumber     string
		status     string
		wantMetode string
		wantBank   string
		wantErr    string
	}{
		{
			name:       "Valid Customer Transfer BSI",
			jumlah:     5000000,
			metode:     "transfer",
			bankTujuan: "BSI",
			sumber:     "",
			status:     "",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankBSI,
		},
		{
			name:       "Valid Customer Transfer Mandiri",
			jumlah:     10000000,
			metode:     "transfer",
			bankTujuan: "Mandiri",
			sumber:     "customer",
			status:     "pending",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankMandiri,
		},
		{
			name:       "Valid Customer Transfer BCA",
			jumlah:     15000000,
			metode:     "",
			bankTujuan: "BCA",
			sumber:     "",
			status:     "",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankBCA,
		},
		{
			name:       "Valid Legacy Customer Request without bank or metode",
			jumlah:     5000000,
			metode:     "",
			bankTujuan: "",
			sumber:     "",
			status:     "",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   "",
		},
		{
			name:       "Rejected Nominal Zero",
			jumlah:     0,
			metode:     "transfer",
			bankTujuan: "BSI",
			wantErr:    "Jumlah wajib diisi",
		},
		{
			name:       "Rejected Nominal Negative",
			jumlah:     -5000000,
			metode:     "transfer",
			bankTujuan: "BSI",
			wantErr:    "Jumlah wajib diisi",
		},
		{
			name:       "Rejected Customer Cannot Use Metode Tunai",
			jumlah:     5000000,
			metode:     "tunai",
			bankTujuan: "",
			wantErr:    "Pembayaran melalui portal customer hanya mendukung metode transfer",
		},
		{
			name:       "Rejected Customer Cannot Set Sumber Admin",
			jumlah:     5000000,
			metode:     "transfer",
			bankTujuan: "BSI",
			sumber:     "admin",
			wantErr:    "Customer tidak diizinkan menentukan sumber pembayaran admin",
		},
		{
			name:       "Rejected Customer Cannot Set Status Diterima",
			jumlah:     5000000,
			metode:     "transfer",
			bankTujuan: "BSI",
			status:     "diterima",
			wantErr:    "Customer tidak diizinkan menetapkan status pembayaran",
		},
		{
			name:       "Rejected Customer Cannot Set Status Ditolak",
			jumlah:     5000000,
			metode:     "transfer",
			bankTujuan: "BSI",
			status:     "ditolak",
			wantErr:    "Customer tidak diizinkan menetapkan status pembayaran",
		},
		{
			name:       "Rejected Customer Invalid Bank Tujuan BRI",
			jumlah:     5000000,
			metode:     "transfer",
			bankTujuan: "BRI",
			wantErr:    "Bank tujuan tidak valid (hanya mendukung BSI, Mandiri, atau BCA)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotMetode, gotBank, err := ValidateCustomerPaymentRequest(tc.jumlah, tc.metode, tc.bankTujuan, tc.sumber, tc.status)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMetode != tc.wantMetode {
				t.Errorf("got metode %q, want %q", gotMetode, tc.wantMetode)
			}
			if gotBank != tc.wantBank {
				t.Errorf("got bank %q, want %q", gotBank, tc.wantBank)
			}
		})
	}
}

func TestFormatPaymentMethodLabel(t *testing.T) {
	tests := []struct {
		name       string
		metode     string
		bankTujuan string
		wantLabel  string
	}{
		{"Transfer BSI", "transfer", "BSI", "Transfer Bank BSI"},
		{"Transfer Mandiri", "transfer", "MANDIRI", "Transfer Bank Mandiri"},
		{"Transfer BCA", "transfer", "BCA", "Transfer Bank BCA"},
		{"Tunai without bank", "tunai", "", "Tunai (Pembayaran di Kantor)"},
		{"Tunai ignores bank if accidentally present", "tunai", "BSI", "Tunai (Pembayaran di Kantor)"},
		{"Legacy transfer without bank", "transfer", "", "Transfer Bank"},
		{"Empty metode and bank fallback", "", "", "Transfer Bank"},
		{"Custom/other bank fallback", "transfer", "Muamalat", "Transfer Bank MUAMALAT"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatPaymentMethodLabel(tc.metode, tc.bankTujuan)
			if got != tc.wantLabel {
				t.Errorf("FormatPaymentMethodLabel(%q, %q) = %q, want %q", tc.metode, tc.bankTujuan, got, tc.wantLabel)
			}
		})
	}
}

func TestNormalizePembayaranMetadata(t *testing.T) {
	tests := []struct {
		name       string
		metode     string
		bankTujuan string
		sumber     string
		wantMetode string
		wantBank   string
		wantSumber string
		wantLabel  string
	}{
		{
			name:       "Admin Transfer BSI",
			metode:     "transfer",
			bankTujuan: "bsi",
			sumber:     "admin",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankBSI,
			wantSumber: SumberPembayaranAdmin,
			wantLabel:  "Transfer Bank BSI",
		},
		{
			name:       "Admin Transfer Mandiri",
			metode:     "transfer",
			bankTujuan: "Mandiri",
			sumber:     "admin",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankMandiri,
			wantSumber: SumberPembayaranAdmin,
			wantLabel:  "Transfer Bank Mandiri",
		},
		{
			name:       "Customer Transfer BCA",
			metode:     "transfer",
			bankTujuan: "BCA",
			sumber:     "customer",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   BankBCA,
			wantSumber: SumberPembayaranCustomer,
			wantLabel:  "Transfer Bank BCA",
		},
		{
			name:       "Admin Tunai clears any bank and formats label without bank",
			metode:     "tunai",
			bankTujuan: "BSI",
			sumber:     "admin",
			wantMetode: MetodePembayaranTunai,
			wantBank:   "",
			wantSumber: SumberPembayaranAdmin,
			wantLabel:  "Tunai (Pembayaran di Kantor)",
		},
		{
			name:       "Legacy empty metadata defaults to customer transfer",
			metode:     "",
			bankTujuan: "",
			sumber:     "",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   "",
			wantSumber: SumberPembayaranCustomer,
			wantLabel:  "Transfer Bank",
		},
		{
			name:       "Unrecognized bank uppercased",
			metode:     "transfer",
			bankTujuan: "bni",
			sumber:     "customer",
			wantMetode: MetodePembayaranTransfer,
			wantBank:   "BNI",
			wantSumber: SumberPembayaranCustomer,
			wantLabel:  "Transfer Bank BNI",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotM, gotB, gotS, gotL := NormalizePembayaranMetadata(tc.metode, tc.bankTujuan, tc.sumber)
			if gotM != tc.wantMetode || gotB != tc.wantBank || gotS != tc.wantSumber || gotL != tc.wantLabel {
				t.Errorf("NormalizePembayaranMetadata(%q, %q, %q) = (%q, %q, %q, %q), want (%q, %q, %q, %q)",
					tc.metode, tc.bankTujuan, tc.sumber,
					gotM, gotB, gotS, gotL,
					tc.wantMetode, tc.wantBank, tc.wantSumber, tc.wantLabel,
				)
			}
		})
	}
}

func TestCalculateAcceptedPaymentTotal(t *testing.T) {
	items := []struct {
		Jumlah float64
		Status string
	}{
		{Jumlah: 5000000, Status: PaymentVerificationDiterima},  // Tunai oleh Admin -> dihitung
		{Jumlah: 10000000, Status: PaymentVerificationDiterima}, // Transfer BSI oleh Admin -> dihitung
		{Jumlah: 7500000, Status: "DITERIMA"},                   // Transfer Mandiri oleh Customer (verified) -> dihitung
		{Jumlah: 8000000, Status: PaymentVerificationPending},   // Pending -> TIDAK dihitung
		{Jumlah: 6000000, Status: PaymentVerificationDitolak},   // Ditolak -> TIDAK dihitung
		{Jumlah: -1000000, Status: PaymentVerificationDiterima}, // Negatif -> TIDAK dihitung
		{Jumlah: math.NaN(), Status: PaymentVerificationDiterima},
	}

	got := CalculateAcceptedPaymentTotal(items)
	want := 22500000.0
	if got != want {
		t.Errorf("CalculateAcceptedPaymentTotal() = %v, want %v", got, want)
	}
}

