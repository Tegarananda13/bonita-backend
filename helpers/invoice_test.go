package helpers_test

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type failingRandReader struct{}

func (failingRandReader) Read(p []byte) (int, error) {
	return 0, errors.New("simulated crypto/rand failure")
}

func newMockGormDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	dialector := postgres.New(postgres.Config{Conn: sqlDB})
	gdb, err := gorm.Open(dialector, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("Failed to open gorm with sqlmock: %v", err)
	}
	cleanup := func() {
		_ = sqlDB.Close()
	}
	return gdb, mock, cleanup
}

func TestGenerateNomorInvoice(t *testing.T) {
	year := time.Now().Year()
	pattern := regexp.MustCompile(fmt.Sprintf(`^INV-BNT-%d-[A-Z0-9]{6}$`, year))

	t.Run("Success_FirstAttempt", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		nomor, err := helpers.GenerateNomorInvoice(gdb)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !pattern.MatchString(nomor) {
			t.Errorf("Generated invoice number %q does not match pattern %s", nomor, pattern.String())
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet sqlmock expectations: %v", err)
		}
	})

	t.Run("Success_RetriesOnDuplicateUntilUnique", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		// Percobaan 1 konflik (count = 1), percobaan 2 unik (count = 0)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		nomor, err := helpers.GenerateNomorInvoice(gdb)
		if err != nil {
			t.Fatalf("Unexpected error on retry: %v", err)
		}
		if !pattern.MatchString(nomor) {
			t.Errorf("Generated invoice number %q does not match pattern", nomor)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet sqlmock expectations: %v", err)
		}
	})

	t.Run("Error_DatabaseCountFailure", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "invoice" WHERE nomor_invoice = $1`)).
			WithArgs(sqlmock.AnyArg()).
			WillReturnError(errors.New("db connection lost"))

		nomor, err := helpers.GenerateNomorInvoice(gdb)
		if err == nil {
			t.Fatal("Expected error when DB Count fails, got nil")
		}
		if nomor != "" {
			t.Errorf("Expected empty nomor on error, got %q", nomor)
		}
	})

	t.Run("Error_CryptoRandFailure", func(t *testing.T) {
		gdb, _, cleanup := newMockGormDB(t)
		defer cleanup()

		origReader := rand.Reader
		rand.Reader = failingRandReader{}
		defer func() {
			rand.Reader = origReader
		}()

		nomor, err := helpers.GenerateNomorInvoice(gdb)
		if err == nil {
			t.Fatal("Expected error when crypto/rand.Reader fails, got nil")
		}
		if nomor != "" {
			t.Errorf("Expected empty nomor on error, got %q", nomor)
		}
	})
}

func TestFormatRupiah(t *testing.T) {
	tests := []struct {
		name   string
		amount float64
		want   string
	}{
		{"Zero", 0, "0"},
		{"SingleDigit", 7, "7"},
		{"ThreeDigits", 999, "999"},
		{"OneThousand", 1000, "1.000"},
		{"TenThousand", 12500, "12.500"},
		{"HundredsOfThousands", 750000, "750.000"},
		{"Millions", 1500000, "1.500.000"},
		{"TensOfMillions", 28500000, "28.500.000"},
		{"Billions", 1250000000, "1.250.000.000"},
		{"WithFractionalPartTruncated", 25000000.89, "25.000.000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := helpers.FormatRupiah(tc.amount)
			if got != tc.want {
				t.Errorf("FormatRupiah(%v) = %q, want %q", tc.amount, got, tc.want)
			}
		})
	}
}

func TestFormatTanggal(t *testing.T) {
	tests := []struct {
		month time.Month
		day   int
		year  int
		want  string
	}{
		{time.January, 5, 2026, "05 Januari 2026"},
		{time.February, 14, 2026, "14 Februari 2026"},
		{time.March, 1, 2026, "01 Maret 2026"},
		{time.April, 20, 2026, "20 April 2026"},
		{time.May, 9, 2026, "09 Mei 2026"},
		{time.June, 30, 2026, "30 Juni 2026"},
		{time.July, 17, 2026, "17 Juli 2026"},
		{time.August, 17, 2026, "17 Agustus 2026"},
		{time.September, 8, 2026, "08 September 2026"},
		{time.October, 29, 2026, "29 Oktober 2026"},
		{time.November, 10, 2026, "10 November 2026"},
		{time.December, 31, 2026, "31 Desember 2026"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			dt := time.Date(tc.year, tc.month, tc.day, 10, 0, 0, 0, time.UTC)
			got := helpers.FormatTanggal(dt)
			if got != tc.want {
				t.Errorf("FormatTanggal(%v) = %q, want %q", dt, got, tc.want)
			}
		})
	}
}

func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"LUNAS", "✅"},
		{"DP / CICILAN", "🔄"},
		{"BELUM LUNAS", "⏳"},
		{"", "⏳"},
		{"LAINNYA", "⏳"},
	}

	for _, tc := range tests {
		t.Run("Status_"+tc.status, func(t *testing.T) {
			if got := helpers.StatusIcon(tc.status); got != tc.want {
				t.Errorf("StatusIcon(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

func TestBuildDaftarJamaahHTML(t *testing.T) {
	t.Run("EmptySlice", func(t *testing.T) {
		html, count := helpers.BuildDaftarJamaahHTML(nil)
		if html != "" || count != 0 {
			t.Errorf("Expected empty HTML and 0 count, got %q and %d", html, count)
		}
	})

	t.Run("MixedJamaah_ActiveWithPerlengkapan_And_Cancelled", func(t *testing.T) {
		list := []models.Pendaftaran{
			{
				NomorPendaftaran:  "UMR-20261029-AAA1",
				Status:            helpers.StatusProses,
				AmbilPerlengkapan: true,
				HargaPerlengkapan: 1500000,
				Customer:          models.Customer{Nama: "H. Ahmad Fauzi"},
			},
			{
				NomorPendaftaran:  "UMR-20261029-BBB2",
				Status:            helpers.StatusBatal,
				AmbilPerlengkapan: false,
				HargaPerlengkapan: 0,
				Customer:          models.Customer{Nama: "Siti Rahmah"},
			},
		}

		html, count := helpers.BuildDaftarJamaahHTML(list)
		if count != 1 {
			t.Errorf("Expected jumlahAmbilPerlengkapan = 1, got %d", count)
		}

		expectedSubstrings := []string{
			"Jamaah 1",
			"H. Ahmad Fauzi",
			"UMR-20261029-AAA1",
			"Perlengkapan Jamaah 1",
			"✓ Rp 1.500.000",
			"Jamaah 2",
			"Siti Rahmah",
			"Batal",
			"UMR-20261029-BBB2",
		}
		for _, sub := range expectedSubstrings {
			if !strings.Contains(html, sub) {
				t.Errorf("Expected HTML to contain %q, got:\n%s", sub, html)
			}
		}
		if strings.Contains(html, "Perlengkapan Jamaah 2") {
			t.Error("Did not expect Perlengkapan Jamaah 2 when AmbilPerlengkapan is false")
		}
	})
}

func TestFormatTanggalWIB_And_BuildApprovalHTML(t *testing.T) {
	// 2026-03-05 08:05 UTC == 2026-03-05 15:05 WIB
	utcTime := time.Date(2026, time.March, 5, 8, 5, 0, 0, time.UTC)
	gotWIB := helpers.FormatTanggalWIB(utcTime)
	wantWIB := "5 Maret 2026, 15:05 WIB"
	if gotWIB != wantWIB {
		t.Errorf("FormatTanggalWIB() = %q, want %q", gotWIB, wantWIB)
	}

	t.Run("BuildApprovalHTML_EmptyNameReturnsEmpty", func(t *testing.T) {
		if got := helpers.BuildApprovalHTML("", &utcTime); got != "" {
			t.Errorf("Expected empty string when approverNama is empty, got %q", got)
		}
	})

	t.Run("BuildApprovalHTML_NilTimeReturnsEmpty", func(t *testing.T) {
		if got := helpers.BuildApprovalHTML("Manager Bonita", nil); got != "" {
			t.Errorf("Expected empty string when approvedAt is nil, got %q", got)
		}
	})

	t.Run("BuildApprovalHTML_ValidReturnsStamp", func(t *testing.T) {
		got := helpers.BuildApprovalHTML("Hj. Rina Manager", &utcTime)
		for _, expected := range []string{
			"Diverifikasi dan disetujui oleh",
			"Hj. Rina Manager",
			"Administration Manager",
			wantWIB,
		} {
			if !strings.Contains(got, expected) {
				t.Errorf("Expected approval HTML to contain %q, got:\n%s", expected, got)
			}
		}
	})
}

func TestBuildInvoiceHTML(t *testing.T) {
	html := helpers.BuildInvoiceHTML(
		"INV-BNT-2026-XYZ123",
		"29 Oktober 2026",
		2,
		"<div>Blok Jamaah</div>",
		"Paket Umroh Reguler Syawal",
		12,
		"15 Mei 2026",
		"28.500.000",
		2,
		"28.500.000",
		"57.000.000",
		"<div>Blok Perlengkapan</div>",
		"60.000.000",
		"<tr><td>Riwayat 1</td></tr>",
		"60.000.000",
		"status-lunas",
		"✅",
		"LUNAS",
		"<div>Stempel Approval</div>",
	)

	expectedParts := []string{
		"<!DOCTYPE html>",
		"Invoice INV-BNT-2026-XYZ123 — Bonita Umroh",
		"29 Oktober 2026",
		"Daftar Jamaah (2 Orang)",
		"<div>Blok Jamaah</div>",
		"Paket Umroh Reguler Syawal",
		"12 Hari",
		"15 Mei 2026",
		"Rp 28.500.000",
		"2 orang × Rp 28.500.000",
		"Rp 57.000.000",
		"<div>Blok Perlengkapan</div>",
		"Rp 60.000.000",
		"<tr><td>Riwayat 1</td></tr>",
		"status-lunas",
		"✅ LUNAS",
		"<div>Stempel Approval</div>",
		"linear-gradient(135deg,#0a2e1c,#1a5c3d)",
	}

	for _, part := range expectedParts {
		if !strings.Contains(html, part) {
			t.Errorf("BuildInvoiceHTML missing expected part %q", part)
		}
	}
}
