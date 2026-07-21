package services

import (
	"fmt"
	"strings"

	"bonita-backend/config"
	"bonita-backend/models"
)

// ─────────────────────────────────────────────────────────────────────────────
// Base Prompt — aturan chatbot yang bersifat tetap
// ─────────────────────────────────────────────────────────────────────────────

const BasePrompt = `
Kamu adalah Bonita Assistant, customer service virtual resmi dari travel umroh Bonita.

Kamu memiliki tiga kemampuan utama:
1. Menjawab pertanyaan mengenai Bonita Umroh (paket, harga, jadwal, pendaftaran, dll).
2. Menjawab pertanyaan umum mengenai ibadah umroh.
3. Membantu customer membuat laporan pengaduan — namun proses pengaduan ditangani oleh sistem secara otomatis, bukan oleh kamu.

Aturan dalam menjawab:
1. Selalu gunakan Bahasa Indonesia yang sopan, ramah, dan mudah dipahami.
2. Berikan jawaban yang singkat, jelas, dan langsung ke inti — maksimal 2 sampai 5 kalimat.
3. Gunakan poin-poin hanya jika memang diperlukan.
4. Jangan memberikan salam atau perkenalan di setiap jawaban.
5. Jangan membuat jawaban seperti artikel panjang.
6. Bersikap seperti customer service yang ramah dan profesional.
7. Jangan menyebut bahwa kamu adalah Gemini, AI Google, atau model AI apapun.
8. Hanya perkenalkan diri sebagai Bonita Assistant jika pengguna bertanya "siapa kamu?" atau pertanyaan sejenis.
9. Jika pertanyaan tidak berhubungan dengan layanan Bonita (misalnya politik, olahraga, pemrograman, tugas sekolah), tolak dengan sopan.
10. Untuk informasi harga, jadwal, atau kuota yang mungkin berubah, gunakan data berikut sebagai referensi terbaru.
11. Jika customer ingin mengadu atau komplain, kamu cukup memberi tahu bahwa sistem akan memandu mereka. Jangan coba memproses pengaduan sendiri.

`

// ─────────────────────────────────────────────────────────────────────────────
// BuildPackageContext — ambil data paket dari database
// ─────────────────────────────────────────────────────────────────────────────

func BuildPackageContext() string {
	var paketList []models.PaketUmroh

	// Ambil maksimal 10 paket, urutkan berdasarkan tanggal keberangkatan terdekat
	config.DB.
		Order("tanggal_berangkat ASC").
		Limit(10).
		Find(&paketList)

	if len(paketList) == 0 {
		return "Saat ini belum ada paket umroh yang tersedia.\n"
	}

	var sb strings.Builder
	sb.WriteString("=== DAFTAR PAKET UMROH (Data Terkini) ===\n\n")

	for i, p := range paketList {
		sisaKuota := p.KuotaMax - p.KuotaTerpakai
		if sisaKuota < 0 {
			sisaKuota = 0
		}

		sb.WriteString(fmt.Sprintf(
			"Paket %d:\n"+
				"  Nama        : %s\n"+
				"  Jenis       : %s\n"+
				"  Harga       : Rp%s\n"+
				"  Durasi      : %d hari\n"+
				"  Berangkat   : %s\n"+
				"  Kuota Maks  : %d\n"+
				"  Terpakai    : %d\n"+
				"  Sisa Kuota  : %d\n\n",
			i+1,
			p.NamaPaket,
			jenisLabel(p.JenisPaket),
			formatRupiah(p.Harga),
			p.Durasi,
			p.TanggalBerangkat.Format("02 January 2006"),
			p.KuotaMax,
			p.KuotaTerpakai,
			sisaKuota,
		))
	}

	return sb.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// BuildBusinessFlowContext — ringkasan alur bisnis Bonita
// ─────────────────────────────────────────────────────────────────────────────

func BuildBusinessFlowContext() string {
	return `=== ALUR PROSES PENDAFTARAN BONITA ===

1. Customer memilih paket umroh di halaman Paket Umroh.
2. Customer melakukan pendaftaran melalui sistem Bonita.
3. Sistem membuat Nomor Pendaftaran dengan format UMR-XXXXX.
4. Customer melakukan verifikasi menggunakan kode OTP yang dikirim ke email.
5. Customer melakukan pembayaran DP pertama minimal Rp5.000.000 dan mengunggah bukti transfer.
6. Admin memverifikasi pembayaran.
7. Setelah pembayaran DP diterima, customer dapat mengunggah dokumen persyaratan.
8. Admin memverifikasi dokumen yang diunggah.
9. Jika pembayaran telah lunas dan seluruh dokumen telah diverifikasi, status jamaah berubah menjadi "Siap Berangkat".
10. Customer dapat memantau seluruh status melalui Portal Jamaah.

`
}

// ─────────────────────────────────────────────────────────────────────────────
// BuildFAQContext — FAQ penting seputar layanan Bonita
// ─────────────────────────────────────────────────────────────────────────────

func BuildFAQContext() string {
	return `=== FAQ BONITA ===

T: Berapa DP minimal untuk pendaftaran?
J: DP pertama minimal Rp5.000.000.

T: Apakah pembayaran berikutnya ada batas jumlahnya?
J: Pembayaran berikutnya bebas, selama total tidak melebihi sisa tagihan paket.

T: Apakah bukti pembayaran wajib diupload?
J: Ya, bukti transfer wajib diunggah setiap kali melakukan pembayaran.

T: Kapan saya bisa mengunggah dokumen?
J: Dokumen baru dapat diunggah setelah pembayaran DP pertama diterima dan diverifikasi oleh admin.

T: Dokumen apa saja yang wajib diunggah?
J: Paspor, KTP, Akte Kelahiran, Kartu Keluarga, dan dokumen Vaksin. Foto bersifat opsional.

T: Apa itu status "Siap Berangkat"?
J: Status ini muncul setelah pembayaran lunas dan seluruh dokumen telah diverifikasi oleh admin Bonita.

T: Bagaimana cara memantau status pendaftaran?
J: Customer dapat memantau status, pembayaran, dan dokumen melalui Portal Jamaah.

T: Berapa lama proses verifikasi pembayaran?
J: Proses verifikasi dilakukan oleh admin Bonita. Biasanya berlangsung dalam 1x24 jam kerja.

`
}

// ─────────────────────────────────────────────────────────────────────────────
// BuildBonitaContext — gabungkan seluruh context menjadi satu string
// Fungsi ini adalah titik integrasi utama.
// Di masa depan, dapat diganti dengan mekanisme RAG tanpa mengubah AskGemini().
// ─────────────────────────────────────────────────────────────────────────────

func BuildBonitaContext() string {
	var sb strings.Builder

	sb.WriteString("\n\n=== KONTEKS INFORMASI BONITA ===\n\n")
	sb.WriteString(BuildPackageContext())
	sb.WriteString(BuildBusinessFlowContext())
	sb.WriteString(BuildFAQContext())
	sb.WriteString("=== AKHIR KONTEKS ===\n\n")

	return sb.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers internal
// ─────────────────────────────────────────────────────────────────────────────

func formatRupiah(n float64) string {
	// Format angka ke format Rupiah tanpa library eksternal
	// Contoh: 35000000 → "35.000.000"
	s := fmt.Sprintf("%.0f", n)
	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += "."
		}
		result += string(c)
	}
	return result
}

func jenisLabel(j string) string {
	if j == "" {
		return "Umum"
	}
	return j
}
