package controllers

import (
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

type ChatbotRequest struct {
	Pertanyaan    string `json:"pertanyaan" binding:"required"`
	Flow          string `json:"flow"`           // "" | "pengaduan"
	Step          string `json:"step"`           // ask_nomor | ask_kategori | ask_isi
	NomorUMR      string `json:"nomor_umr"`      // diisi setelah step ask_nomor berhasil
	PendaftaranID string `json:"pendaftaran_id"` // UUID
	Kategori      string `json:"kategori"`       // diisi setelah step ask_kategori
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
				chatbotResponse(c, req.Pertanyaan,
					"Nomor UMR tidak ditemukan. Silakan periksa kembali dan coba lagi dengan format: **UMR-YYYYMMDDHHMMSS**",
					"ask_nomor", "")
				return
			}

			chatbotResponse(c, req.Pertanyaan,
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
				chatbotResponse(c, req.Pertanyaan,
					"Kategori tidak valid. Silakan pilih salah satu kategori yang tersedia.",
					"ask_kategori", req.PendaftaranID)
				return
			}

			chatbotResponse(c, req.Pertanyaan,
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
					chatbotResponse(c, req.Pertanyaan,
						"Isi pengaduan tidak boleh kosong. Silakan jelaskan keluhan Anda.",
						req.Step, req.PendaftaranID)
					return
				}

				// parse pendaftaran ID
				pendaftaranID, err := uuid.Parse(req.PendaftaranID)
				if err != nil {
					chatbotResponse(c, req.Pertanyaan,
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
					chatbotResponse(c, req.Pertanyaan,
						"Maaf, terjadi kesalahan saat menyimpan pengaduan. Silakan coba lagi.",
						req.Step, req.PendaftaranID)
					return
				}

				// log chatbot
				saveChatLog(req.Pertanyaan, "Pengaduan berhasil dikirim. ID: "+pengaduan.ID.String())

				chatbotResponse(c, req.Pertanyaan,
					"✅ **Terima kasih!** Laporan Anda berhasil dikirim.\n\n"+
						"Admin Bonita akan segera menindaklanjuti pengaduan Anda.\n\n"+
						"Ada yang bisa kami bantu lagi?",
					"done", "")
				return
			}
		}

		// fallback jika step tidak dikenali
		chatbotResponse(c, req.Pertanyaan,
			"Sesi pengaduan tidak dikenali. Silakan mulai ulang.", "error", "")
		return
	}

	// ── DETEKSI INTENT PENGADUAN ─────────────────────────────────────────────

	if detectIntentPengaduan(req.Pertanyaan) {
		chatbotResponse(c, req.Pertanyaan,
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
func chatbotResponse(c *gin.Context, pertanyaan, jawaban, nextStep, pendaftaranID string) {
	saveChatLog(pertanyaan, jawaban)

	c.JSON(http.StatusOK, gin.H{
		"message": "Berhasil mendapatkan jawaban",
		"data": gin.H{
			"pertanyaan":     pertanyaan,
			"jawaban":        jawaban,
			"flow":           "pengaduan",
			"step":           nextStep,
			"pendaftaran_id": pendaftaranID,
		},
	})
}

// saveChatLog — simpan log percakapan (non-fatal)
func saveChatLog(pertanyaan, jawaban string) {
	log := models.ChatbotLog{
		Pertanyaan: pertanyaan,
		Jawaban:    jawaban,
		CreatedAt:  time.Now(),
	}
	config.DB.Create(&log) //nolint
}