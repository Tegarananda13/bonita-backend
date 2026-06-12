package helpers

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"os"

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