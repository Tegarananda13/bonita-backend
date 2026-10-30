package helpers_test

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func TestPaketTersediaScope(t *testing.T) {
	gdb, mock, cleanup := newMockGormDB(t)
	defer cleanup()

	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT * FROM "paket_umroh" WHERE is_active = $1 AND is_finished = $2 AND kuota_terpakai < kuota_max AND tanggal_berangkat > $3`,
	)).
		WithArgs(true, false, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "nama_paket"}))

	var list []models.PaketUmroh
	if err := helpers.PaketTersediaScope(gdb, now).Find(&list).Error; err != nil {
		t.Fatalf("Unexpected error executing PaketTersediaScope: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unmet sqlmock expectations: %v", err)
	}
}

func TestPaketKuotaTersedia(t *testing.T) {
	tests := []struct {
		name          string
		kuotaMax      int
		kuotaTerpakai int
		want          int
	}{
		{"HasAvailableQuota", 45, 15, 30},
		{"SingleSeatRemaining", 30, 29, 1},
		{"ExactlyFull", 45, 45, 0},
		{"OverbookedClampedToZero", 45, 50, 0},
		{"ZeroMaxQuota", 0, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := models.PaketUmroh{
				KuotaMax:      tc.kuotaMax,
				KuotaTerpakai: tc.kuotaTerpakai,
			}
			if got := helpers.PaketKuotaTersedia(p); got != tc.want {
				t.Errorf("PaketKuotaTersedia(%+v) = %d, want %d", p, got, tc.want)
			}
		})
	}
}

func TestReservasiKuotaPaket(t *testing.T) {
	now := time.Date(2026, time.May, 1, 10, 0, 0, 0, time.UTC)
	paketID := uuid.New()

	t.Run("Success_SeatReserved", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(
			`UPDATE "paket_umroh" SET "kuota_terpakai"=kuota_terpakai + $1 WHERE id = $2 AND is_active = $3 AND is_finished = $4 AND kuota_terpakai + $5 <= kuota_max AND tanggal_berangkat > $6`,
		)).
			WithArgs(2, paketID, true, false, 2, now).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		ok, err := helpers.ReservasiKuotaPaket(gdb, paketID, 2, now)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !ok {
			t.Error("Expected ok=true when RowsAffected == 1")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet sqlmock expectations: %v", err)
		}
	})

	t.Run("QuotaFullOrUnavailable_ReturnsFalseNil", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=kuota_terpakai + $1`)).
			WithArgs(5, paketID, true, false, 5, now).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		ok, err := helpers.ReservasiKuotaPaket(gdb, paketID, 5, now)
		if err != nil {
			t.Fatalf("Expected nil error when 0 rows affected, got %v", err)
		}
		if ok {
			t.Error("Expected ok=false when RowsAffected == 0")
		}
	})

	t.Run("DatabaseError_ReturnsFalseAndError", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "kuota_terpakai"=kuota_terpakai + $1`)).
			WillReturnError(errors.New("db deadlock"))
		mock.ExpectRollback()

		ok, err := helpers.ReservasiKuotaPaket(gdb, paketID, 1, now)
		if err == nil {
			t.Fatal("Expected error when DB update fails")
		}
		if ok {
			t.Error("Expected ok=false on DB error")
		}
	})
}

func TestPaketTanggalSelesai_TelahLewat_StatusPerjalanan(t *testing.T) {
	dep := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	paket := models.PaketUmroh{
		TanggalBerangkat: dep,
		Durasi:           9, // selesai: 2026-06-19
		IsFinished:       false,
	}

	expectedEnd := time.Date(2026, time.June, 19, 0, 0, 0, 0, time.UTC)
	if gotEnd := helpers.PaketTanggalSelesai(paket); !gotEnd.Equal(expectedEnd) {
		t.Errorf("PaketTanggalSelesai() = %v, want %v", gotEnd, expectedEnd)
	}

	tests := []struct {
		name       string
		paket      models.PaketUmroh
		now        time.Time
		wantLewat  bool
		wantStatus string
	}{
		{
			name:       "BeforeDeparture_BelumBerangkat",
			paket:      paket,
			now:        dep.Add(-24 * time.Hour),
			wantLewat:  false,
			wantStatus: helpers.PerjalananBelumBerangkat,
		},
		{
			name:       "ExactDepartureMoment_Berjalan",
			paket:      paket,
			now:        dep,
			wantLewat:  false,
			wantStatus: helpers.PerjalananBerjalan,
		},
		{
			name:       "MidTrip_Berjalan",
			paket:      paket,
			now:        dep.AddDate(0, 0, 5),
			wantLewat:  false,
			wantStatus: helpers.PerjalananBerjalan,
		},
		{
			name:       "ExactEndMoment_NotAfterSoStillBerjalan",
			paket:      paket,
			now:        expectedEnd,
			wantLewat:  false,
			wantStatus: helpers.PerjalananBerjalan,
		},
		{
			name:       "AfterEndMoment_Selesai",
			paket:      paket,
			now:        expectedEnd.Add(time.Second),
			wantLewat:  true,
			wantStatus: helpers.PerjalananSelesai,
		},
		{
			name: "ManualIsFinishedTrue_SelesaiEvenBeforeDeparture",
			paket: models.PaketUmroh{
				TanggalBerangkat: dep.AddDate(0, 1, 0),
				Durasi:           12,
				IsFinished:       true,
			},
			now:        dep,
			wantLewat:  false,
			wantStatus: helpers.PerjalananSelesai,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if gotLewat := helpers.PaketTelahLewat(tc.paket, tc.now); gotLewat != tc.wantLewat {
				t.Errorf("PaketTelahLewat() = %v, want %v", gotLewat, tc.wantLewat)
			}
			if gotStatus := helpers.PaketStatusPerjalanan(tc.paket, tc.now); gotStatus != tc.wantStatus {
				t.Errorf("PaketStatusPerjalanan() = %q, want %q", gotStatus, tc.wantStatus)
			}
		})
	}
}

func TestSyncFinishedPaket(t *testing.T) {
	t.Run("Success_BothUpdatesSucceed", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(
			`UPDATE "paket_umroh" SET "is_active"=$1,"is_finished"=$2 WHERE is_finished = $3 AND tanggal_berangkat + (durasi * INTERVAL '1 day') < $4`,
		)).
			WithArgs(false, true, false, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 2))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(
			`UPDATE "paket_umroh" SET "is_active"=$1 WHERE is_finished = $2 AND is_active = $3`,
		)).
			WithArgs(false, true, true).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		if err := helpers.SyncFinishedPaket(); err != nil {
			t.Fatalf("Expected nil error, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("Unmet sqlmock expectations: %v", err)
		}
	})

	t.Run("Error_FirstUpdateFails", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET`)).
			WillReturnError(errors.New("first update error"))
		mock.ExpectRollback()

		if err := helpers.SyncFinishedPaket(); err == nil {
			t.Fatal("Expected error when first update fails, got nil")
		}
	})

	t.Run("Error_SecondUpdateFails", func(t *testing.T) {
		gdb, mock, cleanup := newMockGormDB(t)
		defer cleanup()

		prevDB := config.DB
		config.DB = gdb
		defer func() { config.DB = prevDB }()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET`)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "paket_umroh" SET "is_active"=$1 WHERE is_finished = $2 AND is_active = $3`)).
			WillReturnError(errors.New("second update error"))
		mock.ExpectRollback()

		if err := helpers.SyncFinishedPaket(); err == nil {
			t.Fatal("Expected error when second update fails, got nil")
		}
	})
}

func TestSortPaketByPerjalanan(t *testing.T) {
	now := time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC)

	list := []models.PaketUmroh{
		{
			NamaPaket:        "Selesai Lama",
			TanggalBerangkat: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			Durasi:           9, // selesai 10 April
		},
		{
			NamaPaket:        "Belum Berangkat Jauh",
			TanggalBerangkat: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
			Durasi:           9,
		},
		{
			NamaPaket:        "Berjalan Lebih Akhir",
			TanggalBerangkat: time.Date(2026, time.June, 12, 0, 0, 0, 0, time.UTC),
			Durasi:           10,
		},
		{
			NamaPaket:        "Selesai Baru",
			TanggalBerangkat: time.Date(2026, time.May, 20, 0, 0, 0, 0, time.UTC),
			Durasi:           9, // selesai 29 Mei
		},
		{
			NamaPaket:        "Berjalan Lebih Awal",
			TanggalBerangkat: time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC),
			Durasi:           10,
		},
		{
			NamaPaket:        "Belum Berangkat Dekat",
			TanggalBerangkat: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			Durasi:           9,
		},
	}

	helpers.SortPaketByPerjalanan(list, now)

	expectedOrder := []string{
		"Berjalan Lebih Awal",
		"Berjalan Lebih Akhir",
		"Belum Berangkat Dekat",
		"Belum Berangkat Jauh",
		"Selesai Baru",
		"Selesai Lama",
	}

	for i, wantName := range expectedOrder {
		if list[i].NamaPaket != wantName {
			t.Errorf("Position %d: got %q, want %q", i, list[i].NamaPaket, wantName)
		}
	}
}
