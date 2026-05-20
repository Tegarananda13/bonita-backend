package config

import (
	"fmt"
	"log"

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
		log.Fatal("Gagal koneksi ke database:", err)
	}

	fmt.Println("Database connected!")

	DB = database
}