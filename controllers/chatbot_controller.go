package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"bonita-backend/config"
	"bonita-backend/helpers"
	"bonita-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ── Intent Keywords untuk deteksi pengaduan ───────────────────────────────────

var intentPengaduan = []string{
	"saya ingin mengadu",
	"ingin mengadu",
	"saya ingin komplain",
	"ingin komplain",
	"saya ingin membuat laporan",
	"ingin membuat laporan",
	"saya ada keluhan",
	"ada keluhan",
	"saya ingin melapor",
	"ingin melapor",
	"saya mau mengadu",
	"mau mengadu",
	"saya mau komplain",
	"mau komplain",
	"saya mau melapor",
	"mau melapor",
	"membuat pengaduan",
	"saya ingin pengaduan",
	"buat pengaduan",
	"laporan pengaduan",
	"ingin pengaduan",
}

var kategoriValid = []string{
	"Pembayaran", "Dokumen", "Jadwal", "Hotel", "Transportasi", "Lainnya",
}

// detectIntentPengaduan cek apakah pesan mengandung intent pengaduan
func detectIntentPengaduan(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	for _, kw := range intentPengaduan {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// buildJudul — ambil 10 kata pertama dari isi pengaduan sebagai judul
func buildJudul(isi string) string {
	words := strings.FieldsFunc(isi, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	if len(words) > 10 {
		words = words[:10]
	}
	judul := strings.Join(words, " ")
	if len(isi) > len(judul) {
		judul += "..."
	}
	return judul
}

// ── ChatbotRequest ─────────────────────────────────────────────────────────────

// RegData menyimpan state sementara pendaftaran melalui chatbot
type RegData struct {
	PaketID       string `json:"paket_id"`
	PaketNama     string `json:"paket_nama"`
	NIK           string `json:"nik"`
	Nama          string `json:"nama"`
	TempatLahir   string `json:"tempat_lahir"`
	TanggalLahir  string `json:"tanggal_lahir"`
	JenisKelamin  string `json:"jenis_kelamin"`
	NoHP          string `json:"no_hp"`
	Email         string `json:"email"`
	AlamatLengkap string `json:"alamat_lengkap"`
	Provinsi      string `json:"provinsi"`
	KabupatenKota string `json:"kabupaten_kota"`
	Kecamatan     string `json:"kecamatan"`
	KelurahanDesa string `json:"kelurahan_desa"`
	KodePos       string `json:"kode_pos"`
}

type ChatbotRequest struct {
	Pertanyaan    string  `json:"pertanyaan" binding:"required"`
	Flow          string  `json:"flow"`           // "" | "pengaduan" | "registrasi"
	Step          string  `json:"step"`           // berbeda per flow
	NomorUMR      string  `json:"nomor_umr"`
	PendaftaranID string  `json:"pendaftaran_id"`
	Kategori      string  `json:"kategori"`
	RegData       *RegData `json:"reg_data"`      // state pendaftaran chatbot
}

// ── Chatbot ────────────────────────────────────────────────────────────────────

func Chatbot(c *gin.Context) {

	var req ChatbotRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Pertanyaan wajib diisi"})
		return
	}

	// ── FLOW PENGADUAN ────────────────────────────────────────────────────────

	if req.Flow == "pengaduan" {

		switch req.Step {

		// ── Step 1: customer mengirim nomor UMR ──
		case "ask_nomor":
			nomorInput := strings.TrimSpace(req.Pertanyaan)

			var pendaftaran models.Pendaftaran
			err := config.DB.
				Preload("Customer").
				Where("nomor_pendaftaran = ?", nomorInput).
				First(&pendaftaran).Error

			if err != nil {
				chatbotResponse(c, req.Pertanyaan, "pengaduan",
					"Nomor UMR tidak ditemukan. Silakan periksa kembali dan coba lagi dengan format: **UMR-YYYYMMDDHHMMSS**",
					"ask_nomor", "")
				return
			}

			chatbotResponse(c, req.Pertanyaan, "pengaduan",
				"Baik, Nomor UMR **"+nomorInput+"** atas nama **"+pendaftaran.Customer.Nama+"** ditemukan.\n\n"+
					"Silakan pilih **kategori pengaduan** Anda:",
				"ask_kategori", pendaftaran.ID.String())
			return

		// ── Step 2: customer memilih kategori ──
		case "ask_kategori":
			kategori := strings.TrimSpace(req.Pertanyaan)

			// validasi kategori
			valid := false
			for _, k := range kategoriValid {
				if strings.EqualFold(k, kategori) {
					kategori = k // normalisasi kapitalisasi
					valid = true
					break
				}
			}
			if !valid {
				chatbotResponse(c, req.Pertanyaan, "pengaduan",
					"Kategori tidak valid. Silakan pilih salah satu kategori yang tersedia.",
					"ask_kategori", req.PendaftaranID)
				return
			}

			chatbotResponse(c, req.Pertanyaan, "pengaduan",
				"Kategori **"+kategori+"** dipilih.\n\nSilakan jelaskan keluhan Anda secara singkat:",
				"ask_isi_"+kategori, req.PendaftaranID)
			return

		// ── Step 3: customer mengirim isi pengaduan ──
		default:
			// step berformat "ask_isi_<kategori>"
			if strings.HasPrefix(req.Step, "ask_isi") {
				kategori := req.Kategori

				// fallback: ambil dari step jika tidak ada di request
				if kategori == "" && strings.Contains(req.Step, "_") {
					parts := strings.SplitN(req.Step, "ask_isi_", 2)
					if len(parts) == 2 {
						kategori = parts[1]
					}
				}
				if kategori == "" {
					kategori = "Lainnya"
				}

				isi := strings.TrimSpace(req.Pertanyaan)
				if isi == "" {
					chatbotResponse(c, req.Pertanyaan, "pengaduan",
						"Isi pengaduan tidak boleh kosong. Silakan jelaskan keluhan Anda.",
						req.Step, req.PendaftaranID)
					return
				}

				// parse pendaftaran ID
				pendaftaranID, err := uuid.Parse(req.PendaftaranID)
				if err != nil {
					chatbotResponse(c, req.Pertanyaan, "pengaduan",
						"Terjadi kesalahan pada sesi Anda. Silakan mulai ulang proses pengaduan.",
						"error", "")
					return
				}

				// simpan pengaduan ke database
				pengaduan := models.Pengaduan{
					PendaftaranID: pendaftaranID,
					Judul:         buildJudul(isi),
					IsiPengaduan:  isi,
					Kategori:      kategori,
					Status:        helpers.PengaduanMenunggu,
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
				}

				if err := config.DB.Create(&pengaduan).Error; err != nil {
					chatbotResponse(c, req.Pertanyaan, "pengaduan",
						"Maaf, terjadi kesalahan saat menyimpan pengaduan. Silakan coba lagi.",
						req.Step, req.PendaftaranID)
					return
				}

				// log chatbot
				saveChatLog(req.Pertanyaan, "Pengaduan berhasil dikirim. ID: "+pengaduan.ID.String())

				chatbotResponse(c, req.Pertanyaan, "pengaduan",
					"✅ **Terima kasih!** Laporan Anda berhasil dikirim.\n\n"+
						"Admin Bonita akan segera menindaklanjuti pengaduan Anda.\n\n"+
						"Ada yang bisa kami bantu lagi?",
					"done", "")
				return
			}
		}

		chatbotResponse(c, req.Pertanyaan, "pengaduan",
			"Sesi pengaduan tidak dikenali. Silakan mulai ulang.", "error", "")
		return
	}

	// ── FLOW REGISTRASI ───────────────────────────────────────────────────────

	if req.Flow == "registrasi" {
		handleRegistrasiFlow(c, req)
		return
	}

	// ── DETEKSI INTENT REGISTRASI ─────────────────────────────────────────────

	if detectIntentRegistrasi(req.Pertanyaan) {
		// Ambil daftar paket aktif
		var pakets []models.PaketUmroh
		config.DB.Where("is_active = true AND is_finished = false").Order("tanggal_berangkat ASC").Find(&pakets)

		if len(pakets) == 0 {
			chatbotResponse(c, req.Pertanyaan, "",
				"Maaf, saat ini belum ada paket umroh yang tersedia. Silakan cek kembali nanti.", "", "")
			return
		}

		paketList := "Berikut paket umroh yang tersedia:\n\n"
		for i, p := range pakets {
			paketList += fmt.Sprintf("%d. **%s**\n   💰 %s\n   📅 Berangkat: %s\n\n",
				i+1, p.NamaPaket,
				formatRupiah(p.Harga),
				p.TanggalBerangkat.Format("02 Jan 2006"))
		}
		paketList += "Ketik **nama paket** yang ingin Anda pilih:"

		chatbotResponse(c, req.Pertanyaan, "registrasi", paketList, "pilih_paket", "")
		return
	}

	// ── DETEKSI INTENT PENGADUAN ─────────────────────────────────────────────

	if detectIntentPengaduan(req.Pertanyaan) {
		chatbotResponse(c, req.Pertanyaan, "pengaduan",
			"Baik, saya akan membantu membuat laporan pengaduan.\n\nSilakan masukkan **Nomor UMR** Anda terlebih dahulu.\n\nContoh: **UMR-20260718123456**",
			"ask_nomor", "")
		return
	}

	// ── PERTANYAAN NORMAL → GEMINI ────────────────────────────────────────────

	jawaban, err := helpers.AskGemini(req.Pertanyaan)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Gagal mendapatkan jawaban dari Bonita Assistant",
		})
		return
	}

	saveChatLog(req.Pertanyaan, jawaban)

	c.JSON(http.StatusOK, gin.H{
		"message": "Berhasil mendapatkan jawaban",
		"data": gin.H{
			"pertanyaan": req.Pertanyaan,
			"jawaban":    jawaban,
			"flow":       "",
			"step":       "",
		},
	})
}

// ── Helpers internal ─────────────────────────────────────────────────────────

// chatbotResponse — mengirimkan response chatbot dengan state flow
func chatbotResponse(c *gin.Context, pertanyaan, flow, jawaban, nextStep, pendaftaranID string) {
	saveChatLog(pertanyaan, jawaban)

	c.JSON(http.StatusOK, gin.H{
		"message": "Berhasil mendapatkan jawaban",
		"data": gin.H{
			"pertanyaan":     pertanyaan,
			"jawaban":        jawaban,
			"flow":           flow,
			"step":           nextStep,
			"pendaftaran_id": pendaftaranID,
		},
	})
}

// chatbotResponseReg — response dengan reg_data untuk flow registrasi
func chatbotResponseReg(c *gin.Context, pertanyaan, jawaban, nextStep string, regData *RegData) {
	saveChatLog(pertanyaan, jawaban)
	c.JSON(http.StatusOK, gin.H{
		"message": "Berhasil mendapatkan jawaban",
		"data": gin.H{
			"pertanyaan":     pertanyaan,
			"jawaban":        jawaban,
			"flow":           "registrasi",
			"step":           nextStep,
			"pendaftaran_id": "",
			"reg_data":       regData,
		},
	})
}

// detectIntentRegistrasi cek apakah pesan mengandung intent mendaftar umroh
func detectIntentRegistrasi(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	keywords := []string{
		"daftar umroh", "ingin daftar", "mau daftar", "mendaftar umroh",
		"daftar jamaah", "pendaftaran umroh", "ingin mendaftar",
		"mau mendaftar", "saya ingin daftar", "saya mau daftar",
		"registrasi umroh", "ingin registrasi",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// formatRupiah sudah dideklarasikan di invoice_controller.go dalam package yang sama

// handleRegistrasiFlow menangani seluruh step flow pendaftaran via chatbot
func handleRegistrasiFlow(c *gin.Context, req ChatbotRequest) {
	rd := req.RegData
	if rd == nil {
		rd = &RegData{}
	}
	input := strings.TrimSpace(req.Pertanyaan)

	switch req.Step {

	case "pilih_paket":
		// Cari paket berdasarkan nama (case-insensitive)
		var pakets []models.PaketUmroh
		config.DB.Where("is_active = true AND is_finished = false").Find(&pakets)

		var chosen *models.PaketUmroh
		for i, p := range pakets {
			if strings.EqualFold(strings.TrimSpace(p.NamaPaket), input) ||
				strings.Contains(strings.ToLower(p.NamaPaket), strings.ToLower(input)) {
				chosen = &pakets[i]
				break
			}
		}
		if chosen == nil {
			chatbotResponseReg(c, req.Pertanyaan,
				"Paket tidak ditemukan. Silakan ketik nama paket yang tersedia dengan benar.",
				"pilih_paket", rd)
			return
		}
		rd.PaketID = chosen.ID.String()
		rd.PaketNama = chosen.NamaPaket
		chatbotResponseReg(c, req.Pertanyaan,
			fmt.Sprintf("Paket **%s** dipilih ✅\n\nSekarang saya akan meminta data jamaah satu per satu.\n\nMohon masukkan **NIK** (16 digit):",
				chosen.NamaPaket),
			"ask_nik", rd)

	case "ask_nik":
		// Validasi NIK
		nik := strings.ReplaceAll(input, " ", "")
		if len(nik) != 16 {
			chatbotResponseReg(c, req.Pertanyaan,
				"NIK harus tepat 16 digit angka. Silakan coba lagi:", "ask_nik", rd)
			return
		}
		for _, ch := range nik {
			if ch < '0' || ch > '9' {
				chatbotResponseReg(c, req.Pertanyaan,
					"NIK hanya boleh berisi angka. Silakan coba lagi:", "ask_nik", rd)
				return
			}
		}
		var existing models.Customer
		if config.DB.Where("nik = ?", nik).First(&existing).Error == nil {
			chatbotResponseReg(c, req.Pertanyaan,
				"NIK tersebut sudah terdaftar dalam sistem. Silakan hubungi admin jika ada masalah.",
				"ask_nik", rd)
			return
		}
		rd.NIK = nik
		chatbotResponseReg(c, req.Pertanyaan,
			"NIK diterima ✅\n\nMohon masukkan **Nama Lengkap**:", "ask_nama", rd)

	case "ask_nama":
		rd.Nama = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Nama: **"+rd.Nama+"** ✅\n\nMohon masukkan **Tempat Lahir**:", "ask_tempat_lahir", rd)

	case "ask_tempat_lahir":
		rd.TempatLahir = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Tempat Lahir: **"+rd.TempatLahir+"** ✅\n\nMohon masukkan **Tanggal Lahir** (format: YYYY-MM-DD):",
			"ask_tanggal_lahir", rd)

	case "ask_tanggal_lahir":
		_, err := time.Parse("2006-01-02", input)
		if err != nil {
			chatbotResponseReg(c, req.Pertanyaan,
				"Format tanggal tidak valid. Gunakan format YYYY-MM-DD, contoh: 1990-05-20",
				"ask_tanggal_lahir", rd)
			return
		}
		rd.TanggalLahir = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Tanggal Lahir: **"+rd.TanggalLahir+"** ✅\n\nMohon masukkan **Jenis Kelamin** (Laki-laki/Perempuan):",
			"ask_jenis_kelamin", rd)

	case "ask_jenis_kelamin":
		lower := strings.ToLower(input)
		if !strings.Contains(lower, "laki") && !strings.Contains(lower, "perempuan") {
			chatbotResponseReg(c, req.Pertanyaan,
				"Jenis kelamin tidak valid. Ketik **Laki-laki** atau **Perempuan**:",
				"ask_jenis_kelamin", rd)
			return
		}
		if strings.Contains(lower, "laki") {
			rd.JenisKelamin = "Laki-laki"
		} else {
			rd.JenisKelamin = "Perempuan"
		}
		chatbotResponseReg(c, req.Pertanyaan,
			"Jenis Kelamin: **"+rd.JenisKelamin+"** ✅\n\nMohon masukkan **Nomor HP** (aktif):",
			"ask_no_hp", rd)

	case "ask_no_hp":
		rd.NoHP = input
		chatbotResponseReg(c, req.Pertanyaan,
			"No. HP: **"+rd.NoHP+"** ✅\n\nMohon masukkan **Email**:", "ask_email", rd)

	case "ask_email":
		rd.Email = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Email: **"+rd.Email+"** ✅\n\nMohon masukkan **Alamat Lengkap**:", "ask_alamat", rd)

	case "ask_alamat":
		rd.AlamatLengkap = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Alamat: **"+rd.AlamatLengkap+"** ✅\n\nMohon masukkan **Provinsi**:", "ask_provinsi", rd)

	case "ask_provinsi":
		rd.Provinsi = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Provinsi: **"+rd.Provinsi+"** ✅\n\nMohon masukkan **Kabupaten/Kota**:", "ask_kabupaten", rd)

	case "ask_kabupaten":
		rd.KabupatenKota = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Kab/Kota: **"+rd.KabupatenKota+"** ✅\n\nMohon masukkan **Kecamatan**:", "ask_kecamatan", rd)

	case "ask_kecamatan":
		rd.Kecamatan = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Kecamatan: **"+rd.Kecamatan+"** ✅\n\nMohon masukkan **Kelurahan/Desa**:", "ask_kelurahan", rd)

	case "ask_kelurahan":
		rd.KelurahanDesa = input
		chatbotResponseReg(c, req.Pertanyaan,
			"Kelurahan/Desa: **"+rd.KelurahanDesa+"** ✅\n\nMohon masukkan **Kode Pos**:", "ask_kode_pos", rd)

	case "ask_kode_pos":
		rd.KodePos = input

		// Tampilkan ringkasan untuk konfirmasi
		ringkasan := fmt.Sprintf(
			"📋 **Ringkasan Data Pendaftaran:**\n\n"+
				"Paket        : %s\n"+
				"NIK          : %s\n"+
				"Nama         : %s\n"+
				"Tempat Lahir : %s\n"+
				"Tanggal Lahir: %s\n"+
				"Jenis Kelamin: %s\n"+
				"No. HP       : %s\n"+
				"Email        : %s\n"+
				"Alamat       : %s\n"+
				"Provinsi     : %s\n"+
				"Kab/Kota     : %s\n"+
				"Kecamatan    : %s\n"+
				"Kelurahan    : %s\n"+
				"Kode Pos     : %s\n\n"+
				"Apakah data sudah benar? Ketik **Ya** untuk mendaftar atau **Tidak** untuk mengulang.",
			rd.PaketNama, rd.NIK, rd.Nama, rd.TempatLahir, rd.TanggalLahir,
			rd.JenisKelamin, rd.NoHP, rd.Email, rd.AlamatLengkap,
			rd.Provinsi, rd.KabupatenKota, rd.Kecamatan, rd.KelurahanDesa, rd.KodePos)
		chatbotResponseReg(c, req.Pertanyaan, ringkasan, "konfirmasi", rd)

	case "konfirmasi":
		if strings.ToLower(input) == "tidak" || strings.ToLower(input) == "ulang" {
			// Reset dan mulai dari paket
			chatbotResponseReg(c, req.Pertanyaan,
				"Baik, pendaftaran dibatalkan. Ketik **daftar umroh** untuk memulai kembali.",
				"batal", nil)
			return
		}
		if strings.ToLower(input) != "ya" {
			chatbotResponseReg(c, req.Pertanyaan,
				"Ketik **Ya** untuk konfirmasi atau **Tidak** untuk membatalkan.",
				"konfirmasi", rd)
			return
		}

		// ── Eksekusi pendaftaran ──────────────────────────────────────────────
		paketID, err := uuid.Parse(rd.PaketID)
		if err != nil {
			chatbotResponseReg(c, req.Pertanyaan,
				"Terjadi kesalahan pada data paket. Silakan mulai ulang.", "error", nil)
			return
		}

		var paket models.PaketUmroh
		if config.DB.First(&paket, "id = ?", paketID).Error != nil {
			chatbotResponseReg(c, req.Pertanyaan,
				"Paket tidak ditemukan. Silakan mulai ulang.", "error", nil)
			return
		}
		if paket.KuotaTerpakai >= paket.KuotaMax {
			chatbotResponseReg(c, req.Pertanyaan,
				"Maaf, kuota paket ini sudah penuh. Silakan pilih paket lain.", "error", nil)
			return
		}

		// Parse tanggal lahir
		tl, _ := time.Parse("2006-01-02", rd.TanggalLahir)

		// Buat customer
		customer := models.Customer{
			NIK:           rd.NIK,
			Nama:          rd.Nama,
			TempatLahir:   rd.TempatLahir,
			TanggalLahir:  tl,
			JenisKelamin:  rd.JenisKelamin,
			NoHP:          rd.NoHP,
			Email:         rd.Email,
			AlamatLengkap: rd.AlamatLengkap,
			Provinsi:      rd.Provinsi,
			KabupatenKota: rd.KabupatenKota,
			Kecamatan:     rd.Kecamatan,
			KelurahanDesa: rd.KelurahanDesa,
			KodePos:       rd.KodePos,
			CreatedAt:     time.Now(),
		}
		if config.DB.Create(&customer).Error != nil {
			chatbotResponseReg(c, req.Pertanyaan,
				"Gagal menyimpan data customer. Silakan coba lagi.", "error", nil)
			return
		}

		// Buat invoice
		nomorInvoice, _ := helpers.GenerateNomorInvoice(config.DB)
		invoice := models.Invoice{
			NomorInvoice:     nomorInvoice,
			TotalOrang:       1,
			TotalTagihan:     paket.Harga,
			TotalPembayaran:  0,
			StatusPembayaran: models.InvoiceStatusBelumBayar,
		}
		if config.DB.Create(&invoice).Error != nil {
			chatbotResponseReg(c, req.Pertanyaan,
				"Gagal membuat invoice. Silakan coba lagi.", "error", nil)
			return
		}

		// Buat pendaftaran
		nomor := "UMR-" + time.Now().Format("20060102150405") + "-" + rd.NIK[len(rd.NIK)-4:]
		batasDP := time.Now().Add(24 * time.Hour)
		pendaftaran := models.Pendaftaran{
			CustomerID:         customer.ID,
			PaketID:            paketID,
			UserID:             nil,
			InvoiceID:          &invoice.ID,
			NomorPendaftaran:   nomor,
			DocumentStatus:     helpers.DocumentBelum,
			Status:             helpers.StatusProses,
			RegistrationSource: helpers.SourceChatbot,
			RegisteredBy:       "AI Chatbot",
			TanggalDaftar:      time.Now(),
			BatasWaktuDP:       batasDP,
		}
		if config.DB.Create(&pendaftaran).Error != nil {
			chatbotResponseReg(c, req.Pertanyaan,
				"Gagal menyimpan pendaftaran. Silakan coba lagi.", "error", nil)
			return
		}

		// Update kuota
		config.DB.Model(&paket).Update("kuota_terpakai", paket.KuotaTerpakai+1)

		// Format deadline DP dalam WIB
		wib := time.FixedZone("WIB", 7*3600)
		batasDPStr := batasDP.In(wib).Format("02 January 2006, 15:04 WIB")

		// Log & response sukses
		saveChatLog(req.Pertanyaan, "Registrasi berhasil: "+nomor)
		c.JSON(http.StatusOK, gin.H{
			"message": "Berhasil mendapatkan jawaban",
			"data": gin.H{
				"pertanyaan":        req.Pertanyaan,
				"jawaban":           fmt.Sprintf("✅ **Pendaftaran berhasil!**\n\n📋 Nomor Pendaftaran: **%s**\n🧾 Nomor Invoice: **%s**\n📦 Paket: **%s**\n\n⏰ **Pembayaran DP harus dilakukan paling lambat: %s**\n\nSimpan nomor pendaftaran Anda untuk login ke Portal Jamaah. Admin Bonita akan segera memproses pendaftaran Anda.", nomor, nomorInvoice, paket.NamaPaket, batasDPStr),
				"flow":              "registrasi",
				"step":              "selesai",
				"pendaftaran_id":    pendaftaran.ID.String(),
				"nomor_pendaftaran": nomor,
				"batas_waktu_dp":    batasDP,
				"reg_data":          nil,
			},
		})
		return

	default:
		chatbotResponseReg(c, req.Pertanyaan,
			"Sesi pendaftaran tidak dikenali. Ketik **daftar umroh** untuk memulai.",
			"error", nil)
	}
}

// ── Helpers internal (lanjutan) ───────────────────────────────────────────────

// _ supresses unused import warning for json
var _ = json.Marshal

// saveChatLog — simpan log percakapan (non-fatal)
func saveChatLog(pertanyaan, jawaban string) {
	log := models.ChatbotLog{
		Pertanyaan: pertanyaan,
		Jawaban:    jawaban,
		CreatedAt:  time.Now(),
	}
	config.DB.Create(&log) //nolint
}