package config

import (
	"os"
	"reflect"
	"testing"

	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestMigrate_NilDB(t *testing.T) {
	prevDB := DB
	DB = nil
	defer func() { DB = prevDB }()

	// Memastikan pemanggilan Migrate() saat DB == nil tidak menyebabkan panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Migrate() panicked on nil DB: %v", r)
		}
	}()

	Migrate()
}

func TestGetMigratedModels(t *testing.T) {
	modelsList := GetMigratedModels()

	expectedCount := 16
	if len(modelsList) != expectedCount {
		t.Fatalf("GetMigratedModels() returned %d models, want %d", len(modelsList), expectedCount)
	}

	expectedTypes := []string{
		"*models.Invoice",
		"*models.Customer",
		"*models.User",
		"*models.PaketUmroh",
		"*models.Pendaftaran",
		"*models.Dokumen",
		"*models.Pembayaran",
		"*models.VerifikasiOTP",
		"*models.ChatbotLog",
		"*models.CustomerSession",
		"*models.DetailFasilitas",
		"*models.Pengaduan",
		"*models.FotoPaket",
		"*models.FotoFasilitas",
		"*models.Ulasan",
		"*models.FotoUlasan",
	}

	for i, m := range modelsList {
		if m == nil {
			t.Errorf("Model at index %d is nil", i)
			continue
		}

		typeName := reflect.TypeOf(m).String()
		if typeName != expectedTypes[i] {
			t.Errorf("Model at index %d: got type %s, want %s", i, typeName, expectedTypes[i])
		}
	}
}

func TestMigrate_UnitWithMockDB(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer sqlDB.Close()

	dialector := postgres.New(postgres.Config{
		Conn: sqlDB,
	})
	gdb, err := gorm.Open(dialector, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatalf("Failed to open gorm with sqlmock: %v", err)
	}

	prevDB := DB
	DB = gdb
	defer func() { DB = prevDB }()

	// Mock pemanggilan AutoMigrate agar tidak memicu query introspeksi postgres catalog pada sqlmock
	var autoMigratedModels []interface{}
	prevAutoMigrateFn := autoMigrateFn
	autoMigrateFn = func(db *gorm.DB, dst ...interface{}) error {
		autoMigratedModels = append(autoMigratedModels, dst...)
		return nil
	}
	defer func() { autoMigrateFn = prevAutoMigrateFn }()

	// Dalam Migrate() terdapat 63 pemanggilan DB.Exec (ALTER TABLE, UPDATE, INSERT, DROP)
	// Kita pasang expectation untuk setiap eksekusi DDL/DML tersebut
	totalExecStatements := 63
	for i := 0; i < totalExecStatements; i++ {
		mock.ExpectExec(".*").WillReturnResult(sqlmock.NewResult(0, 1))
	}

	Migrate()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled SQL expectations: %v", err)
	}

	// Verifikasi bahwa AutoMigrate menerima seluruh 16 model
	expectedModelCount := 16
	if len(autoMigratedModels) != expectedModelCount {
		t.Errorf("autoMigrateFn received %d models, want %d", len(autoMigratedModels), expectedModelCount)
	}

	// Verifikasi beberapa model penting terdaftar
	foundInvoice := false
	foundCustomer := false
	foundPendaftaran := false
	for _, m := range autoMigratedModels {
		switch m.(type) {
		case *models.Invoice:
			foundInvoice = true
		case *models.Customer:
			foundCustomer = true
		case *models.Pendaftaran:
			foundPendaftaran = true
		}
	}
	if !foundInvoice || !foundCustomer || !foundPendaftaran {
		t.Errorf("Missing essential models in AutoMigrate call: Invoice=%v, Customer=%v, Pendaftaran=%v",
			foundInvoice, foundCustomer, foundPendaftaran)
	}
}

func TestMigrate_Integration(t *testing.T) {
	if os.Getenv("TEST_INTEGRATION_DB") != "true" {
		t.Skip("Skipping DB migration integration test; set TEST_INTEGRATION_DB=true to run against live PostgreSQL")
	}

	if DB == nil {
		t.Skip("Database not connected, skipping integration test")
	}

	// Memastikan migrasi berjalan idempoten tanpa error pada database nyata
	Migrate()
}
