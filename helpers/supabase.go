package helpers

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"strings"

	"github.com/go-resty/resty/v2"
)

func UploadToSupabase(file multipart.File, fileName string, bucket string) (string, error) {

	// baca isi file
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	url := os.Getenv("SUPABASE_URL")
	key := os.Getenv("SUPABASE_SECRET_KEY")

	uploadURL := fmt.Sprintf(
		"%s/storage/v1/object/%s/%s",
		url,
		bucket,
		fileName,
	)

	client := resty.New()

	resp, err := client.R().
		SetHeader("Authorization", "Bearer "+key).
		SetHeader("apikey", key).
		SetHeader("Content-Type", "application/octet-stream").
		SetBody(bytes.NewReader(fileBytes)).
		Put(uploadURL)

	if err != nil {
		return "", err
	}

	if resp.StatusCode() >= 300 {
		return "", fmt.Errorf(
			"supabase error: %s",
			resp.String(),
		)
	}

	// buat URL public
	publicURL := fmt.Sprintf(
		"%s/storage/v1/object/public/%s/%s",
		url,
		bucket,
		fileName,
	)

	return publicURL, nil
}

func DeleteFromSupabase(fileURL string, bucket string) error {

	// kalau kosong tidak perlu hapus
	if fileURL == "" {
		return nil
	}

	// contoh URL:
	// https://xxx.supabase.co/storage/v1/object/public/paket/foto.jpg

	prefix := fmt.Sprintf(
		"%s/storage/v1/object/public/%s/",
		os.Getenv("SUPABASE_URL"),
		bucket,
	)

	// ambil nama file saja
	fileName := strings.TrimPrefix(fileURL, prefix)

	// endpoint delete Supabase
	deleteURL := fmt.Sprintf(
		"%s/storage/v1/object/%s/%s",
		os.Getenv("SUPABASE_URL"),
		bucket,
		fileName,
	)

	key := os.Getenv("SUPABASE_SECRET_KEY")

	client := resty.New()

	resp, err := client.R().
		SetHeader("Authorization", "Bearer "+key).
		SetHeader("apikey", key).
		Delete(deleteURL)

	if err != nil {
		return err
	}

	if resp.StatusCode() >= 300 {
		return fmt.Errorf(
			"gagal hapus file supabase: %s",
			resp.String(),
		)
	}

	return nil
}
