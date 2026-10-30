package config

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestBuildDSN(t *testing.T) {
	// Simpan environment variables asli dan restore di akhir test
	envKeys := []string{"DB_HOST", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_PORT", "DB_SSLMODE"}
	origEnvs := make(map[string]string)
	for _, k := range envKeys {
		origEnvs[k] = os.Getenv(k)
	}
	defer func() {
		for k, v := range origEnvs {
			if v != "" {
				_ = os.Setenv(k, v)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}()

	tests := []struct {
		name     string
		envs     map[string]string
		expected string
	}{
		{
			name: "StandardConfiguration",
			envs: map[string]string{
				"DB_HOST":     "localhost",
				"DB_USER":     "postgres",
				"DB_PASSWORD": "mypassword",
				"DB_NAME":     "bonita_umroh_db",
				"DB_PORT":     "5432",
				"DB_SSLMODE":  "disable",
			},
			expected: "host=localhost user=postgres password=mypassword dbname=bonita_umroh_db port=5432 sslmode=disable",
		},
		{
			name: "CustomRemoteConfiguration",
			envs: map[string]string{
				"DB_HOST":     "10.0.0.1",
				"DB_USER":     "bonita_admin",
				"DB_PASSWORD": "SecurePassword#123",
				"DB_NAME":     "bonita_prod",
				"DB_PORT":     "5433",
				"DB_SSLMODE":  "require",
			},
			expected: "host=10.0.0.1 user=bonita_admin password=SecurePassword#123 dbname=bonita_prod port=5433 sslmode=require",
		},
		{
			name: "EmptyConfiguration",
			envs: map[string]string{
				"DB_HOST":     "",
				"DB_USER":     "",
				"DB_PASSWORD": "",
				"DB_NAME":     "",
				"DB_PORT":     "",
				"DB_SSLMODE":  "",
			},
			expected: "host= user= password= dbname= port= sslmode=",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.envs {
				if v != "" {
					_ = os.Setenv(k, v)
				} else {
					_ = os.Unsetenv(k)
				}
			}

			dsn := BuildDSN()
			if dsn != tc.expected {
				t.Errorf("BuildDSN() = %q; want %q", dsn, tc.expected)
			}
		})
	}
}

func TestConfigureConnectionPool(t *testing.T) {
	t.Run("Success_ValidDatabase", func(t *testing.T) {
		sqlDB, _, err := sqlmock.New()
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

		err = ConfigureConnectionPool(gdb)
		if err != nil {
			t.Errorf("ConfigureConnectionPool(gdb) expected nil error, got: %v", err)
		}

		// Verifikasi parameter connection pool diterapkan
		stats := sqlDB.Stats()
		if stats.MaxOpenConnections != 10 {
			t.Errorf("Expected MaxOpenConnections 10, got %d", stats.MaxOpenConnections)
		}
	})

	t.Run("Failure_InvalidDatabaseInstance", func(t *testing.T) {
		// GORM DB tanpa underlying connection pool
		invalidGDB := &gorm.DB{Config: &gorm.Config{}}

		err := ConfigureConnectionPool(invalidGDB)
		if err == nil {
			t.Errorf("ConfigureConnectionPool(invalidGDB) expected error, got nil")
		}
	})
}

func TestConnectDatabase_ConnectionFailure(t *testing.T) {
	// Simpan environment variables asli
	envKeys := []string{"DB_HOST", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_PORT", "DB_SSLMODE"}
	origEnvs := make(map[string]string)
	for _, k := range envKeys {
		origEnvs[k] = os.Getenv(k)
	}
	defer func() {
		for k, v := range origEnvs {
			if v != "" {
				_ = os.Setenv(k, v)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}()

	// Pasang environment variable dengan host/port yang pasti gagal koneksi
	_ = os.Setenv("DB_HOST", "127.0.0.1")
	_ = os.Setenv("DB_PORT", "1") // Port 1 hampir selalu connection refused
	_ = os.Setenv("DB_USER", "invalid_user")
	_ = os.Setenv("DB_PASSWORD", "invalid_pass")
	_ = os.Setenv("DB_NAME", "invalid_db")
	_ = os.Setenv("DB_SSLMODE", "disable")

	var fatalLogged bool
	var fatalMessage string

	prevLogFatal := logFatal
	logFatal = func(format string, v ...interface{}) {
		fatalLogged = true
		fatalMessage = fmt.Sprintf(format, v...)
	}
	defer func() {
		logFatal = prevLogFatal
	}()

	ConnectDatabase()

	if !fatalLogged {
		t.Errorf("Expected ConnectDatabase() to log fatal error on connection failure, but it did not")
	}
	if !strings.Contains(fatalMessage, "Gagal koneksi ke database") {
		t.Errorf("Expected error message to contain 'Gagal koneksi ke database', got: %s", fatalMessage)
	}
}

func TestConnectDatabase_Integration(t *testing.T) {
	if os.Getenv("TEST_INTEGRATION_DB") != "true" {
		t.Skip("Skipping DB integration test; set TEST_INTEGRATION_DB=true to run against live PostgreSQL")
	}

	prevDB := DB
	defer func() { DB = prevDB }()

	ConnectDatabase()

	if DB == nil {
		t.Errorf("Expected DB to not be nil after successful ConnectDatabase()")
	}
}

