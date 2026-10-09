package helpers

import (
	"bonita-backend/models"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"gorm.io/gorm"
)

func GenerateNomorInvoice(db *gorm.DB) (string, error) {
	year := time.Now().Year()
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	for {
		code := make([]byte, 6)

		for i := range code {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
			if err != nil {
				return "", err
			}

			code[i] = chars[n.Int64()]
		}

		nomor := fmt.Sprintf("INV-BNT-%d-%s", year, string(code))

		// Pastikan nomor belum pernah digunakan
		var count int64
		if err := db.Model(&models.Invoice{}).
			Where("nomor_invoice = ?", nomor).
			Count(&count).Error; err != nil {
			return "", err
		}

		if count == 0 {
			return nomor, nil
		}
	}
}
