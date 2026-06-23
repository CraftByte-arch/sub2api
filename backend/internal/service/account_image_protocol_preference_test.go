package service

import "testing"

func TestAccountOpenAIImageProtocolPreference(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  string
	}{
		{name: "nil extra defaults auto", extra: nil, want: OpenAIImageProtocolPreferenceAuto},
		{name: "images accepted", extra: map[string]any{OpenAIImageProtocolPreferenceExtraKey: "images"}, want: OpenAIImageProtocolPreferenceImages},
		{name: "responses accepted", extra: map[string]any{OpenAIImageProtocolPreferenceExtraKey: "responses"}, want: OpenAIImageProtocolPreferenceResponses},
		{name: "case and whitespace normalized", extra: map[string]any{OpenAIImageProtocolPreferenceExtraKey: " Responses "}, want: OpenAIImageProtocolPreferenceResponses},
		{name: "unknown defaults auto", extra: map[string]any{OpenAIImageProtocolPreferenceExtraKey: "bad"}, want: OpenAIImageProtocolPreferenceAuto},
		{name: "non string defaults auto", extra: map[string]any{OpenAIImageProtocolPreferenceExtraKey: true}, want: OpenAIImageProtocolPreferenceAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (&Account{Extra: tt.extra}).OpenAIImageProtocolPreference()
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
