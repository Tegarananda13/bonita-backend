package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var DB *gorm.DB

var logFatal = log.Fatalf

// BuildDSN membentuk connection string PostgreSQL dari environment variables.
func BuildDSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_SSLMODE"),
	)
}

// ConfigureConnectionPool mengonfigurasi connection pool SQL database.
func ConfigureConnectionPool(database *gorm.DB) error {
	sqlDB, err := database.DB()
	if err != nil {
		return err
	}

	// Connection Pool Configuration
	sqlDB.SetMaxOpenConns(10)           // maksimal koneksi aktif
	sqlDB.SetMaxIdleConns(5)            // koneksi idle yang disimpan
	sqlDB.SetConnMaxLifetime(time.Hour) // koneksi direcycle tiap 1 jam
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)
	return nil
}

func ConnectDatabase() {
	dsn := BuildDSN()

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
		// FK dikelola manual via SQL migration — cegah AutoMigrate membuat FK otomatis
		// yang bisa konflik dengan constraint yang sudah ada (Phase 2 & 3).
		DisableForeignKeyConstraintWhenMigrating: true,
	})

	if err != nil {
		logFatal("Gagal koneksi ke database: %v", err)
		return
	}

	if err := ConfigureConnectionPool(database); err != nil {
		logFatal("Gagal mendapatkan database instance: %v", err)
		return
	}

	fmt.Println("Database connected!")

	DB = database
}
