package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// Load .env
	godotenv.Load(".env")

	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_SSLMODE"),
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Gagal koneksi DB:", err)
	}

	// Identifikasi ID duplikat: paket dengan nama+harga+durasi sama,
	// simpan yang paling lama (created_at terkecil = asli),
	// hapus yang lebih baru yang tidak ada di tabel pendaftaran
	type IDRow struct {
		ID string
	}
	var dupIDs []IDRow

	db.Raw(`
		SELECT id FROM (
			SELECT id,
				ROW_NUMBER() OVER (
					PARTITION BY nama_paket, harga, durasi
					ORDER BY created_at ASC
				) AS rn
			FROM paket_umroh
		) ranked
		WHERE rn > 1
		  AND id NOT IN (SELECT DISTINCT paket_id FROM pendaftaran WHERE paket_id IS NOT NULL)
	`).Scan(&dupIDs)

	if len(dupIDs) == 0 {
		fmt.Println("Tidak ada duplikat ditemukan — database sudah bersih.")
		return
	}

	ids := make([]string, len(dupIDs))
	for i, row := range dupIDs {
		ids[i] = row.ID
	}

	fmt.Printf("Ditemukan %d baris duplikat: %v\n", len(ids), ids)

	// Hapus fasilitas yang terkait dengan paket duplikat terlebih dahulu
	res1 := db.Exec("DELETE FROM detail_fasilitas WHERE paket_id IN ?", ids)
	if res1.Error != nil {
		log.Fatal("Error hapus fasilitas duplikat:", res1.Error)
	}
	fmt.Printf("Hapus %d baris detail_fasilitas terkait\n", res1.RowsAffected)

	// Hapus paket duplikat
	res2 := db.Exec("DELETE FROM paket_umroh WHERE id IN ?", ids)
	if res2.Error != nil {
		log.Fatal("Error hapus paket duplikat:", res2.Error)
	}
	fmt.Printf("Berhasil menghapus %d baris duplikat dari paket_umroh\n", res2.RowsAffected)
}
