package config

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var DB *gorm.DB

func ConnectDatabase() {
	dsn := "host=localhost user=postgres password=konfidentiell dbname=bonita_umroh_db port=5432 sslmode=disable"

	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})

	if err != nil {
		log.Fatalf("Gagal koneksi ke database: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		log.Fatalf("Gagal mendapatkan database instance: %v", err)
	}

	// Connection Pool Configuration
	sqlDB.SetMaxOpenConns(10)          // maksimal koneksi aktif
	sqlDB.SetMaxIdleConns(5)           // koneksi idle yang disimpan
	sqlDB.SetConnMaxLifetime(time.Hour) // koneksi direcycle tiap 1 jam
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)

	fmt.Println("Database connected!")

	DB = database
}