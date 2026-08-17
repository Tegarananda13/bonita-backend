package helpers

// Konstanta registration source
const (
	SourceCustomer = "customer"
	SourceAdmin    = "admin"
	SourceChatbot  = "chatbot"
)

// GetRegistrationLabel menghasilkan label yang human-readable untuk ditampilkan di frontend.
//
// Contoh:
//
//	source="customer"           → "👤 Customer"
//	source="admin", by="Budi"   → "🧑‍💼 Budi"
//	source="chatbot"            → "🤖 AI Chatbot"
func GetRegistrationLabel(source string, by string) string {
	switch source {
	case SourceCustomer:
		return "👤 Customer"
	case SourceAdmin:
		if by != "" {
			return "🧑‍💼 " + by
		}
		return "🧑‍💼 Admin"
	case SourceChatbot:
		return "🤖 AI Chatbot"
	default:
		return "Unknown"
	}
}
