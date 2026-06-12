package helpers

import (
	"fmt"
	"net/smtp"
	"os"
)

func SendEmail(to string, subject string, body string) error {

	// ambil konfigurasi dari .env
	from := os.Getenv("EMAIL_USERNAME")
	password := os.Getenv("EMAIL_PASSWORD")
	host := os.Getenv("EMAIL_HOST")
	port := os.Getenv("EMAIL_PORT")

	// alamat SMTP
	address := host + ":" + port

	// authentication Gmail
	auth := smtp.PlainAuth(
		"",
		from,
		password,
		host,
	)

	// format email
	message := []byte(
		"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n" +
			body,
	)

	// kirim email
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