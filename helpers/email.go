package helpers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// LogoContentID adalah Content-ID (CID) untuk inline image logo Bonita Umroh pada email HTML.
const LogoContentID = "bonita-logo"

// EmailInfoRow merepresentasikan satu baris informasi terstruktur (key-value) pada kartu detail email.
type EmailInfoRow struct {
	Label      string
	Value      string
	IsBadge    bool
	BadgeBg    string
	BadgeColor string
}

// EmailTemplateData mendefinisikan struktur konten dinamis yang dirender ke dalam template dasar Bonita Umroh.
type EmailTemplateData struct {
	Preheader        string
	CategoryBadge    string
	Title            string
	RecipientName    string
	IntroText        string
	HighlightLabel   string
	HighlightValue   string
	HighlightSubtext string
	InfoRows         []EmailInfoRow
	BodyParagraphs   []string
	WarningTitle     string
	WarningText      string
	WarningVariant   string // "warning" (amber-gold) atau "info" (emerald)
	HasLogo          bool
}

// ResolveBonitaLogoPath mencari lokasi file logo asli Bonita Umroh pada bonita-frontend/src/assets/logo-bonita.jpg
// dan bonita-frontend/public/logo-bonita.jpg.
func ResolveBonitaLogoPath() string {
	if customPath := strings.TrimSpace(os.Getenv("BONITA_LOGO_PATH")); customPath != "" {
		if info, err := os.Stat(customPath); err == nil && !info.IsDir() {
			return customPath
		}
		return ""
	}

	var candidates []string

	if _, callerFile, _, ok := runtime.Caller(0); ok {
		helpersDir := filepath.Dir(callerFile)
		candidates = append(candidates,
			filepath.Clean(filepath.Join(helpersDir, "..", "..", "bonita-frontend", "src", "assets", "logo-bonita.jpg")),
			filepath.Clean(filepath.Join(helpersDir, "..", "..", "bonita-frontend", "public", "logo-bonita.jpg")),
		)
	}

	candidates = append(candidates,
		filepath.Join("..", "bonita-frontend", "src", "assets", "logo-bonita.jpg"),
		filepath.Join("..", "bonita-frontend", "public", "logo-bonita.jpg"),
		filepath.Join("..", "..", "bonita-frontend", "src", "assets", "logo-bonita.jpg"),
		filepath.Join("..", "..", "bonita-frontend", "public", "logo-bonita.jpg"),
		filepath.Join("bonita-frontend", "src", "assets", "logo-bonita.jpg"),
		filepath.Join("bonita-frontend", "public", "logo-bonita.jpg"),
	)

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Size() > 0 {
			return candidate
		}
	}

	return ""
}

// LoadBonitaLogo membaca file logo asli Bonita Umroh untuk disematkan sebagai inline CID attachment.
func LoadBonitaLogo() ([]byte, string, error) {
	logoPath := ResolveBonitaLogoPath()
	if logoPath == "" {
		return nil, "", fmt.Errorf("file logo logo-bonita.jpg tidak ditemukan pada bonita-frontend")
	}

	data, err := os.ReadFile(logoPath)
	if err != nil {
		return nil, logoPath, err
	}
	if len(data) == 0 {
		return nil, logoPath, fmt.Errorf("file logo kosong: %s", logoPath)
	}
	return data, logoPath, nil
}

// RenderBaseEmailTemplate menghasilkan pasangan (htmlBody, plainBody) yang konsisten dengan identitas visual
// dan warna logo hijau resmi Bonita Umroh ("BONITA UMRAH - MITRA UMRAH TERPERCAYA").
// Seluruh data dinamis di-escape menggunakan html.EscapeString untuk mencegah HTML injection.
func RenderBaseEmailTemplate(data EmailTemplateData) (string, string) {
	recipient := strings.TrimSpace(data.RecipientName)
	if recipient == "" {
		recipient = "Jamaah Bonita Umroh"
	}

	escapedRecipient := html.EscapeString(recipient)
	escapedTitle := html.EscapeString(strings.TrimSpace(data.Title))
	escapedCategory := html.EscapeString(strings.TrimSpace(data.CategoryBadge))
	escapedPreheader := html.EscapeString(strings.TrimSpace(data.Preheader))
	if escapedPreheader == "" {
		escapedPreheader = escapedTitle
	}
	escapedIntro := html.EscapeString(strings.TrimSpace(data.IntroText))

	// Header Logo: selaras dengan latar hijau logo (#0e6f22) dan aksen emerald Bonita Umroh.
	var logoHeaderHTML string
	if data.HasLogo {
		logoHeaderHTML = fmt.Sprintf(
			`<img src="cid:%s" alt="Bonita Umroh - Mitra Umrah Terpercaya" width="84" height="84" style="display:block; width:84px; height:84px; max-width:84px; border-radius:16px; border:2px solid rgba(167,243,208,0.35); background-color:#0e6f22; box-shadow:0 4px 14px rgba(0,0,0,0.25); margin:0 auto 14px auto; outline:none; text-decoration:none;" />`,
			LogoContentID,
		)
	} else {
		logoHeaderHTML = `<table role="presentation" cellpadding="0" cellspacing="0" border="0" align="center" style="margin:0 auto 14px auto;">
			<tr>
				<td align="center" valign="middle" width="72" height="72" style="width:72px; height:72px; border-radius:16px; background-color:#0e6f22; border:2px solid #c8963e; color:#ffffff; font-family:'Segoe UI',Roboto,Helvetica,Arial,sans-serif; font-size:26px; font-weight:800; letter-spacing:1px; text-align:center;">
					BU
				</td>
			</tr>
		</table>`
	}

	// Category badge
	var categoryHTML string
	if escapedCategory != "" {
		categoryHTML = fmt.Sprintf(
			`<div style="margin-bottom:12px;">
				<span style="display:inline-block; background-color:#ecfdf5; color:#0f5132; border:1px solid #a7f3d0; border-radius:999px; padding:5px 14px; font-size:11px; font-weight:700; letter-spacing:0.8px; text-transform:uppercase;">%s</span>
			</div>`,
			escapedCategory,
		)
	}

	// Highlight Box (untuk Kode OTP)
	var highlightHTML string
	if strings.TrimSpace(data.HighlightValue) != "" {
		escapedHLabel := html.EscapeString(strings.TrimSpace(data.HighlightLabel))
		escapedHVal := html.EscapeString(strings.TrimSpace(data.HighlightValue))
		escapedHSub := html.EscapeString(strings.TrimSpace(data.HighlightSubtext))

		var subHTML string
		if escapedHSub != "" {
			subHTML = fmt.Sprintf(
				`<div style="margin-top:10px; font-size:13px; line-height:1.5; color:#166534; font-weight:600;">%s</div>`,
				escapedHSub,
			)
		}

		highlightHTML = fmt.Sprintf(
			`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:24px 0;">
				<tr>
					<td align="center" style="background-color:#f0fdf4; border:2px dashed #1a6b43; border-radius:14px; padding:24px 20px; text-align:center;">
						<div style="font-size:11px; font-weight:700; color:#1a6b43; letter-spacing:1.5px; text-transform:uppercase; margin-bottom:8px;">%s</div>
						<div style="font-family:'Courier New',Courier,monospace,Arial,sans-serif; font-size:36px; font-weight:800; letter-spacing:8px; color:#0f5132; line-height:1.2; padding:6px 0;">%s</div>
						%s
					</td>
				</tr>
			</table>`,
			escapedHLabel,
			escapedHVal,
			subHTML,
		)
	}

	// Info Rows Table (untuk Pengaduan, Pembayaran, Pendaftaran, dll.)
	var infoRowsHTML string
	if len(data.InfoRows) > 0 {
		var rowsBuf bytes.Buffer
		for i, row := range data.InfoRows {
			labelEsc := html.EscapeString(strings.TrimSpace(row.Label))
			valEsc := html.EscapeString(strings.TrimSpace(row.Value))
			borderBottom := "border-bottom:1px solid #d8ece2;"
			if i == len(data.InfoRows)-1 {
				borderBottom = ""
			}

			var valueCellContent string
			if row.IsBadge {
				bg := row.BadgeBg
				if bg == "" {
					bg = "#ecfdf5"
				}
				fg := row.BadgeColor
				if fg == "" {
					fg = "#0f5132"
				}
				valueCellContent = fmt.Sprintf(
					`<span style="display:inline-block; background-color:%s; color:%s; border-radius:6px; padding:4px 10px; font-size:12px; font-weight:700; letter-spacing:0.3px;">%s</span>`,
					html.EscapeString(bg),
					html.EscapeString(fg),
					valEsc,
				)
			} else {
				valueCellContent = fmt.Sprintf(`<span style="color:#0a2e1c; font-weight:600; font-size:14px;">%s</span>`, valEsc)
			}

			rowsBuf.WriteString(fmt.Sprintf(
				`<tr>
					<td valign="top" style="padding:12px 16px; color:#475569; font-size:13px; font-weight:500; width:42%%; %s">%s</td>
					<td valign="top" align="right" style="padding:12px 16px; text-align:right; %s">%s</td>
				</tr>`,
				borderBottom, labelEsc, borderBottom, valueCellContent,
			))
		}

		infoRowsHTML = fmt.Sprintf(
			`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:22px 0; background-color:#f4f9f6; border:1px solid #d1e7dd; border-radius:12px; border-collapse:separate; border-spacing:0;">
				%s
			</table>`,
			rowsBuf.String(),
		)
	}

	// Body Paragraphs
	var paragraphsHTML bytes.Buffer
	for _, p := range data.BodyParagraphs {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		paragraphsHTML.WriteString(fmt.Sprintf(
			`<p style="margin:0 0 14px 0; font-size:15px; line-height:1.65; color:#334155;">%s</p>`,
			html.EscapeString(trimmed),
		))
	}

	// Warning / Callout Box (selaras dengan garis emas-perunggu #c8963e dan hijau #1a6b43 pada logo)
	var warningHTML string
	if strings.TrimSpace(data.WarningText) != "" {
		wTitle := html.EscapeString(strings.TrimSpace(data.WarningTitle))
		wText := html.EscapeString(strings.TrimSpace(data.WarningText))

		bgColor := "#fdf8ed"
		borderColor := "#c8963e"
		titleColor := "#92400e"
		textColor := "#78350f"
		if data.WarningVariant == "info" {
			bgColor = "#f0fdf4"
			borderColor = "#1a6b43"
			titleColor = "#0f5132"
			textColor = "#14532d"
		}

		var titlePart string
		if wTitle != "" {
			titlePart = fmt.Sprintf(`<div style="font-size:13px; font-weight:700; color:%s; margin-bottom:4px;">%s</div>`, titleColor, wTitle)
		}

		warningHTML = fmt.Sprintf(
			`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:20px 0 8px 0;">
				<tr>
					<td style="background-color:%s; border-left:4px solid %s; border-radius:8px; padding:14px 16px;">
						%s
						<div style="font-size:13px; line-height:1.55; color:%s;">%s</div>
					</td>
				</tr>
			</table>`,
			bgColor, borderColor, titlePart, textColor, wText,
		)
	}

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html lang="id">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<meta http-equiv="X-UA-Compatible" content="IE=edge" />
<title>%s</title>
<style type="text/css">
@media only screen and (max-width: 600px) {
  .email-wrapper { padding: 12px 8px !important; }
  .email-content { padding: 24px 20px !important; }
  .email-header { padding: 24px 20px !important; }
  .email-footer { padding: 18px 20px !important; }
}
</style>
</head>
<body style="margin:0; padding:0; background-color:#eef5f1; font-family:'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif; -webkit-font-smoothing:antialiased; color:#1e293b;">
<div style="display:none; max-height:0; overflow:hidden; opacity:0; font-size:1px; line-height:1px; color:#eef5f1;">%s</div>
<table role="presentation" class="email-wrapper" width="100%%" cellpadding="0" cellspacing="0" border="0" style="background-color:#eef5f1; padding:28px 14px;">
	<tr>
		<td align="center">
			<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="max-width:580px; width:100%%; background-color:#ffffff; border-radius:16px; overflow:hidden; border:1px solid #d1e7dd; box-shadow:0 8px 24px rgba(10,46,28,0.08);">
				<!-- Top Emerald & Bronze-Gold Accent Bar -->
				<tr>
					<td style="height:5px; background-color:#c8963e; background-image:linear-gradient(90deg,#0f5132 0%%,#1a6b43 45%%,#c8963e 100%%); font-size:0; line-height:0;">&nbsp;</td>
				</tr>
				<!-- Brand Header -->
				<tr>
					<td class="email-header" align="center" style="background-color:#0f3d28; background-image:linear-gradient(135deg,#0a2e1c 0%%,#0f5132 50%%,#1a6b43 100%%); padding:28px 32px; text-align:center;">
						%s
						<div style="color:#ffffff; font-size:20px; font-weight:700; letter-spacing:1.4px; margin:0;">BONITA UMROH</div>
						<div style="width:56px; height:2px; background-color:#c8963e; margin:7px auto;"></div>
						<div style="color:#a7f3d0; font-size:11px; font-weight:600; letter-spacing:2.2px; margin-top:2px; text-transform:uppercase;">Mitra Umrah Terpercaya</div>
					</td>
				</tr>
				<!-- Main Content Card -->
				<tr>
					<td class="email-content" style="padding:32px 36px; background-color:#ffffff;">
						%s
						<h1 style="margin:0 0 18px 0; font-size:22px; line-height:1.3; font-weight:700; color:#0a2e1c;">%s</h1>
						<p style="margin:0 0 14px 0; font-size:15px; line-height:1.6; color:#1e293b;">Halo <strong>%s</strong>,</p>
						<p style="margin:0 0 16px 0; font-size:15px; line-height:1.65; color:#334155;">%s</p>
						%s
						%s
						%s
						%s
						<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin-top:26px; border-top:1px solid #e2ece7; padding-top:20px;">
							<tr>
								<td style="font-size:14px; line-height:1.6; color:#475569;">
									Salam hangat,<br />
									<strong style="color:#0f5132; font-size:15px;">Tim Bonita Umroh</strong>
								</td>
							</tr>
						</table>
					</td>
				</tr>
				<!-- Footer -->
				<tr>
					<td class="email-footer" align="center" style="background-color:#f4f9f6; border-top:1px solid #d8ece2; padding:20px 32px; text-align:center;">
						<p style="margin:0 0 6px 0; font-size:12px; line-height:1.5; color:#0f5132; font-weight:600;">Bonita Umroh &mdash; Mitra Umrah Terpercaya</p>
						<p style="margin:0; font-size:11px; line-height:1.5; color:#64748b;">Email ini dikirim secara otomatis oleh sistem. Mohon tidak membalas langsung ke alamat email ini.</p>
					</td>
				</tr>
			</table>
		</td>
	</tr>
</table>
</body>
</html>`,
		escapedTitle,
		escapedPreheader,
		logoHeaderHTML,
		categoryHTML,
		escapedTitle,
		escapedRecipient,
		escapedIntro,
		highlightHTML,
		infoRowsHTML,
		paragraphsHTML.String(),
		warningHTML,
	)

	// Susun versi Plain-Text yang bersih dan mudah dibaca
	var plainBuf bytes.Buffer
	plainBuf.WriteString("BONITA UMROH - MITRA UMRAH TERPERCAYA\n")
	plainBuf.WriteString("==================================================\n\n")
	if strings.TrimSpace(data.Title) != "" {
		plainBuf.WriteString(strings.ToUpper(strings.TrimSpace(data.Title)) + "\n\n")
	}
	plainBuf.WriteString(fmt.Sprintf("Halo %s,\n\n", recipient))
	if strings.TrimSpace(data.IntroText) != "" {
		plainBuf.WriteString(strings.TrimSpace(data.IntroText) + "\n\n")
	}
	if strings.TrimSpace(data.HighlightValue) != "" {
		if strings.TrimSpace(data.HighlightLabel) != "" {
			plainBuf.WriteString(strings.TrimSpace(data.HighlightLabel) + ": ")
		}
		plainBuf.WriteString(strings.TrimSpace(data.HighlightValue) + "\n")
		if strings.TrimSpace(data.HighlightSubtext) != "" {
			plainBuf.WriteString(strings.TrimSpace(data.HighlightSubtext) + "\n")
		}
		plainBuf.WriteString("\n")
	}
	if len(data.InfoRows) > 0 {
		for _, row := range data.InfoRows {
			plainBuf.WriteString(fmt.Sprintf("- %s: %s\n", strings.TrimSpace(row.Label), strings.TrimSpace(row.Value)))
		}
		plainBuf.WriteString("\n")
	}
	for _, p := range data.BodyParagraphs {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			plainBuf.WriteString(trimmed + "\n\n")
		}
	}
	if strings.TrimSpace(data.WarningText) != "" {
		if strings.TrimSpace(data.WarningTitle) != "" {
			plainBuf.WriteString(strings.TrimSpace(data.WarningTitle) + ": ")
		}
		plainBuf.WriteString(strings.TrimSpace(data.WarningText) + "\n\n")
	}
	plainBuf.WriteString("Terima kasih,\n")
	plainBuf.WriteString("Tim Bonita Umroh\n\n")
	plainBuf.WriteString("--------------------------------------------------\n")
	plainBuf.WriteString("Email ini dikirim secara otomatis oleh sistem Bonita Umroh.\n")

	return htmlBody, plainBuf.String()
}

// BuildOTPEmail membuat konten email OTP (HTML dan Plain-Text) menggunakan template resmi Bonita Umroh.
func BuildOTPEmail(recipientName string, kodeOTP string) (string, string) {
	_, _, err := LoadBonitaLogo()
	hasLogo := err == nil

	data := EmailTemplateData{
		Preheader:        "Kode verifikasi OTP Anda berlaku selama 5 menit. Jangan berikan kode ini kepada siapa pun.",
		CategoryBadge:    "Verifikasi Keamanan",
		Title:            "Verifikasi Email Anda",
		RecipientName:    recipientName,
		IntroText:        "Kami menerima permintaan akses ke Portal Jamaah Bonita Umroh. Silakan gunakan kode One-Time Password (OTP) berikut untuk menyelesaikan proses verifikasi Anda:",
		HighlightLabel:   "Kode OTP Anda",
		HighlightValue:   kodeOTP,
		HighlightSubtext: "OTP berlaku selama 5 menit sejak email ini dikirimkan.",
		BodyParagraphs: []string{
			"Apabila Anda tidak merasa melakukan permintaan kode verifikasi ini, silakan abaikan email ini. Akun dan data pendaftaran Anda tetap aman.",
		},
		WarningTitle:   "Peringatan Keamanan",
		WarningText:    "Jangan berikan kode ini kepada siapa pun, termasuk pihak yang mengatasnamakan Admin atau Tim Bonita Umroh. Kode verifikasi ini bersifat rahasia.",
		WarningVariant: "warning",
		HasLogo:        hasLogo,
	}

	return RenderBaseEmailTemplate(data)
}

// BuildOTPEmailHTML mengembalikan string HTML dari template email OTP Bonita Umroh.
func BuildOTPEmailHTML(recipientName string, kodeOTP string) string {
	htmlBody, _ := BuildOTPEmail(recipientName, kodeOTP)
	return htmlBody
}

// BuildPengaduanEmail membuat konten email notifikasi status pengaduan (HTML dan Plain-Text) menggunakan template bersama Bonita Umroh.
func BuildPengaduanEmail(recipientName string, nomorReferensi string, status string, detail ...string) (string, string) {
	_, _, err := LoadBonitaLogo()
	hasLogo := err == nil

	statusLower := strings.ToLower(strings.TrimSpace(status))
	var title, intro, followUp, badgeLabel, badgeBg, badgeColor string

	switch statusLower {
	case "diproses":
		title = "Pengaduan Anda Sedang Diproses"
		badgeLabel = "Sedang Diproses"
		badgeBg = "#fef3c7"
		badgeColor = "#92400e"
		intro = fmt.Sprintf("Pengaduan Anda dengan nomor referensi %s saat ini telah berstatus Diproses.", strings.TrimSpace(nomorReferensi))
		followUp = "Admin Bonita Umroh sedang menindaklanjuti pengaduan Anda dengan saksama. Jika ada pertanyaan, Anda dapat menghubungi kami kembali melalui aplikasi."
	case "selesai":
		title = "Pengaduan Anda Telah Selesai"
		badgeLabel = "Selesai Ditangani"
		badgeBg = "#dcfce7"
		badgeColor = "#166534"
		intro = fmt.Sprintf("Pengaduan Anda dengan nomor referensi %s telah berstatus Selesai.", strings.TrimSpace(nomorReferensi))
		followUp = "Pengaduan Anda telah selesai ditangani oleh Admin Bonita Umroh. Jika ada pertanyaan lebih lanjut, Anda dapat menghubungi kami kembali melalui aplikasi."
	default:
		title = "Pembaruan Status Pengaduan"
		badgeLabel = strings.Title(statusLower)
		badgeBg = "#e2e8f0"
		badgeColor = "#334155"
		intro = fmt.Sprintf("Status pengaduan Anda dengan nomor referensi %s telah diperbarui.", strings.TrimSpace(nomorReferensi))
		followUp = "Terima kasih telah menyampaikan masukan kepada kami."
	}

	infoRows := []EmailInfoRow{
		{
			Label: "Nomor Referensi Pendaftaran",
			Value: nomorReferensi,
		},
	}
	if len(detail) > 0 && strings.TrimSpace(detail[0]) != "" {
		infoRows = append(infoRows, EmailInfoRow{
			Label: "Judul Pengaduan",
			Value: strings.TrimSpace(detail[0]),
		})
	}
	if len(detail) > 1 && strings.TrimSpace(detail[1]) != "" {
		infoRows = append(infoRows, EmailInfoRow{
			Label: "Kategori",
			Value: strings.TrimSpace(detail[1]),
		})
	}
	infoRows = append(infoRows, EmailInfoRow{
		Label:      "Status Pengaduan",
		Value:      badgeLabel,
		IsBadge:    true,
		BadgeBg:    badgeBg,
		BadgeColor: badgeColor,
	})

	data := EmailTemplateData{
		Preheader:      intro,
		CategoryBadge:  "Layanan Pengaduan Jamaah",
		Title:          title,
		RecipientName:  recipientName,
		IntroText:      intro,
		InfoRows:       infoRows,
		BodyParagraphs: []string{followUp},
		WarningTitle:   "Informasi Layanan",
		WarningText:    "Anda dapat memantau riwayat dan perkembangan pengaduan secara langsung melalui menu Pengaduan pada Portal Jamaah Bonita Umroh.",
		WarningVariant: "info",
		HasLogo:        hasLogo,
	}

	return RenderBaseEmailTemplate(data)
}

// BuildPengaduanEmailHTML mengembalikan string HTML untuk email notifikasi pengaduan.
func BuildPengaduanEmailHTML(recipientName string, nomorReferensi string, status string, detail ...string) string {
	htmlBody, _ := BuildPengaduanEmail(recipientName, nomorReferensi, status, detail...)
	return htmlBody
}

// BuildPembayaranEmail menyiapkan template notifikasi pembayaran terstruktur yang konsisten dengan template dasar Bonita Umroh.
func BuildPembayaranEmail(recipientName string, nomorPendaftaran string, nomorInvoice string, nominal string, statusPembayaran string) (string, string) {
	_, _, err := LoadBonitaLogo()
	hasLogo := err == nil

	statusClean := strings.TrimSpace(statusPembayaran)
	badgeBg := "#ecfdf5"
	badgeColor := "#0f5132"
	if strings.EqualFold(statusClean, "ditolak") || strings.EqualFold(statusClean, "belum") {
		badgeBg = "#fef2f2"
		badgeColor = "#991b1b"
	} else if strings.EqualFold(statusClean, "pending") || strings.EqualFold(statusClean, "menunggu") {
		badgeBg = "#fef3c7"
		badgeColor = "#92400e"
	}

	var rows []EmailInfoRow
	if strings.TrimSpace(nomorPendaftaran) != "" {
		rows = append(rows, EmailInfoRow{Label: "Nomor Pendaftaran", Value: nomorPendaftaran})
	}
	if strings.TrimSpace(nomorInvoice) != "" {
		rows = append(rows, EmailInfoRow{Label: "Nomor Invoice", Value: nomorInvoice})
	}
	if strings.TrimSpace(nominal) != "" {
		rows = append(rows, EmailInfoRow{Label: "Nominal Pembayaran", Value: nominal})
	}
	if statusClean != "" {
		rows = append(rows, EmailInfoRow{
			Label:      "Status Pembayaran",
			Value:      statusClean,
			IsBadge:    true,
			BadgeBg:    badgeBg,
			BadgeColor: badgeColor,
		})
	}

	data := EmailTemplateData{
		Preheader:      fmt.Sprintf("Informasi status pembayaran untuk pendaftaran %s.", strings.TrimSpace(nomorPendaftaran)),
		CategoryBadge:  "Informasi Pembayaran",
		Title:          "Informasi Status Pembayaran",
		RecipientName:  recipientName,
		IntroText:      "Berikut adalah rincian informasi pembayaran ibadah Umroh Anda yang tercatat pada sistem Bonita Umroh:",
		InfoRows:       rows,
		BodyParagraphs: []string{"Silakan masuk ke Portal Jamaah Bonita Umroh untuk melihat rincian tagihan, riwayat pembayaran, serta mengunduh bukti invoice Anda."},
		WarningTitle:   "Catatan Penting",
		WarningText:    "Pastikan seluruh transaksi pembayaran hanya dilakukan melalui rekening resmi yang tertera pada halaman pembayaran aplikasi Bonita Umroh.",
		WarningVariant: "info",
		HasLogo:        hasLogo,
	}

	return RenderBaseEmailTemplate(data)
}

// isHTMLContent memeriksa apakah string body sudah merupakan dokumen HTML lengkap.
func isHTMLContent(body string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html")
}

var (
	reStyleAndTitle = regexp.MustCompile(`(?is)<(style|title|head)[^>]*>.*?</(style|title|head)>`)
	reBlockEndTags  = regexp.MustCompile(`(?i)</(p|div|tr|h1|h2|h3|li|table)>|<br\s*/?>`)
	reAllHTMLTags   = regexp.MustCompile(`<[^>]+>`)
	reMultiNewlines = regexp.MustCompile(`\n{3,}`)
)

// HTMLToPlainText mengekstrak teks biasa (plain-text alternative) dari dokumen HTML email.
func HTMLToPlainText(htmlContent string) string {
	cleaned := reStyleAndTitle.ReplaceAllString(htmlContent, "")
	cleaned = reBlockEndTags.ReplaceAllString(cleaned, "\n")
	cleaned = reAllHTMLTags.ReplaceAllString(cleaned, "")
	cleaned = html.UnescapeString(cleaned)
	cleaned = strings.ReplaceAll(cleaned, "\u00a0", " ")
	cleaned = strings.ReplaceAll(cleaned, "&nbsp;", " ")
	cleaned = strings.ReplaceAll(cleaned, "&bull;", "-")
	cleaned = strings.ReplaceAll(cleaned, "&mdash;", "-")

	lines := strings.Split(cleaned, "\n")
	var outLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "." {
			trimmed = ""
		}
		outLines = append(outLines, trimmed)
	}

	result := strings.TrimSpace(strings.Join(outLines, "\n"))
	result = reMultiNewlines.ReplaceAllString(result, "\n\n")
	return result
}

// wrapBase64Lines memformat string base64 menjadi baris-baris sepanjang 76 karakter sesuai standar MIME RFC 2045.
func wrapBase64Lines(encoded string) string {
	const maxLen = 76
	var buf bytes.Buffer
	for len(encoded) > maxLen {
		buf.WriteString(encoded[:maxLen])
		buf.WriteString("\r\n")
		encoded = encoded[maxLen:]
	}
	if len(encoded) > 0 {
		buf.WriteString(encoded)
		buf.WriteString("\r\n")
	}
	return buf.String()
}

// sanitizeSMTPBody memastikan tidak ada baris tunggal yang hanya berisi "." agar tidak memutus stream SMTP DATA.
func sanitizeSMTPBody(s string) string {
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "." {
			lines[i] = " ."
		}
	}
	return strings.Join(lines, "\r\n")
}

// BuildMIMEEmail menyusun payload MIME email yang mendukung multipart/alternative (plain-text + HTML)
// serta multipart/related untuk menyematkan logo asli Bonita Umroh via Content-ID (CID).
func BuildMIMEEmail(from string, to string, subject string, htmlBody string, plainBody string, logoBytes []byte) []byte {
	const relatedBoundary = "BonitaRelBoundary_9f8e7d6c5b4a"
	const altBoundary = "BonitaAltBoundary_1a2b3c4d5e6f"

	safeSubject := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(subject), "\r", ""), "\n", " ")
	safeFrom := strings.TrimSpace(from)
	safeTo := strings.TrimSpace(to)

	if strings.TrimSpace(plainBody) == "" {
		plainBody = HTMLToPlainText(htmlBody)
	}

	safePlain := sanitizeSMTPBody(plainBody)
	safeHTML := sanitizeSMTPBody(htmlBody)

	var msg bytes.Buffer
	if safeFrom != "" {
		msg.WriteString(fmt.Sprintf("From: Bonita Umroh <%s>\r\n", safeFrom))
	}
	if safeTo != "" {
		msg.WriteString(fmt.Sprintf("To: %s\r\n", safeTo))
	}
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", safeSubject))
	msg.WriteString("MIME-Version: 1.0\r\n")

	attachLogo := len(logoBytes) > 0 && strings.Contains(htmlBody, "cid:"+LogoContentID)

	if attachLogo {
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/related; boundary=\"%s\"\r\n\r\n", relatedBoundary))
		msg.WriteString(fmt.Sprintf("--%s\r\n", relatedBoundary))
	}

	msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", altBoundary))

	// Bagian 1: Plain-Text Alternative
	msg.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
	msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	msg.WriteString(safePlain)
	msg.WriteString("\r\n\r\n")

	// Bagian 2: HTML Email
	msg.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
	msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	msg.WriteString(safeHTML)
	msg.WriteString("\r\n\r\n")

	msg.WriteString(fmt.Sprintf("--%s--\r\n", altBoundary))

	// Bagian 3: Inline CID Logo Bonita Umroh (jika tersedia)
	if attachLogo {
		msg.WriteString(fmt.Sprintf("\r\n--%s\r\n", relatedBoundary))
		msg.WriteString("Content-Type: image/jpeg; name=\"logo-bonita.jpg\"\r\n")
		msg.WriteString("Content-Transfer-Encoding: base64\r\n")
		msg.WriteString(fmt.Sprintf("Content-ID: <%s>\r\n", LogoContentID))
		msg.WriteString("Content-Disposition: inline; filename=\"logo-bonita.jpg\"\r\n\r\n")
		encodedLogo := base64.StdEncoding.EncodeToString(logoBytes)
		msg.WriteString(wrapBase64Lines(encodedLogo))
		msg.WriteString(fmt.Sprintf("--%s--\r\n", relatedBoundary))
	}

	return msg.Bytes()
}

// SendEmail mengirim email melalui konfigurasi SMTP Gmail (.env).
// Fungsi ini mempertahankan signature asli `SendEmail(to, subject, body string) error` untuk menjaga
// kompatibilitas penuh dengan seluruh controller dan unit test yang sudah ada.
func SendEmail(to string, subject string, body string) error {
	from := os.Getenv("EMAIL_USERNAME")
	password := os.Getenv("EMAIL_PASSWORD")
	host := os.Getenv("EMAIL_HOST")
	port := os.Getenv("EMAIL_PORT")

	address := host + ":" + port

	auth := smtp.PlainAuth(
		"",
		from,
		password,
		host,
	)

	logoBytes, _, _ := LoadBonitaLogo()

	var htmlBody, plainBody string
	if isHTMLContent(body) {
		htmlBody = body
		plainBody = HTMLToPlainText(body)
	} else {
		// Jika pemanggil mengirim teks biasa, bungkus otomatis ke dalam template dasar Bonita Umroh
		paragraphs := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n")
		htmlBody, plainBody = RenderBaseEmailTemplate(EmailTemplateData{
			Preheader:      subject,
			CategoryBadge:  "Notifikasi Sistem",
			Title:          subject,
			RecipientName:  "Jamaah Bonita Umroh",
			BodyParagraphs: paragraphs,
			HasLogo:        len(logoBytes) > 0,
		})
	}

	message := BuildMIMEEmail(from, to, subject, htmlBody, plainBody, logoBytes)

	err := smtp.SendMail(
		address,
		auth,
		from,
		[]string{to},
		message,
	)
	if err != nil {
		return err
	}

	fmt.Println("Email berhasil dikirim ke:", to)

	return nil
}
