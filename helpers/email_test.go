package helpers

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

func TestLoadBonitaLogo_And_Fallback(t *testing.T) {
	t.Run("LoadsOriginalLogoFromFrontendPublic", func(t *testing.T) {
		logoBytes, logoPath, err := LoadBonitaLogo()
		if err != nil {
			t.Fatalf("Expected original logo to be loaded from bonita-frontend/public/logo-bonita.jpg, got error: %v", err)
		}
		if len(logoBytes) != 9517 {
			t.Errorf("Expected green logo-bonita.jpg (9517 bytes), got %d bytes", len(logoBytes))
		}
		if !strings.HasSuffix(logoPath, "logo-bonita.jpg") {
			t.Errorf("Expected logo path to end with logo-bonita.jpg, got %s", logoPath)
		}
		// JPEG magic bytes: FF D8 FF
		if len(logoBytes) < 3 || logoBytes[0] != 0xFF || logoBytes[1] != 0xD8 || logoBytes[2] != 0xFF {
			t.Errorf("Expected valid JPEG header (FF D8 FF) in logo-bonita.jpg")
		}
	})

	t.Run("GracefulFallbackWhenLogoNotAccessible", func(t *testing.T) {
		prevEnv := os.Getenv("BONITA_LOGO_PATH")
		_ = os.Setenv("BONITA_LOGO_PATH", "/path/tidak/ada/logo-bonita.jpg")
		defer func() {
			if prevEnv == "" {
				_ = os.Unsetenv("BONITA_LOGO_PATH")
			} else {
				_ = os.Setenv("BONITA_LOGO_PATH", prevEnv)
			}
		}()

		_, _, err := LoadBonitaLogo()
		if err == nil {
			t.Fatal("Expected error when BONITA_LOGO_PATH points to non-existent file")
		}

		htmlBody, plainBody := BuildOTPEmail("Tegar Ananda", "491673")
		if strings.Contains(htmlBody, "cid:"+LogoContentID) {
			t.Error("Expected fallback HTML header without broken cid: image when logo is unavailable")
		}
		if !strings.Contains(htmlBody, "BONITA UMROH") {
			t.Error("Expected BONITA UMROH header identity in fallback HTML")
		}
		if !strings.Contains(plainBody, "491673") {
			t.Error("Expected OTP code in plain-text output during fallback")
		}
	})
}

func TestBuildOTPEmail_TemplateAndDynamicData(t *testing.T) {
	t.Run("RendersDynamicOTPAndRecipientNameCorrectly", func(t *testing.T) {
		recipient := "Tegar Ananda"
		dynamicOTP := "839201"

		htmlBody, plainBody := BuildOTPEmail(recipient, dynamicOTP)

		// Struktur HTML & styling inline
		requiredHTMLParts := []string{
			"<!DOCTYPE html>",
			"BONITA UMROH",
			"Verifikasi Email Anda",
			"Halo <strong>Tegar Ananda</strong>",
			dynamicOTP,
			"5 menit",
			"Jangan berikan kode ini kepada siapa pun",
			"Tim Bonita Umroh",
			"dikirim secara otomatis oleh sistem",
			"cid:" + LogoContentID,
			"#0f5132", // Hijau tua Bonita Umroh
			"#1a6b43", // Hijau emerald Bonita Umroh
		}
		for _, part := range requiredHTMLParts {
			if !strings.Contains(htmlBody, part) {
				t.Errorf("HTML email missing expected part %q", part)
			}
		}

		// Pastikan kode contoh 491673 tidak di-hardcode ketika kode dinamis berbeda dikirim
		if strings.Contains(htmlBody, "491673") {
			t.Error("Template must not hardcode example OTP 491673 when dynamic OTP is 839201")
		}

		// Versi Plain-Text
		requiredPlainParts := []string{
			"BONITA UMROH",
			"VERIFIKASI EMAIL ANDA",
			"Halo Tegar Ananda,",
			dynamicOTP,
			"5 menit",
			"Jangan berikan kode ini kepada siapa pun",
			"Tim Bonita Umroh",
			"dikirim secara otomatis",
		}
		for _, part := range requiredPlainParts {
			if !strings.Contains(plainBody, part) {
				t.Errorf("Plain-text email missing expected part %q", part)
			}
		}
	})

	t.Run("FallbackGreetingWhenRecipientNameEmpty", func(t *testing.T) {
		htmlBody, plainBody := BuildOTPEmail("   ", "112233")
		if !strings.Contains(htmlBody, "Halo <strong>Jamaah Bonita Umroh</strong>") {
			t.Errorf("Expected default greeting in HTML when name is empty, got: %s", htmlBody)
		}
		if !strings.Contains(plainBody, "Halo Jamaah Bonita Umroh,") {
			t.Errorf("Expected default greeting in plain text when name is empty, got: %s", plainBody)
		}
	})

	t.Run("HTMLEscapingPreventsInjection", func(t *testing.T) {
		maliciousName := `<script>alert("xss")</script> & "Budi"`
		maliciousOTP := `<b>123456</b>`

		htmlBody, _ := BuildOTPEmail(maliciousName, maliciousOTP)

		if strings.Contains(htmlBody, `<script>alert("xss")</script>`) {
			t.Fatal("Vulnerability: raw <script> tag was not escaped in HTML email!")
		}
		if strings.Contains(htmlBody, `<b>123456</b>`) {
			t.Fatal("Vulnerability: raw <b> tag in OTP was not escaped in HTML email!")
		}
		if !strings.Contains(htmlBody, `&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt; &amp; &#34;Budi&#34;`) {
			t.Errorf("Expected escaped recipient name in HTML body")
		}
	})
}

func TestSharedNotificationTemplates(t *testing.T) {
	t.Run("PengaduanDiprosesAndSelesai", func(t *testing.T) {
		htmlDiproses, plainDiproses := BuildPengaduanEmail(
			"Hj. Siti Aminah",
			"UMR-20261029-001",
			"diproses",
			"Jadwal Manasik <Urgent>",
			"Layanan & Informasi",
		)

		if !strings.Contains(htmlDiproses, "Pengaduan Anda Sedang Diproses") {
			t.Error("Expected Diproses title in HTML")
		}
		if !strings.Contains(htmlDiproses, "UMR-20261029-001") {
			t.Error("Expected nomor referensi in HTML")
		}
		if !strings.Contains(htmlDiproses, "Jadwal Manasik &lt;Urgent&gt;") {
			t.Error("Expected escaped judul pengaduan in HTML")
		}
		if !strings.Contains(htmlDiproses, "Layanan &amp; Informasi") {
			t.Error("Expected escaped kategori pengaduan in HTML")
		}
		if !strings.Contains(plainDiproses, "UMR-20261029-001") {
			t.Error("Expected nomor referensi in plain text")
		}

		htmlSelesai, _ := BuildPengaduanEmail(
			"Hj. Siti Aminah",
			"UMR-20261029-001",
			"selesai",
		)
		if !strings.Contains(htmlSelesai, "Pengaduan Anda Telah Selesai") {
			t.Error("Expected Selesai title in HTML")
		}
		if !strings.Contains(htmlSelesai, "Selesai Ditangani") {
			t.Error("Expected Selesai Ditangani badge in HTML")
		}
	})

	t.Run("PembayaranNotificationTemplate", func(t *testing.T) {
		htmlPay, plainPay := BuildPembayaranEmail(
			"H. Ahmad Fauzi",
			"UMR-20261029-009",
			"INV-BNT-2026-009",
			"Rp 10.000.000",
			"DP Diterima",
		)

		for _, expected := range []string{"UMR-20261029-009", "INV-BNT-2026-009", "Rp 10.000.000", "DP Diterima"} {
			if !strings.Contains(htmlPay, expected) {
				t.Errorf("Expected %q in pembayaran HTML email", expected)
			}
			if !strings.Contains(plainPay, expected) {
				t.Errorf("Expected %q in pembayaran plain-text email", expected)
			}
		}

		// Cabang status ditolak & pending
		htmlRejected, _ := BuildPembayaranEmail("Jamaah", "UMR-1", "INV-1", "Rp 5.000.000", "ditolak")
		if !strings.Contains(htmlRejected, "#fef2f2") {
			t.Error("Expected red badge background #fef2f2 for ditolak payment status")
		}
		htmlPending, _ := BuildPembayaranEmail("Jamaah", "UMR-1", "INV-1", "Rp 5.000.000", "pending")
		if !strings.Contains(htmlPending, "#fef3c7") {
			t.Error("Expected amber badge background #fef3c7 for pending payment status")
		}
	})

	t.Run("PengaduanDefaultStatus_And_HTMLHelper", func(t *testing.T) {
		htmlDefault := BuildPengaduanEmailHTML("Jamaah", "UMR-20261029-999", "menunggu")
		if !strings.Contains(htmlDefault, "Pembaruan Status Pengaduan") {
			t.Error("Expected default pengaduan title when status is not diproses/selesai")
		}
	})

	t.Run("EmptyLogoFileReturnsError_And_SanitizeStandaloneDot", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "empty-logo-*.jpg")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		_ = tmpFile.Close()
		defer os.Remove(tmpFile.Name())

		prevEnv := os.Getenv("BONITA_LOGO_PATH")
		_ = os.Setenv("BONITA_LOGO_PATH", tmpFile.Name())
		defer func() {
			if prevEnv == "" {
				_ = os.Unsetenv("BONITA_LOGO_PATH")
			} else {
				_ = os.Setenv("BONITA_LOGO_PATH", prevEnv)
			}
		}()

		if _, _, err := LoadBonitaLogo(); err == nil {
			t.Error("Expected error when logo file is 0 bytes")
		}

		// Test standalone dot sanitization & empty plainBody fallback in BuildMIMEEmail
		mimeBytes := BuildMIMEEmail("from@bonita.com", "to@bonita.com", "Subj", "<html><body><p>Baris 1</p><p>.</p><p>Baris 2</p></body></html>", "", nil)
		for _, line := range strings.Split(string(mimeBytes), "\r\n") {
			if strings.TrimSpace(line) == "." {
				t.Fatal("Found unsanitized standalone dot line in MIME output")
			}
		}
	})

	t.Run("SendEmail_ReturnsErrorWhenSMTPUnreachable", func(t *testing.T) {
		prevHost := os.Getenv("EMAIL_HOST")
		prevPort := os.Getenv("EMAIL_PORT")
		_ = os.Setenv("EMAIL_HOST", "127.0.0.1")
		_ = os.Setenv("EMAIL_PORT", "1")
		defer func() {
			_ = os.Setenv("EMAIL_HOST", prevHost)
			_ = os.Setenv("EMAIL_PORT", prevPort)
		}()

		if err := SendEmail("to@bonita.com", "Test", "Body"); err == nil {
			t.Error("Expected error when SMTP server is unreachable")
		}
	})
}

func TestBuildMIMEEmail_MultipartAndCIDAttachment(t *testing.T) {
	logoBytes, _, err := LoadBonitaLogo()
	if err != nil {
		t.Fatalf("Failed to load logo: %v", err)
	}

	htmlBody, plainBody := BuildOTPEmail("Tegar Ananda", "491673")
	mimeBytes := BuildMIMEEmail("bonitaumrah.system@gmail.com", "jamaah@example.com", "Kode OTP Bonita Travel", htmlBody, plainBody, logoBytes)
	mimeStr := string(mimeBytes)

	expectedHeaders := []string{
		"From: Bonita Umroh <bonitaumrah.system@gmail.com>",
		"To: jamaah@example.com",
		"Subject: Kode OTP Bonita Travel",
		"MIME-Version: 1.0",
		"Content-Type: multipart/related;",
		"Content-Type: multipart/alternative;",
		"Content-Type: text/plain; charset=\"UTF-8\"",
		"Content-Type: text/html; charset=\"UTF-8\"",
		"Content-Type: image/jpeg; name=\"logo-bonita.jpg\"",
		"Content-Transfer-Encoding: base64",
		"Content-ID: <" + LogoContentID + ">",
		"Content-Disposition: inline; filename=\"logo-bonita.jpg\"",
	}

	for _, hdr := range expectedHeaders {
		if !strings.Contains(mimeStr, hdr) {
			t.Errorf("MIME message missing expected header/section: %q", hdr)
		}
	}

	// Pastikan tidak ada baris tunggal "." yang dapat memutus stream DATA SMTP
	for _, line := range strings.Split(mimeStr, "\r\n") {
		if strings.TrimSpace(line) == "." {
			t.Fatal("MIME message contains a standalone '.' line which would prematurely terminate SMTP DATA stream")
		}
	}
}

func TestSendEmail_WithMockSMTPAndLegacyPlainText(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Skipping mock TCP SMTP test (socket listen unavailable in restricted environment): %v", err)
	}
	defer l.Close()

	addr := l.Addr().(*net.TCPAddr)
	host := addr.IP.String()
	port := strconv.Itoa(addr.Port)

	origUser := os.Getenv("EMAIL_USERNAME")
	origPass := os.Getenv("EMAIL_PASSWORD")
	origHost := os.Getenv("EMAIL_HOST")
	origPort := os.Getenv("EMAIL_PORT")
	defer func() {
		_ = os.Setenv("EMAIL_USERNAME", origUser)
		_ = os.Setenv("EMAIL_PASSWORD", origPass)
		_ = os.Setenv("EMAIL_HOST", origHost)
		_ = os.Setenv("EMAIL_PORT", origPort)
	}()

	_ = os.Setenv("EMAIL_USERNAME", "test@bonita.com")
	_ = os.Setenv("EMAIL_PASSWORD", "mockpassword")
	_ = os.Setenv("EMAIL_HOST", host)
	_ = os.Setenv("EMAIL_PORT", port)

	receivedData := make(chan string, 2)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				w := bufio.NewWriter(c)
				_, _ = w.WriteString("220 localhost ESMTP\r\n")
				_ = w.Flush()
				var dataBuf strings.Builder
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					up := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(up, "EHLO") || strings.HasPrefix(up, "HELO"):
						_, _ = w.WriteString("250-localhost\r\n250 AUTH PLAIN\r\n")
						_ = w.Flush()
					case strings.HasPrefix(up, "AUTH PLAIN"):
						_, _ = w.WriteString("235 2.7.0 OK\r\n")
						_ = w.Flush()
					case strings.HasPrefix(up, "MAIL FROM:") || strings.HasPrefix(up, "RCPT TO:"):
						_, _ = w.WriteString("250 OK\r\n")
						_ = w.Flush()
					case strings.HasPrefix(up, "DATA"):
						_, _ = w.WriteString("354 End data with <CR><LF>.<CR><LF>\r\n")
						_ = w.Flush()
						for {
							dl, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if strings.TrimSpace(dl) == "." {
								break
							}
							dataBuf.WriteString(dl)
						}
						receivedData <- dataBuf.String()
						_, _ = w.WriteString("250 OK\r\n")
						_ = w.Flush()
					case strings.HasPrefix(up, "QUIT"):
						_, _ = w.WriteString("221 Bye\r\n")
						_ = w.Flush()
						return
					default:
						_, _ = w.WriteString("250 OK\r\n")
						_ = w.Flush()
					}
				}
			}(conn)
		}
	}()

	// 1. Kirim email HTML OTP
	otpHTML := BuildOTPEmailHTML("Tegar Ananda", "491673")
	if err := SendEmail("jamaah@bonita.com", "Kode OTP Bonita Travel", otpHTML); err != nil {
		t.Fatalf("SendEmail with HTML failed: %v", err)
	}
	rawMsg1 := <-receivedData
	if !strings.Contains(rawMsg1, "Content-ID: <"+LogoContentID+">") || !strings.Contains(rawMsg1, "491673") {
		t.Error("Expected inline CID logo and OTP code in transmitted SMTP payload")
	}

	// 2. Kirim email teks biasa (legacy caller) -> otomatis dibungkus template HTML Bonita Umroh
	if err := SendEmail("jamaah@bonita.com", "Informasi Sistem", "Pesan notifikasi sederhana.\n\nParagraf kedua."); err != nil {
		t.Fatalf("SendEmail with plain text failed: %v", err)
	}
	rawMsg2 := <-receivedData
	if !strings.Contains(rawMsg2, "BONITA UMROH") || !strings.Contains(rawMsg2, "Pesan notifikasi sederhana.") {
		t.Error("Expected legacy plain text call to be wrapped in Bonita Umroh HTML template")
	}
}

func TestLiveSMTP_OTPEmail_IfEnabled(t *testing.T) {
	if os.Getenv("RUN_LIVE_SMTP_TEST") != "1" {
		t.Skip("Skipping live SMTP test; set RUN_LIVE_SMTP_TEST=1 to test real Gmail SMTP delivery")
	}

	_ = godotenv.Load("../.env")
	recipient := strings.TrimSpace(os.Getenv("EMAIL_USERNAME"))
	if recipient == "" {
		t.Fatal("EMAIL_USERNAME is empty in .env")
	}

	htmlBody := BuildOTPEmailHTML("Tegar Ananda", "491673")
	if err := SendEmail(recipient, "Kode OTP Bonita Travel", htmlBody); err != nil {
		t.Fatalf("Live SMTP OTP delivery failed: %v", err)
	}
}
