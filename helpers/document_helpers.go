package helpers

import (
	"bonita-backend/models"
	"sort"
	"strings"
)

// RequiredDocTypes adalah 6 jenis dokumen wajib pendaftaran umroh.
var RequiredDocTypes = []string{
	"paspor",
	"ktp",
	"kartu_keluarga",
	"akta_lahir",
	"vaksin",
	"pas_foto",
}

// NormalizeRequiredDocType memetakan berbagai alias/variasi jenis dokumen ke 6 jenis dokumen wajib.
// Mengembalikan "" jika bukan merupakan salah satu dari 6 dokumen wajib (misal: "lainnya", "visa", "tiket_pesawat", "nusuk").
func NormalizeRequiredDocType(jenis string) string {
	cleaned := strings.ToLower(strings.TrimSpace(jenis))
	cleaned = strings.ReplaceAll(cleaned, "-", "_")
	cleaned = strings.ReplaceAll(cleaned, " ", "_")
	switch cleaned {
	case "paspor", "passport":
		return "paspor"
	case "ktp":
		return "ktp"
	case "kartu_keluarga", "kk":
		return "kartu_keluarga"
	case "akta_lahir", "akte_kelahiran", "akta", "aktalahir", "akte":
		return "akta_lahir"
	case "vaksin", "vaksinasi", "vaccine":
		return "vaksin"
	case "pas_foto", "foto", "pasfoto", "pas_photo", "photo", "pasphoto":
		return "pas_foto"
	default:
		return ""
	}
}

// CalculateDocumentStatus menghitung document_status dari daftar dokumen pendaftaran.
// Status yang dihasilkan konsisten dengan 5 status sistem:
// - belum: belum ada dokumen yang diunggah
// - pending: ada dokumen wajib yang sedang menunggu verifikasi admin
// - revisi: tidak ada yang pending, namun ada dokumen wajib yang ditolak oleh admin
// - lengkap: seluruh 6 dokumen wajib telah diverifikasi dan berstatus diterima
// - belum_lengkap: tidak ada yang pending/ditolak, namun belum seluruh 6 dokumen wajib terpenuhi
func CalculateDocumentStatus(dokumenList []models.Dokumen) string {
	if len(dokumenList) == 0 {
		return DocumentBelum
	}

	// Buat copy dan urutkan dokumen berdasarkan CreatedAt ASC agar dokumen terbaru menimpa status sebelumnya
	docs := make([]models.Dokumen, len(dokumenList))
	copy(docs, dokumenList)
	sort.SliceStable(docs, func(i, j int) bool {
		return docs[i].CreatedAt.Before(docs[j].CreatedAt)
	})

	// Peta status terkini untuk masing-masing canonical required doc type
	latestStatus := make(map[string]string)

	for _, d := range docs {
		canonical := NormalizeRequiredDocType(d.JenisDokumen)
		if canonical == "" {
			// Dokumen non-wajib (misal: "lainnya", "visa", dll) tidak dihitung dalam syarat kelengkapan wajib
			continue
		}
		latestStatus[canonical] = d.StatusValidasi
	}

	// Jika belum ada satu pun dokumen wajib yang diunggah (misal hanya upload "lainnya")
	if len(latestStatus) == 0 {
		return DocumentBelumLengkap
	}

	hasPending := false
	hasDitolak := false
	diterimaCount := 0

	for _, reqType := range RequiredDocTypes {
		status, exists := latestStatus[reqType]
		if !exists {
			continue
		}
		switch status {
		case PaymentVerificationPending:
			hasPending = true
		case PaymentVerificationDitolak:
			hasDitolak = true
		case PaymentVerificationDiterima:
			diterimaCount++
		}
	}

	if hasPending {
		return DocumentPending
	}
	if hasDitolak {
		return DocumentRevisi
	}
	if diterimaCount == len(RequiredDocTypes) {
		return DocumentLengkap
	}
	return DocumentBelumLengkap
}

// IsDocumentLengkap memeriksa apakah seluruh 6 dokumen wajib sudah berstatus lengkap.
func IsDocumentLengkap(dokumenList []models.Dokumen) bool {
	return CalculateDocumentStatus(dokumenList) == DocumentLengkap
}

// GetDocumentCompletenessLabel mengembalikan label kelengkapan dokumen sesuai UI: "Dokumen Lengkap" atau "Dokumen Belum Lengkap".
func GetDocumentCompletenessLabel(dokumenList []models.Dokumen) string {
	if IsDocumentLengkap(dokumenList) {
		return "Dokumen Lengkap"
	}
	return "Dokumen Belum Lengkap"
}
