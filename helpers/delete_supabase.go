package helpers

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-resty/resty/v2"
)

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