package moderation

import "testing"

func TestVerdictRejection(t *testing.T) {
	tests := []struct {
		name    string
		verdict Verdict
		want    bool
	}{
		{"safe", Verdict{NSFL: 0.01, NSFW: 0.02, SFW: 0.97}, false},
		{"nsfw", Verdict{NSFL: 0.01, NSFW: 0.91, SFW: 0.08}, true},
		{"gore", Verdict{NSFL: 0.91, NSFW: 0.01, SFW: 0.08}, true},
		{"near miss", Verdict{NSFL: 0.59, NSFW: 0.59, SFW: 0.01}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.verdict.Rejection(0.60, 0.60) != ""; got != tt.want {
				t.Fatalf("Rejection() blocked = %v, want %v", got, tt.want)
			}
		})
	}
}
