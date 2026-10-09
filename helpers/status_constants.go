package helpers

const (
	PaymentBelum = "belum"
	PaymentDP    = "dp"
	PaymentLunas = "lunas"

	PaymentVerificationPending  = "pending"
	PaymentVerificationDiterima = "diterima"
	PaymentVerificationDitolak  = "ditolak"

	DocumentBelum        = "belum"
	DocumentPending      = "pending"
	DocumentBelumLengkap = "belum_lengkap"
	DocumentRevisi       = "revisi"
	DocumentLengkap      = "lengkap"

	StatusProses                    = "proses"
	StatusMenungguDokumen           = "menunggu_dokumen"
	StatusMenungguPembayaran        = "menunggu_pembayaran"
	StatusMenungguVerifikasiManager = "menunggu_verifikasi_manager"
	StatusPerluPerbaikan            = "perlu_perbaikan"
	StatusSiapBerangkat             = "siap_berangkat"
	StatusSelesai                   = "selesai"
	StatusKadaluarsa                = "kadaluarsa"

	// Pengaduan
	PengaduanMenunggu = "menunggu"
	PengaduanDiproses = "diproses"
	PengaduanSelesai  = "selesai"

	// Perlengkapan tambahan jamaah — harga default per jamaah
	DefaultHargaPerlengkapan = 1_450_000
)
