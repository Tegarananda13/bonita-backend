package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"bonita-backend/helpers"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// mockSyncFinishedPaket menyiapkan ekspektasi query untuk helpers.SyncFinishedPaket()
func mockSyncFinishedPaket(mock sqlmock.Sqlmock, shouldFail bool) {
	if shouldFail {
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1,"is_finished"=$2 WHERE is_finished = $3 AND tanggal_berangkat + (durasi * INTERVAL '1 day') < $4`)).
			WithArgs(false, true, false, sqlmock.AnyArg()).
			WillReturnError(errors.New("simulated error sync finished paket"))
		mock.ExpectRollback()
		return
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1,"is_finished"=$2 WHERE is_finished = $3 AND tanggal_berangkat + (durasi * INTERVAL '1 day') < $4`)).
		WithArgs(false, true, false, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1 WHERE is_finished = $2 AND is_active = $3`)).
		WithArgs(false, true, true).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()
}

// ── 1. Unit Tests untuk ProcessExpiry ─────────────────────────────────────────

func TestProcessExpiry_NoExpired(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	// Tidak ada pendaftaran proses yang melewati batas waktu DP
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))

	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestProcessExpiry_ExpiredWithoutDP(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	paketID := uuid.New()
	nomorP := "UMR-EXP-001"
	nomorInv := "INV-EXP-001"
	batasWaktu := time.Now().Add(-2 * time.Hour)

	// 1. Query pendaftaran yang expired
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "paket_id", "status", "batas_waktu_dp"}).
			AddRow(nomorP, nomorInv, paketID, helpers.StatusProses, batasWaktu))

	// 2. Cek DP -> 0 (belum ada DP diterima)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
		WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// 3. Update status pendaftaran dalam invoice -> kadaluarsa
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_invoice = $2 AND status = $3`)).
		WithArgs(helpers.StatusKadaluarsa, nomorInv, helpers.StatusProses).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// 4. Kembalikan kuota paket
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=GREATEST(kuota_terpakai - $1, 0) WHERE id = $2`)).
		WithArgs(1, paketID.String()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestProcessExpiry_ExpiredWithDP_NotExpired(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	paketID := uuid.New()
	nomorP := "UMR-EXP-002"
	nomorInv := "INV-EXP-002"
	batasWaktu := time.Now().Add(-1 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "paket_id", "status", "batas_waktu_dp"}).
			AddRow(nomorP, nomorInv, paketID, helpers.StatusProses, batasWaktu))

	// Cek DP -> 1 (sudah ada DP diterima)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
		WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// Karena hasDP == true, pendaftaran TIDAK diupdate dan kuota TIDAK dikembalikan
	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestProcessExpiry_NoInvoice_DirectlyExpired(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	nomorP := "UMR-EXP-NO-INV"
	batasWaktu := time.Now().Add(-5 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "status", "batas_waktu_dp"}).
			AddRow(nomorP, "", helpers.StatusProses, batasWaktu))

	// Pendaftaran tanpa nomor invoice langsung dikadaluarsakan secara individual
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_pendaftaran = $2`)).
		WithArgs(helpers.StatusKadaluarsa, nomorP).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestProcessExpiry_GroupInvoice_MultipleMembers(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	paketID := uuid.New()
	nomorInv := "INV-EXP-GROUP"
	batasWaktu := time.Now().Add(-3 * time.Hour)

	// 3 pendaftaran dalam 1 invoice grup yang sama
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "paket_id", "status", "batas_waktu_dp"}).
			AddRow("UMR-GRP-01", nomorInv, paketID, helpers.StatusProses, batasWaktu).
			AddRow("UMR-GRP-02", nomorInv, paketID, helpers.StatusProses, batasWaktu).
			AddRow("UMR-GRP-03", nomorInv, paketID, helpers.StatusProses, batasWaktu))

	// Cek DP hanya dipanggil 1 kali untuk invoice ini
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
		WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// Update seluruh pendaftaran dalam invoice grup
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_invoice = $2 AND status = $3`)).
		WithArgs(helpers.StatusKadaluarsa, nomorInv, helpers.StatusProses).
		WillReturnResult(sqlmock.NewResult(1, 3))
	mock.ExpectCommit()

	// Kuota dikembalikan sebesar 3 (sesuai jumlah anggota grup)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=GREATEST(kuota_terpakai - $1, 0) WHERE id = $2`)).
		WithArgs(3, paketID.String()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestProcessExpiry_DatabaseError_QueryPendaftaran(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnError(errors.New("db connection failure"))

	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestProcessExpiry_DatabaseError_UpdateInvoice(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	paketID := uuid.New()
	nomorP := "UMR-EXP-ERR-01"
	nomorInv := "INV-EXP-ERR-01"
	batasWaktu := time.Now().Add(-1 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "paket_id", "status", "batas_waktu_dp"}).
			AddRow(nomorP, nomorInv, paketID, helpers.StatusProses, batasWaktu))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
		WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	// Update pendaftaran gagal
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_invoice = $2 AND status = $3`)).
		WithArgs(helpers.StatusKadaluarsa, nomorInv, helpers.StatusProses).
		WillReturnError(errors.New("update deadlock error"))
	mock.ExpectRollback()

	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestProcessExpiry_DatabaseError_UpdateKuota(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	paketID := uuid.New()
	nomorP := "UMR-EXP-ERR-02"
	nomorInv := "INV-EXP-ERR-02"
	batasWaktu := time.Now().Add(-1 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran", "nomor_invoice", "paket_id", "status", "batas_waktu_dp"}).
			AddRow(nomorP, nomorInv, paketID, helpers.StatusProses, batasWaktu))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "pembayaran" WHERE nomor_invoice = $1 AND status = $2`)).
		WithArgs(nomorInv, helpers.PaymentVerificationDiterima).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "pendaftaran" SET "status"=$1 WHERE nomor_invoice = $2 AND status = $3`)).
		WithArgs(helpers.StatusKadaluarsa, nomorInv, helpers.StatusProses).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// Update kuota gagal
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=GREATEST(kuota_terpakai - $1, 0) WHERE id = $2`)).
		WithArgs(1, paketID.String()).
		WillReturnError(errors.New("quota update error"))
	mock.ExpectRollback()

	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestProcessExpiry_SyncFinishedPaketFailure_Continues(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	// Simulasi SyncFinishedPaket gagal, ProcessExpiry harus tetap melanjutkan query pendaftaran
	mockSyncFinishedPaket(mock, true)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))

	ProcessExpiry()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

// ── 2. Unit Tests untuk ManualProcessExpiry ───────────────────────────────────

func TestManualProcessExpiry_Success(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest(http.MethodPost, "/admin/pendaftaran/process-expiry", nil)
	c.Request = req

	ManualProcessExpiry(c)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if resp.Message != "Proses expiry berhasil dijalankan" {
		t.Errorf("Unexpected message: '%s'", resp.Message)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

// ── 3. Unit Tests untuk Scheduler dan Konstanta ───────────────────────────────

func TestStartExpiryScheduler(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mockSyncFinishedPaket(mock, false)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "pendaftaran" WHERE status = $1 AND batas_waktu_dp < $2 AND batas_waktu_dp != '0001-01-01 00:00:00+00'`)).
		WithArgs(helpers.StatusProses, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"nomor_pendaftaran"}))

	StartExpiryScheduler()

	// Beri jeda sangat singkat agar goroutine startup menyelesaikan pemanggilan ProcessExpiry pertama
	time.Sleep(30 * time.Millisecond)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestDPDeadlineHours_Constant(t *testing.T) {
	if DPDeadlineHours != 24 {
		t.Errorf("Expected DPDeadlineHours to be 24, got %d", DPDeadlineHours)
	}
}

