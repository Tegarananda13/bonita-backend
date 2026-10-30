package helpers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"bonita-backend/config"

	"github.com/DATA-DOG/go-sqlmock"
	"google.golang.org/genai"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func setupMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}

	dialector := postgres.New(postgres.Config{
		Conn:       sqlDB,
		DriverName: "postgres",
	})

	gdb, err := gorm.Open(dialector, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatalf("failed to open gorm DB with sqlmock: %v", err)
	}

	cleanup := func() {
		_ = sqlDB.Close()
	}

	return gdb, mock, cleanup
}

func TestAskGemini_WithoutValidAPIKey(t *testing.T) {
	prevKey := os.Getenv("GEMINI_API_KEY")
	_ = os.Setenv("GEMINI_API_KEY", "")
	defer func() {
		_ = os.Setenv("GEMINI_API_KEY", prevKey)
	}()

	resp, err := AskGemini("Apa saja dokumen wajib pendaftaran umroh?")
	if err == nil {
		t.Fatal("Expected error when GEMINI_API_KEY is empty, got nil")
	}
	if resp != "" {
		t.Errorf("Expected empty response on error, got %q", resp)
	}
}

func TestAskGemini_ClientInitFailure(t *testing.T) {
	prevKey := os.Getenv("GEMINI_API_KEY")
	_ = os.Setenv("GEMINI_API_KEY", "dummy-api-key")
	defer func() {
		_ = os.Setenv("GEMINI_API_KEY", prevKey)
	}()

	prevNewClient := newGenAIClient
	newGenAIClient = func(ctx context.Context, cfg *genai.ClientConfig) (*genai.Client, error) {
		return nil, errors.New("simulated genai client initialization failure")
	}
	defer func() { newGenAIClient = prevNewClient }()

	resp, err := AskGemini("Berapa harga paket umroh?")
	if err == nil {
		t.Fatal("Expected error on client creation failure, got nil")
	}
	if resp != "" {
		t.Errorf("Expected empty response on error, got %q", resp)
	}
	if !strings.Contains(err.Error(), "simulated genai client initialization failure") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestAskGemini_Success(t *testing.T) {
	prevDB := config.DB
	gdb, mock, cleanup := setupMockDB(t)
	defer cleanup()
	config.DB = gdb
	defer func() { config.DB = prevDB }()

	// Mock query paket umroh untuk BuildBonitaContext()
	rows := sqlmock.NewRows([]string{
		"id", "nama_paket", "jenis_paket", "harga", "tanggal_berangkat",
		"durasi", "hotel_mekkah", "hotel_madinah", "maskapai", "kuota_max",
		"kuota_terpakai", "deskripsi", "is_active", "is_finished",
	}).AddRow(
		"a0000000-0000-0000-0000-000000000001", "Paket Ramadhan Berkah", "reguler", 35000000.0, time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
		12, "Hotel Hilton", "Hotel Pullman", "Garuda Indonesia", 40,
		10, "Paket umroh reguler terbaik", true, false,
	)
	mock.ExpectQuery(`SELECT \* FROM "paket_umroh"`).
		WillReturnRows(rows)

	prevKey := os.Getenv("GEMINI_API_KEY")
	_ = os.Setenv("GEMINI_API_KEY", "valid-test-key")
	defer func() {
		_ = os.Setenv("GEMINI_API_KEY", prevKey)
	}()

	expectedAnswer := "Untuk mendaftar umroh, Anda memerlukan KTP, KK, dan Paspor."
	var capturedPrompt string

	prevNewClient := newGenAIClient
	newGenAIClient = func(ctx context.Context, cfg *genai.ClientConfig) (*genai.Client, error) {
		mockHTTPClient := &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				bodyBytes, _ := io.ReadAll(req.Body)
				capturedPrompt = string(bodyBytes)

				jsonResp := `{
					"candidates": [
						{
							"content": {
								"role": "model",
								"parts": [
									{
										"text": "` + expectedAnswer + `"
									}
								]
							},
							"finishReason": "STOP"
						}
					]
				}`

				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(bytes.NewBufferString(jsonResp)),
				}, nil
			}),
		}

		cfg.HTTPClient = mockHTTPClient
		return genai.NewClient(ctx, cfg)
	}
	defer func() { newGenAIClient = prevNewClient }()

	question := "Bagaimana alur pendaftaran jamaah baru?"
	resp, err := AskGemini(question)

	if err != nil {
		t.Fatalf("Unexpected error from AskGemini: %v", err)
	}

	if resp != expectedAnswer {
		t.Errorf("AskGemini response mismatch: got %q, want %q", resp, expectedAnswer)
	}

	// Verifikasi bahwa captured prompt memuat BasePrompt, Context dari DB, dan pertanyaan
	if !strings.Contains(capturedPrompt, question) {
		t.Errorf("Captured prompt does not contain question %q", question)
	}
	if !strings.Contains(capturedPrompt, "Kamu adalah Bonita Assistant") {
		t.Errorf("Captured prompt does not contain BasePrompt content")
	}
	if !strings.Contains(capturedPrompt, "Paket Ramadhan Berkah") {
		t.Errorf("Captured prompt does not contain dynamic package context")
	}
}

func TestAskGemini_GenerateContentError(t *testing.T) {
	prevDB := config.DB
	gdb, mock, cleanup := setupMockDB(t)
	defer cleanup()
	config.DB = gdb
	defer func() { config.DB = prevDB }()

	mock.ExpectQuery(`SELECT \* FROM "paket_umroh"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	prevKey := os.Getenv("GEMINI_API_KEY")
	_ = os.Setenv("GEMINI_API_KEY", "valid-test-key")
	defer func() {
		_ = os.Setenv("GEMINI_API_KEY", prevKey)
	}()

	prevNewClient := newGenAIClient
	newGenAIClient = func(ctx context.Context, cfg *genai.ClientConfig) (*genai.Client, error) {
		mockHTTPClient := &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Status:     "500 Internal Server Error",
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(bytes.NewBufferString(`{
						"error": {
							"code": 500,
							"message": "Gemini backend unavailable",
							"status": "INTERNAL"
						}
					}`)),
				}, nil
			}),
		}

		cfg.HTTPClient = mockHTTPClient
		return genai.NewClient(ctx, cfg)
	}
	defer func() { newGenAIClient = prevNewClient }()

	resp, err := AskGemini("Kapan jadwal keberangkatan terdekat?")
	if err == nil {
		t.Fatal("Expected error when Gemini backend returns 500, got nil")
	}
	if resp != "" {
		t.Errorf("Expected empty response on error, got %q", resp)
	}
}
