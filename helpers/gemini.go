package helpers

import (
	"context"
	"os"

	"bonita-backend/services"

	"google.golang.org/genai"
)

// AskGemini mengirimkan pertanyaan ke Gemini API.
//
// Fungsi ini hanya bertanggung jawab pada proses pengiriman prompt ke Gemini.
// Seluruh konteks informasi Bonita (paket, FAQ, alur bisnis) dibangun oleh
// services.BuildBonitaContext() sehingga prompt selalu menggunakan data terkini
// dari database.
//
// Untuk mengubah sumber data atau beralih ke RAG, cukup ubah BuildBonitaContext()
// di services/prompt_builder.go tanpa menyentuh file ini.
func AskGemini(question string) (string, error) {

	// ambil API Key dari environment
	apiKey := os.Getenv("GEMINI_API_KEY")

	// buat client Gemini
	client, err := genai.NewClient(
		context.Background(),
		&genai.ClientConfig{
			APIKey: apiKey,
		},
	)

	if err != nil {
		return "", err
	}

	// Bangun prompt:
	//   1. Base Prompt  — aturan tetap chatbot
	//   2. Dynamic Context — data terbaru dari database (paket, FAQ, alur bisnis)
	//   3. Pertanyaan customer
	bonitaContext := services.BuildBonitaContext()

	prompt := services.BasePrompt +
		bonitaContext +
		"Pertanyaan Customer:\n" +
		question

	// kirim ke Gemini
	result, err := client.Models.GenerateContent(
		context.Background(),
		"gemini-2.5-flash",
		genai.Text(prompt),
		nil,
	)

	if err != nil {
		return "", err
	}

	return result.Text(), nil
}