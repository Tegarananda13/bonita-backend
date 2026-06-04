package helpers

import (
	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/google/uuid"
)

func UpdateStatusPendaftaran(pendaftaranID uuid.UUID) {

	var pendaftaran models.Pendaftaran

	if err := config.DB.
		First(&pendaftaran, "id = ?", pendaftaranID).Error; err != nil {

		return
	}

	var status string

	switch {

	case pendaftaran.PaymentStatus == PaymentLunas &&
		pendaftaran.DocumentStatus == DocumentLengkap:

		status = StatusSiapBerangkat

	case pendaftaran.PaymentStatus == PaymentLunas:

		status = StatusMenungguDokumen

	case pendaftaran.DocumentStatus == DocumentLengkap:

		status = StatusMenungguPembayaran

	case pendaftaran.PaymentStatus == PaymentDP:

		status = StatusProses

	default:

		status = StatusProses
	}

	config.DB.
		Model(&pendaftaran).
		Update("status", status)
}