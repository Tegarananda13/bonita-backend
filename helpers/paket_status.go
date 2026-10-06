package helpers

import (
	"sort"
	"time"

	"bonita-backend/config"
	"bonita-backend/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PaketTersediaScope membatasi query ke paket yang masih bisa didaftarkan:
// aktif, belum selesai, kuota masih tersisa, dan tanggal berangkat belum lewat.
func PaketTersediaScope(db *gorm.DB, now time.Time) *gorm.DB {
	return db.Where(
		"is_active = ? AND is_finished = ? AND kuota_terpakai < kuota_max AND tanggal_berangkat > ?",
		true, false, now,
	)
}

// PaketKuotaTersedia = KuotaMax - KuotaTerpakai (dihitung, tidak disimpan).
func PaketKuotaTersedia(p models.PaketUmroh) int {
	if s := p.KuotaMax - p.KuotaTerpakai; s > 0 {
		return s
	}
	return 0
}

// ReservasiKuotaPaket menambah kuota_terpakai sebanyak n secara atomik (satu UPDATE
// bersyarat) hanya jika paket masih tersedia. Return false jika tidak tersedia/penuh.
// Gunakan tx yang sama dengan pembuatan pendaftaran agar bisa di-rollback.
func ReservasiKuotaPaket(tx *gorm.DB, paketID uuid.UUID, n int, now time.Time) (bool, error) {
	res := tx.Model(&models.PaketUmroh{}).
		Where("id = ? AND is_active = ? AND is_finished = ? AND kuota_terpakai + ? <= kuota_max AND tanggal_berangkat > ?",
			paketID, true, false, n, now).
		UpdateColumn("kuota_terpakai", gorm.Expr("kuota_terpakai + ?", n))
	return res.RowsAffected > 0, res.Error
}

// Label status perjalanan paket (dihitung, tidak disimpan di database).
const (
	PerjalananBerjalan       = "Berjalan"
	PerjalananBelumBerangkat = "Belum Berangkat"
	PerjalananSelesai        = "Selesai"
)

// PaketTanggalSelesai = TanggalBerangkat + Durasi (hari).
func PaketTanggalSelesai(p models.PaketUmroh) time.Time {
	return p.TanggalBerangkat.AddDate(0, 0, p.Durasi)
}

// PaketTelahLewat true jika perkiraan tanggal selesai perjalanan sudah terlewati.
func PaketTelahLewat(p models.PaketUmroh, now time.Time) bool {
	return now.After(PaketTanggalSelesai(p))
}

// PaketStatusPerjalanan menentukan status perjalanan:
//   - IsFinished (manual) atau TanggalBerangkat+Durasi terlewati → Selesai
//   - belum mencapai TanggalBerangkat                            → Belum Berangkat
//   - selain itu                                                 → Berjalan
func PaketStatusPerjalanan(p models.PaketUmroh, now time.Time) string {
	if p.IsFinished || PaketTelahLewat(p, now) {
		return PerjalananSelesai
	}
	if now.Before(p.TanggalBerangkat) {
		return PerjalananBelumBerangkat
	}
	return PerjalananBerjalan
}

// SyncFinishedPaket menerapkan business rule secara massal di database:
//  1. Paket yang TanggalBerangkat + Durasi sudah lewat → is_finished = true, is_active = false
//  2. Paket is_finished = true tidak boleh is_active = true (perbaikan data lama)
//
// Idempotent dan murah; dipanggil dari scheduler dan sebelum membaca/mengubah paket.
func SyncFinishedPaket() error {
	if err := config.DB.Model(&models.PaketUmroh{}).
		Where("is_finished = ? AND tanggal_berangkat + (durasi * INTERVAL '1 day') < ?", false, time.Now()).
		Updates(map[string]interface{}{"is_finished": true, "is_active": false}).Error; err != nil {
		return err
	}
	return config.DB.Model(&models.PaketUmroh{}).
		Where("is_finished = ? AND is_active = ?", true, true).
		Update("is_active", false).Error
}

// SortPaketByPerjalanan mengurutkan paket: Berjalan → Belum Berangkat → Selesai.
// Berjalan & Belum Berangkat: TanggalBerangkat terdekat dulu.
// Selesai: yang paling baru selesai dulu.
func SortPaketByPerjalanan(list []models.PaketUmroh, now time.Time) {
	rank := map[string]int{
		PerjalananBerjalan:       0,
		PerjalananBelumBerangkat: 1,
		PerjalananSelesai:        2,
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		sa, sb := PaketStatusPerjalanan(a, now), PaketStatusPerjalanan(b, now)
		if sa != sb {
			return rank[sa] < rank[sb]
		}
		if sa == PerjalananSelesai {
			return PaketTanggalSelesai(a).After(PaketTanggalSelesai(b))
		}
		return a.TanggalBerangkat.Before(b.TanggalBerangkat)
	})
}
