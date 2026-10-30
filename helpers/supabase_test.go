package helpers_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"bonita-backend/helpers"
)

type memoryMultipartFile struct {
	*bytes.Reader
}

func (m *memoryMultipartFile) Close() error {
	return nil
}

type failingMultipartFile struct{}

func (failingMultipartFile) Read(p []byte) (int, error) {
	return 0, errors.New("simulated file read error")
}
func (failingMultipartFile) ReadAt(p []byte, off int64) (int, error) {
	return 0, errors.New("simulated file readAt error")
}
func (failingMultipartFile) Seek(offset int64, whence int) (int64, error) {
	return 0, errors.New("simulated seek error")
}
func (failingMultipartFile) Close() error {
	return nil
}

func TestUploadToSupabase(t *testing.T) {
	t.Run("Error_FileReadFailure", func(t *testing.T) {
		url, err := helpers.UploadToSupabase(failingMultipartFile{}, "ktp.jpg", "dokumen")
		if err == nil {
			t.Fatal("Expected error when file read fails, got nil")
		}
		if url != "" {
			t.Errorf("Expected empty URL on error, got %q", url)
		}
	})

	t.Run("Error_NetworkRequestFailure", func(t *testing.T) {
		prevURL := os.Getenv("SUPABASE_URL")
		prevKey := os.Getenv("SUPABASE_SECRET_KEY")
		defer func() {
			_ = os.Setenv("SUPABASE_URL", prevURL)
			_ = os.Setenv("SUPABASE_SECRET_KEY", prevKey)
		}()

		// Invalid URL scheme/host triggers resty client error immediately without external network call
		_ = os.Setenv("SUPABASE_URL", "http://127.0.0.1:1")
		_ = os.Setenv("SUPABASE_SECRET_KEY", "test-key")

		file := &memoryMultipartFile{Reader: bytes.NewReader([]byte("dummy-bytes"))}
		url, err := helpers.UploadToSupabase(file, "paspor.jpg", "dokumen")
		if err == nil {
			t.Fatal("Expected network error when Supabase endpoint is unreachable")
		}
		if url != "" {
			t.Errorf("Expected empty URL on network error, got %q", url)
		}
	})

	t.Run("Success_200OK_And_HTTPErrorStatus", func(t *testing.T) {
		var receivedMethod, receivedPath, receivedAuth, receivedAPIKey string
		var receivedBody []byte
		statusCodeToReturn := http.StatusOK

		var ts *httptest.Server
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Skipf("Skipping httptest.NewServer test in restricted socket sandbox: %v", r)
				}
			}()
			ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedMethod = r.Method
				receivedPath = r.URL.Path
				receivedAuth = r.Header.Get("Authorization")
				receivedAPIKey = r.Header.Get("apikey")
				receivedBody, _ = io.ReadAll(r.Body)

				w.WriteHeader(statusCodeToReturn)
				if statusCodeToReturn >= 300 {
					_, _ = w.Write([]byte(`{"error":"bucket not found"}`))
				} else {
					_, _ = w.Write([]byte(`{"Key":"dokumen/paspor-001.jpg"}`))
				}
			}))
		}()
		if ts == nil {
			return
		}
		defer ts.Close()

		prevURL := os.Getenv("SUPABASE_URL")
		prevKey := os.Getenv("SUPABASE_SECRET_KEY")
		defer func() {
			_ = os.Setenv("SUPABASE_URL", prevURL)
			_ = os.Setenv("SUPABASE_SECRET_KEY", prevKey)
		}()

		_ = os.Setenv("SUPABASE_URL", ts.URL)
		_ = os.Setenv("SUPABASE_SECRET_KEY", "mock-secret-token")

		// 1. Sukses upload (HTTP 200)
		fileContent := []byte("isi-file-paspor")
		file := &memoryMultipartFile{Reader: bytes.NewReader(fileContent)}
		publicURL, err := helpers.UploadToSupabase(file, "paspor-001.jpg", "dokumen")
		if err != nil {
			t.Fatalf("Unexpected error on UploadToSupabase: %v", err)
		}

		expectedPublicURL := ts.URL + "/storage/v1/object/public/dokumen/paspor-001.jpg"
		if publicURL != expectedPublicURL {
			t.Errorf("Public URL = %q, want %q", publicURL, expectedPublicURL)
		}
		if receivedMethod != http.MethodPut {
			t.Errorf("Expected PUT method, got %s", receivedMethod)
		}
		if receivedPath != "/storage/v1/object/dokumen/paspor-001.jpg" {
			t.Errorf("Unexpected request path: %s", receivedPath)
		}
		if receivedAuth != "Bearer mock-secret-token" || receivedAPIKey != "mock-secret-token" {
			t.Errorf("Unexpected auth headers: Authorization=%q, apikey=%q", receivedAuth, receivedAPIKey)
		}
		if !bytes.Equal(receivedBody, fileContent) {
			t.Errorf("Uploaded body mismatch: got %q, want %q", string(receivedBody), string(fileContent))
		}

		// 2. Gagal upload (HTTP >= 300)
		statusCodeToReturn = http.StatusBadRequest
		file2 := &memoryMultipartFile{Reader: bytes.NewReader(fileContent)}
		failURL, err := helpers.UploadToSupabase(file2, "paspor-001.jpg", "dokumen")
		if err == nil {
			t.Fatal("Expected error when Supabase returns HTTP 400")
		}
		if !strings.Contains(err.Error(), "supabase error:") {
			t.Errorf("Expected 'supabase error:' prefix, got %v", err)
		}
		if failURL != "" {
			t.Errorf("Expected empty URL on HTTP error, got %q", failURL)
		}
	})
}

func TestDeleteFromSupabase(t *testing.T) {
	t.Run("EmptyFileURL_ReturnsNilImmediately", func(t *testing.T) {
		if err := helpers.DeleteFromSupabase("", "dokumen"); err != nil {
			t.Errorf("Expected nil error for empty fileURL, got %v", err)
		}
	})

	t.Run("Error_NetworkRequestFailure", func(t *testing.T) {
		prevURL := os.Getenv("SUPABASE_URL")
		prevKey := os.Getenv("SUPABASE_SECRET_KEY")
		defer func() {
			_ = os.Setenv("SUPABASE_URL", prevURL)
			_ = os.Setenv("SUPABASE_SECRET_KEY", prevKey)
		}()

		_ = os.Setenv("SUPABASE_URL", "http://127.0.0.1:1")
		_ = os.Setenv("SUPABASE_SECRET_KEY", "test-key")

		err := helpers.DeleteFromSupabase("http://127.0.0.1:1/storage/v1/object/public/paket/foto.jpg", "paket")
		if err == nil {
			t.Fatal("Expected error when Supabase delete endpoint is unreachable")
		}
	})

	t.Run("Success_200OK_And_HTTPErrorStatus", func(t *testing.T) {
		var deletedMethod, deletedPath string
		statusCodeToReturn := http.StatusOK

		var ts *httptest.Server
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Skipf("Skipping httptest.NewServer test in restricted socket sandbox: %v", r)
				}
			}()
			ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deletedMethod = r.Method
				deletedPath = r.URL.Path
				w.WriteHeader(statusCodeToReturn)
				if statusCodeToReturn >= 300 {
					_, _ = w.Write([]byte(`{"error":"delete forbidden"}`))
				} else {
					_, _ = w.Write([]byte(`{"message":"deleted"}`))
				}
			}))
		}()
		if ts == nil {
			return
		}
		defer ts.Close()

		prevURL := os.Getenv("SUPABASE_URL")
		prevKey := os.Getenv("SUPABASE_SECRET_KEY")
		defer func() {
			_ = os.Setenv("SUPABASE_URL", prevURL)
			_ = os.Setenv("SUPABASE_SECRET_KEY", prevKey)
		}()

		_ = os.Setenv("SUPABASE_URL", ts.URL)
		_ = os.Setenv("SUPABASE_SECRET_KEY", "mock-secret-token")

		publicFileURL := ts.URL + "/storage/v1/object/public/paket/foto-umroh.jpg"

		// 1. Sukses hapus (HTTP 200)
		if err := helpers.DeleteFromSupabase(publicFileURL, "paket"); err != nil {
			t.Fatalf("Unexpected error on DeleteFromSupabase: %v", err)
		}
		if deletedMethod != http.MethodDelete {
			t.Errorf("Expected DELETE method, got %s", deletedMethod)
		}
		if deletedPath != "/storage/v1/object/paket/foto-umroh.jpg" {
			t.Errorf("Unexpected delete path: got %q, want %q", deletedPath, "/storage/v1/object/paket/foto-umroh.jpg")
		}

		// 2. Gagal hapus (HTTP >= 300)
		statusCodeToReturn = http.StatusInternalServerError
		err := helpers.DeleteFromSupabase(publicFileURL, "paket")
		if err == nil {
			t.Fatal("Expected error when Supabase DELETE returns HTTP 500")
		}
		if !strings.Contains(err.Error(), "gagal hapus file supabase:") {
			t.Errorf("Expected 'gagal hapus file supabase:' in error, got %v", err)
		}
	})
}
