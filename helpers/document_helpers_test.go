package helpers_test

import (
	"testing"
	"time"

	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/google/uuid"
)

func makeDoc(jenis, status string, createdAt time.Time) models.Dokumen {
	return models.Dokumen{
		ID:             uuid.New(),
		JenisDokumen:   jenis,
		StatusValidasi: status,
		CreatedAt:      createdAt,
	}
}

// TestDocumentCompleteness_10MinimalCases menguji ke-10 skenario minimal yang ditentukan.
func TestDocumentCompleteness_10MinimalCases(t *testing.T) {
	now := time.Now()

	// 1. Tidak ada dokumen → Belum Lengkap
	t.Run("1_EmptyDocuments", func(t *testing.T) {
		docs := []models.Dokumen{}
		if helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=false, got true")
		}
		if label := helpers.GetDocumentCompletenessLabel(docs); label != "Dokumen Belum Lengkap" {
			t.Errorf("Expected 'Dokumen Belum Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentBelum {
			t.Errorf("Expected DocumentBelum ('belum'), got '%s'", status)
		}
	})

	// 2. Hanya 1 dokumen wajib → Belum Lengkap
	t.Run("2_SingleRequiredDocument", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
		}
		if helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=false, got true")
		}
		if label := helpers.GetDocumentCompletenessLabel(docs); label != "Dokumen Belum Lengkap" {
			t.Errorf("Expected 'Dokumen Belum Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentBelumLengkap {
			t.Errorf("Expected DocumentBelumLengkap ('belum_lengkap'), got '%s'", status)
		}
	})

	// 3. 5 dari 6 dokumen wajib → Belum Lengkap
	t.Run("3_FiveOfSixRequiredDocuments", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			// missing: pas_foto
		}
		if helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=false, got true")
		}
		if label := helpers.GetDocumentCompletenessLabel(docs); label != "Dokumen Belum Lengkap" {
			t.Errorf("Expected 'Dokumen Belum Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentBelumLengkap {
			t.Errorf("Expected DocumentBelumLengkap ('belum_lengkap'), got '%s'", status)
		}
	})

	// 4. Keenam dokumen wajib tersedia → Lengkap
	t.Run("4_AllSixRequiredDocuments", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			makeDoc("pas_foto", helpers.PaymentVerificationDiterima, now),
		}
		if !helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=true, got false")
		}
		if label := helpers.GetDocumentCompletenessLabel(docs); label != "Dokumen Lengkap" {
			t.Errorf("Expected 'Dokumen Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentLengkap {
			t.Errorf("Expected DocumentLengkap ('lengkap'), got '%s'", status)
		}
	})

	// 5. Keenam dokumen wajib + Lainnya → Lengkap
	t.Run("5_AllSixRequiredPlusLainnya", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			makeDoc("pas_foto", helpers.PaymentVerificationDiterima, now),
			makeDoc("lainnya", helpers.PaymentVerificationDiterima, now),
		}
		if !helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=true, got false")
		}
		if label := helpers.GetDocumentCompletenessLabel(docs); label != "Dokumen Lengkap" {
			t.Errorf("Expected 'Dokumen Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentLengkap {
			t.Errorf("Expected DocumentLengkap ('lengkap'), got '%s'", status)
		}
	})

	// 6. Lima dokumen wajib + Lainnya → Belum Lengkap
	t.Run("6_FiveRequiredPlusLainnya", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			// missing pas_foto, but has lainnya
			makeDoc("lainnya", helpers.PaymentVerificationDiterima, now),
		}
		if helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=false, got true")
		}
		if label := helpers.GetDocumentCompletenessLabel(docs); label != "Dokumen Belum Lengkap" {
			t.Errorf("Expected 'Dokumen Belum Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentBelumLengkap {
			t.Errorf("Expected DocumentBelumLengkap ('belum_lengkap'), got '%s'", status)
		}
	})

	// 7. Ada 6 record tetapi salah satunya adalah Lainnya → Belum Lengkap
	t.Run("7_SixRecordsWithOneLainnya", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			makeDoc("lainnya", helpers.PaymentVerificationDiterima, now), // 6 total records!
		}
		if len(docs) != 6 {
			t.Fatalf("Expected exactly 6 records, got %d", len(docs))
		}
		if helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=false despite having 6 records")
		}
		if label := helpers.GetDocumentCompletenessLabel(docs); label != "Dokumen Belum Lengkap" {
			t.Errorf("Expected 'Dokumen Belum Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentBelumLengkap {
			t.Errorf("Expected DocumentBelumLengkap ('belum_lengkap'), got '%s'", status)
		}
	})

	// 8. Ada duplicate salah satu dokumen → tetap berdasarkan unique jenis dokumen
	t.Run("8_DuplicateDocumentTypeDoesNotSatisfyMissingRequired", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now.Add(time.Minute)), // duplicate Paspor
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			// missing pas_foto (total records: 6)
		}
		if len(docs) != 6 {
			t.Fatalf("Expected exactly 6 records, got %d", len(docs))
		}
		if helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=false due to missing pas_foto despite 6 records")
		}
		if label := helpers.GetDocumentCompletenessLabel(docs); label != "Dokumen Belum Lengkap" {
			t.Errorf("Expected 'Dokumen Belum Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentBelumLengkap {
			t.Errorf("Expected DocumentBelumLengkap ('belum_lengkap'), got '%s'", status)
		}
	})

	// 9. Setelah dokumen yang sebelumnya ditolak diganti dengan dokumen baru dan diterima → status dihitung ulang dengan benar
	t.Run("9_ReplaceRejectedDocumentFlow", func(t *testing.T) {
		t1 := now
		t2 := now.Add(1 * time.Hour)
		t3 := now.Add(2 * time.Hour)

		// Fase A: Paspor ditolak, 5 dokumen lainnya diterima
		docsA := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDitolak, t1),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, t1),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, t1),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, t1),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, t1),
			makeDoc("pas_foto", helpers.PaymentVerificationDiterima, t1),
		}
		if statusA := helpers.CalculateDocumentStatus(docsA); statusA != helpers.DocumentRevisi {
			t.Errorf("Phase A: Expected DocumentRevisi ('revisi'), got '%s'", statusA)
		}
		if helpers.IsDocumentLengkap(docsA) {
			t.Errorf("Phase A: Expected IsDocumentLengkap=false")
		}

		// Fase B: Customer mengunggah Paspor pengganti (status: pending)
		docsB := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDitolak, t1),
			makeDoc("paspor", helpers.PaymentVerificationPending, t2), // pengganti terbaru
			makeDoc("ktp", helpers.PaymentVerificationDiterima, t1),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, t1),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, t1),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, t1),
			makeDoc("pas_foto", helpers.PaymentVerificationDiterima, t1),
		}
		if statusB := helpers.CalculateDocumentStatus(docsB); statusB != helpers.DocumentPending {
			t.Errorf("Phase B: Expected DocumentPending ('pending'), got '%s'", statusB)
		}
		if helpers.IsDocumentLengkap(docsB) {
			t.Errorf("Phase B: Expected IsDocumentLengkap=false")
		}

		// Fase C: Admin memverifikasi dan menerima Paspor pengganti
		docsC := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDitolak, t1),
			makeDoc("paspor", helpers.PaymentVerificationDiterima, t3), // diverifikasi diterima
			makeDoc("ktp", helpers.PaymentVerificationDiterima, t1),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, t1),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, t1),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, t1),
			makeDoc("pas_foto", helpers.PaymentVerificationDiterima, t1),
		}
		if statusC := helpers.CalculateDocumentStatus(docsC); statusC != helpers.DocumentLengkap {
			t.Errorf("Phase C: Expected DocumentLengkap ('lengkap'), got '%s'", statusC)
		}
		if !helpers.IsDocumentLengkap(docsC) {
			t.Errorf("Phase C: Expected IsDocumentLengkap=true")
		}
		if labelC := helpers.GetDocumentCompletenessLabel(docsC); labelC != "Dokumen Lengkap" {
			t.Errorf("Phase C: Expected label 'Dokumen Lengkap', got '%s'", labelC)
		}
	})

	// 10. Menghapus salah satu dokumen wajib → kembali menjadi Belum Lengkap jika sistem mengizinkan penghapusan
	t.Run("10_DeleteOneRequiredDocument", func(t *testing.T) {
		docsBefore := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			makeDoc("pas_foto", helpers.PaymentVerificationDiterima, now),
		}
		if !helpers.IsDocumentLengkap(docsBefore) {
			t.Fatalf("Expected initial state to be Lengkap")
		}

		// Menghapus Pas Foto (tersisa 5 dokumen)
		docsAfter := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
		}
		if helpers.IsDocumentLengkap(docsAfter) {
			t.Errorf("Expected IsDocumentLengkap=false after deleting required document")
		}
		if label := helpers.GetDocumentCompletenessLabel(docsAfter); label != "Dokumen Belum Lengkap" {
			t.Errorf("Expected 'Dokumen Belum Lengkap', got '%s'", label)
		}
		if status := helpers.CalculateDocumentStatus(docsAfter); status != helpers.DocumentBelumLengkap {
			t.Errorf("Expected DocumentBelumLengkap ('belum_lengkap'), got '%s'", status)
		}
	})
}

// TestDocumentCompleteness_AliasesAndVariations menguji integrasi variasi jenis dokumen (foto vs pas_foto, kk vs kartu_keluarga, dll).
func TestDocumentCompleteness_AliasesAndVariations(t *testing.T) {
	now := time.Now()

	// Skenario Jamaah upload dengan "foto" (pilihan UI Portal)
	t.Run("PortalCustomer_FotoAlias", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationDiterima, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationDiterima, now),
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			makeDoc("foto", helpers.PaymentVerificationDiterima, now), // key di portal
		}
		if !helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=true for 'foto' alias")
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentLengkap {
			t.Errorf("Expected DocumentLengkap, got '%s'", status)
		}
	})

	// Skenario Admin upload dengan "pas_foto"
	t.Run("Admin_PasFotoAlias", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationDiterima, now),
			makeDoc("ktp", helpers.PaymentVerificationDiterima, now),
			makeDoc("kk", helpers.PaymentVerificationDiterima, now),             // alias KK
			makeDoc("akte_kelahiran", helpers.PaymentVerificationDiterima, now), // alias Akte Kelahiran
			makeDoc("vaksin", helpers.PaymentVerificationDiterima, now),
			makeDoc("pas_foto", helpers.PaymentVerificationDiterima, now), // pas_foto
		}
		if !helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=true for mixed aliases")
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentLengkap {
			t.Errorf("Expected DocumentLengkap, got '%s'", status)
		}
	})

	// Skenario Dokumen baru diupload jamaah (semua pending, belum diverifikasi admin)
	t.Run("PendingStatus_WhenDocsUploadedNotYetVerified", func(t *testing.T) {
		docs := []models.Dokumen{
			makeDoc("paspor", helpers.PaymentVerificationPending, now),
			makeDoc("ktp", helpers.PaymentVerificationPending, now),
			makeDoc("kartu_keluarga", helpers.PaymentVerificationPending, now),
			makeDoc("akta_lahir", helpers.PaymentVerificationPending, now),
			makeDoc("vaksin", helpers.PaymentVerificationPending, now),
			makeDoc("foto", helpers.PaymentVerificationPending, now),
		}
		if status := helpers.CalculateDocumentStatus(docs); status != helpers.DocumentPending {
			t.Errorf("Expected DocumentPending ('pending'), got '%s'", status)
		}
		if helpers.IsDocumentLengkap(docs) {
			t.Errorf("Expected IsDocumentLengkap=false when pending")
		}
	})
}
