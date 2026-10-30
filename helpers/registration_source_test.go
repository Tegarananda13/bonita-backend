package helpers_test

import (
	"testing"

	"bonita-backend/helpers"
)

func TestGetRegistrationLabel(t *testing.T) {
	tests := []struct {
		name   string
		source string
		by     string
		want   string
	}{
		{
			name:   "CustomerSource_EmptyBy",
			source: helpers.SourceCustomer,
			by:     "",
			want:   "👤 Customer",
		},
		{
			name:   "CustomerSource_IgnoresBy",
			source: helpers.SourceCustomer,
			by:     "Ahmad",
			want:   "👤 Customer",
		},
		{
			name:   "AdminSource_WithSpecificAdminName",
			source: helpers.SourceAdmin,
			by:     "Budi",
			want:   "🧑‍💼 Budi",
		},
		{
			name:   "AdminSource_FallbackWhenByEmpty",
			source: helpers.SourceAdmin,
			by:     "",
			want:   "🧑‍💼 Admin",
		},
		{
			name:   "ChatbotSource_EmptyBy",
			source: helpers.SourceChatbot,
			by:     "",
			want:   "🤖 AI Chatbot",
		},
		{
			name:   "ChatbotSource_IgnoresBy",
			source: helpers.SourceChatbot,
			by:     "Bot",
			want:   "🤖 AI Chatbot",
		},
		{
			name:   "UnknownSource_EmptyString",
			source: "",
			by:     "",
			want:   "Unknown",
		},
		{
			name:   "UnknownSource_UnrecognizedValue",
			source: "walk_in",
			by:     "Staf",
			want:   "Unknown",
		},
		{
			name:   "UnknownSource_CaseSensitive",
			source: "CUSTOMER",
			by:     "",
			want:   "Unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := helpers.GetRegistrationLabel(tc.source, tc.by)
			if got != tc.want {
				t.Errorf("GetRegistrationLabel(%q, %q) = %q, want %q", tc.source, tc.by, got, tc.want)
			}
		})
	}
}
