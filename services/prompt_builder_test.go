package services

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var testPaketUmrohCols = []string{
	"id",
	"nama_paket",
	"jenis_paket",
	"foto_paket",
	"harga",
	"tanggal_berangkat",
	"durasi",
	"deskripsi",
	"kuota_max",
	"kuota_terpakai",
	"batas_pendaftaran",
	"is_active",
	"is_finished",
	"created_at",
}

// Helper untuk setup mock gorm DB untuk package services
func setupServiceMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	dialector := postgres.New(postgres.Config{
		Conn:       sqlDB,
		DriverName: "postgres",
	})

	gormDB, err := gorm.Open(dialector, &gorm.Config{
		SkipDefaultTransaction: true,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("failed to open gorm db: %v", err)
	}

	prevDB := config.DB
	config.DB = gormDB

	cleanup := func() {
		config.DB = prevDB
		_ = sqlDB.Close()
	}

	return mock, cleanup
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Test BasePrompt Constant
// ─────────────────────────────────────────────────────────────────────────────

func TestBasePrompt(t *testing.T) {
	requiredPhrases := []string{
		"Bonita Assistant",
		"customer service virtual resmi dari travel umroh Bonita",
		"Menjawab pertanyaan mengenai Bonita Umroh",
		"Menjawab pertanyaan umum mengenai ibadah umroh",
		"Membantu customer membuat laporan pengaduan",
		"maksimal 2 sampai 5 kalimat",
		"Jangan menyebut bahwa kamu adalah Gemini",
	}

	for _, phrase := range requiredPhrases {
		if !strings.Contains(BasePrompt, phrase) {
			t.Errorf("BasePrompt expected to contain %q, but did not", phrase)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Test formatRupiah Helper
// ─────────────────────────────────────────────────────────────────────────────

func TestFormatRupiah(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected string
	}{
		{"Zero", 0, "0"},
		{"SingleDigit", 7, "7"},
		{"TwoDigits", 45, "45"},
		{"ThreeDigits", 999, "999"},
		{"Thousands", 1000, "1.000"},
		{"TensOfThousands", 25000, "25.000"},
		{"HundredsOfThousands", 750000, "750.000"},
		{"Millions_FiveMillion", 5000000, "5.000.000"},
		{"Millions_ThirtyFiveMillion", 35000000, "35.000.000"},
		{"HundredsOfMillions", 123456789, "123.456.789"},
		{"Billions", 1000000000, "1.000.000.000"},
		{"Trillions", 1250000000000, "1.250.000.000.000"},
		{"FloatRounding", 35000000.49, "35.000.000"},
		{"FloatRoundingUp", 35000000.85, "35.000.001"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatRupiah(tc.input)
			if got != tc.expected {
				t.Errorf("formatRupiah(%v) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Test jenisLabel Helper
// ─────────────────────────────────────────────────────────────────────────────

func TestJenisLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"EmptyString_ReturnsUmum", "", "Umum"},
		{"Reguler", "Reguler", "Reguler"},
		{"VIP", "VIP", "VIP"},
		{"VVIP", "VVIP", "VVIP"},
		{"Hemat", "Hemat", "Hemat"},
		{"Ramadhan", "Ramadhan", "Ramadhan"},
		{"AwalMusim", "Awal Musim", "Awal Musim"},
		{"CustomCategory", "Plus Turki", "Plus Turki"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := jenisLabel(tc.input)
			if got != tc.expected {
				t.Errorf("jenisLabel(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Test BuildBusinessFlowContext
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildBusinessFlowContext(t *testing.T) {
	ctx := BuildBusinessFlowContext()

	expectedSections := []string{
		"=== ALUR PROSES PENDAFTARAN BONITA ===",
		"1. Customer memilih paket umroh di halaman Paket Umroh.",
		"2. Customer melakukan pendaftaran melalui sistem Bonita.",
		"3. Sistem membuat Nomor Pendaftaran dengan format UMR-YYYYMMDD-XXXX",
		"4. Customer melakukan verifikasi menggunakan kode OTP yang dikirim ke email.",
		"5. Customer melakukan pembayaran DP pertama minimal Rp5.000.000 dan mengunggah bukti transfer.",
		"6. Admin memverifikasi pembayaran.",
		"7. Setelah pembayaran DP diterima, customer dapat mengunggah dokumen persyaratan.",
		"8. Admin memverifikasi dokumen yang diunggah.",
		"9. Jika pembayaran telah lunas dan seluruh dokumen telah diverifikasi, status jamaah berubah menjadi \"Siap Berangkat\".",
		"10. Customer dapat memantau seluruh status melalui Portal Jamaah.",
	}

	for _, sec := range expectedSections {
		if !strings.Contains(ctx, sec) {
			t.Errorf("BuildBusinessFlowContext missing expected text: %q", sec)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Test BuildFAQContext
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildFAQContext(t *testing.T) {
	ctx := BuildFAQContext()

	expectedFAQs := []string{
		"=== FAQ BONITA ===",
		"T: Berapa DP minimal untuk pendaftaran?",
		"J: DP pertama minimal Rp5.000.000.",
		"T: Apakah pembayaran berikutnya ada batas jumlahnya?",
		"J: Pembayaran berikutnya bebas, selama total tidak melebihi sisa tagihan paket.",
		"T: Apakah bukti pembayaran wajib diupload?",
		"J: Ya, bukti transfer wajib diunggah setiap kali melakukan pembayaran.",
		"T: Kapan saya bisa mengunggah dokumen?",
		"J: Dokumen baru dapat diunggah setelah pembayaran DP pertama diterima dan diverifikasi oleh admin.",
		"T: Dokumen apa saja yang wajib diunggah?",
		"J: Paspor, KTP, Akte Kelahiran, Kartu Keluarga, dan dokumen Vaksin. Foto bersifat opsional.",
		"T: Apa itu status \"Siap Berangkat\"?",
		"J: Status ini muncul setelah pembayaran lunas dan seluruh dokumen telah diverifikasi oleh admin Bonita.",
		"T: Bagaimana cara memantau status pendaftaran?",
		"J: Customer dapat memantau status, pembayaran, dan dokumen melalui Portal Jamaah.",
		"T: Berapa lama proses verifikasi pembayaran?",
		"J: Proses verifikasi dilakukan oleh admin Bonita. Biasanya berlangsung dalam 1x24 jam kerja.",
	}

	for _, faq := range expectedFAQs {
		if !strings.Contains(ctx, faq) {
			t.Errorf("BuildFAQContext missing expected text: %q", faq)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 6. Test BuildPackageContext
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildPackageContext(t *testing.T) {
	t.Run("EmptyDatabase_ReturnsNoPackagesMessage", func(t *testing.T) {
		mock, cleanup := setupServiceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" ORDER BY tanggal_berangkat ASC LIMIT $1`)).
			WithArgs(10).
			WillReturnRows(sqlmock.NewRows(testPaketUmrohCols))

		output := BuildPackageContext()

		expected := "Saat ini belum ada paket umroh yang tersedia.\n"
		if output != expected {
			t.Errorf("BuildPackageContext() = %q; want %q", output, expected)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("PopulatedDatabase_FormatsPackagesCorrectly", func(t *testing.T) {
		mock, cleanup := setupServiceMockDB(t)
		defer cleanup()

		tBerangkat1 := time.Date(2026, time.November, 20, 9, 0, 0, 0, time.UTC)
		tBerangkat2 := time.Date(2026, time.December, 15, 10, 0, 0, 0, time.UTC)
		tBerangkat3 := time.Date(2027, time.January, 10, 8, 30, 0, 0, time.UTC)
		now := time.Now()

		rows := sqlmock.NewRows(testPaketUmrohCols).
			// Paket 1: Normal kuota sisa 25, jenis "VIP"
			AddRow(uuid.New(), "Umrah Akhir Tahun VIP", "VIP", "foto1.jpg", 35000000.0, tBerangkat1, 9, "Deskripsi VIP", 40, 15, 7, true, false, now).
			// Paket 2: Kuota habis sisa 0, jenis kosong ("" -> fallback ke "Umum")
			AddRow(uuid.New(), "Umrah Reguler Barokah", "", "foto2.jpg", 28500000.0, tBerangkat2, 12, "Deskripsi Reguler", 30, 30, 5, true, false, now).
			// Paket 3: Overbooked kuota terpakai 50 > max 45 (sisaKuota < 0 -> clamp ke 0)
			AddRow(uuid.New(), "Umrah Hemat Awal Tahun", "Hemat", "foto3.jpg", 24000000.0, tBerangkat3, 10, "Deskripsi Hemat", 45, 50, 3, true, false, now)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" ORDER BY tanggal_berangkat ASC LIMIT $1`)).
			WithArgs(10).
			WillReturnRows(rows)

		output := BuildPackageContext()

		if !strings.Contains(output, "=== DAFTAR PAKET UMROH (Data Terkini) ===") {
			t.Errorf("Expected header not found in output: %s", output)
		}

		// Verifikasi Paket 1
		p1Checks := []string{
			"Paket 1:",
			"Nama        : Umrah Akhir Tahun VIP",
			"Jenis       : VIP",
			"Harga       : Rp35.000.000",
			"Durasi      : 9 hari",
			"Berangkat   : 20 November 2026",
			"Kuota Maks  : 40",
			"Terpakai    : 15",
			"Sisa Kuota  : 25",
		}
		for _, check := range p1Checks {
			if !strings.Contains(output, check) {
				t.Errorf("Paket 1 missing line %q in output: %s", check, output)
			}
		}

		// Verifikasi Paket 2 (jenis fallback ke Umum, sisa kuota 0)
		p2Checks := []string{
			"Paket 2:",
			"Nama        : Umrah Reguler Barokah",
			"Jenis       : Umum",
			"Harga       : Rp28.500.000",
			"Durasi      : 12 hari",
			"Berangkat   : 15 December 2026",
			"Kuota Maks  : 30",
			"Terpakai    : 30",
			"Sisa Kuota  : 0",
		}
		for _, check := range p2Checks {
			if !strings.Contains(output, check) {
				t.Errorf("Paket 2 missing line %q in output: %s", check, output)
			}
		}

		// Verifikasi Paket 3 (overbooked clamp sisa kuota < 0 menjadi 0)
		p3Checks := []string{
			"Paket 3:",
			"Nama        : Umrah Hemat Awal Tahun",
			"Jenis       : Hemat",
			"Harga       : Rp24.000.000",
			"Durasi      : 10 hari",
			"Berangkat   : 10 January 2027",
			"Kuota Maks  : 45",
			"Terpakai    : 50",
			"Sisa Kuota  : 0",
		}
		for _, check := range p3Checks {
			if !strings.Contains(output, check) {
				t.Errorf("Paket 3 missing line %q in output: %s", check, output)
			}
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 7. Test BuildBonitaContext (Integration of all sub-contexts)
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildBonitaContext(t *testing.T) {
	t.Run("WithPackages", func(t *testing.T) {
		mock, cleanup := setupServiceMockDB(t)
		defer cleanup()

		tBerangkat := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
		now := time.Now()

		rows := sqlmock.NewRows(testPaketUmrohCols).
			AddRow(uuid.New(), "Paket Bonita Spesial", "VIP", "foto.jpg", 32000000.0, tBerangkat, 10, "Deskripsi", 30, 10, 5, true, false, now)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" ORDER BY tanggal_berangkat ASC LIMIT $1`)).
			WithArgs(10).
			WillReturnRows(rows)

		output := BuildBonitaContext()

		// Verifikasi pembungkus utama
		if !strings.HasPrefix(output, "\n\n=== KONTEKS INFORMASI BONITA ===\n\n") {
			t.Errorf("BuildBonitaContext expected to start with header wrapper, got: %s", output)
		}
		if !strings.HasSuffix(output, "=== AKHIR KONTEKS ===\n\n") {
			t.Errorf("BuildBonitaContext expected to end with footer wrapper, got: %s", output)
		}

		// Verifikasi memuat ketiga sub-konteks
		if !strings.Contains(output, "=== DAFTAR PAKET UMROH (Data Terkini) ===") {
			t.Errorf("BuildBonitaContext missing package context")
		}
		if !strings.Contains(output, "Paket Bonita Spesial") {
			t.Errorf("BuildBonitaContext missing specific package name")
		}
		if !strings.Contains(output, "=== ALUR PROSES PENDAFTARAN BONITA ===") {
			t.Errorf("BuildBonitaContext missing business flow context")
		}
		if !strings.Contains(output, "=== FAQ BONITA ===") {
			t.Errorf("BuildBonitaContext missing FAQ context")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})

	t.Run("WithoutPackages", func(t *testing.T) {
		mock, cleanup := setupServiceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "paket_umroh" ORDER BY tanggal_berangkat ASC LIMIT $1`)).
			WithArgs(10).
			WillReturnRows(sqlmock.NewRows(testPaketUmrohCols))

		output := BuildBonitaContext()

		if !strings.Contains(output, "Saat ini belum ada paket umroh yang tersedia.") {
			t.Errorf("BuildBonitaContext expected empty package notice")
		}
		if !strings.Contains(output, "=== ALUR PROSES PENDAFTARAN BONITA ===") {
			t.Errorf("BuildBonitaContext missing business flow context")
		}
		if !strings.Contains(output, "=== FAQ BONITA ===") {
			t.Errorf("BuildBonitaContext missing FAQ context")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("Unfulfilled expectations: %v", err)
		}
	})
}

