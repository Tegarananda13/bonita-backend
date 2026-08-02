package helpers

import (
	"bonita-backend/models"
	"fmt"
	"time"

	"gorm.io/gorm"
)

func GenerateNomorInvoice(db *gorm.DB) (string, error) {
	year := time.Now().Year()

	var count int64

	if err := db.Model(&models.Invoice{}).
		Where("nomor_invoice LIKE ?", fmt.Sprintf("INV-BNT-%d-%%", year)).
		Count(&count).Error; err != nil {
		return "", err
	}

	seq := count + 1
	nomor := fmt.Sprintf("INV-BNT-%d-%06d", year, seq)

	return nomor, nil
}