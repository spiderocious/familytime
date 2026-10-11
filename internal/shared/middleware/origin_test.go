package middleware

import "testing"

func TestOriginPolicy(t *testing.T) {
	listed := []string{"https://play.familytime.app"}
	cases := []struct {
		name         string
		origin       string
		allowPrivate bool
		want         bool
	}{
		{"listed origin, production", "https://play.familytime.app", false, true},
		{"unlisted public origin, production", "https://evil.example", false, false},
		{"lan address, production", "http://192.168.1.20:5173", false, false},
		{"lan address, development", "http://192.168.1.20:5173", true, true},
		{"ten-dot address, development", "http://10.0.0.5:5173", true, true},
		{"localhost, development", "http://localhost:5173", true, true},
		{"mdns name, development", "http://macbook.local:5173", true, true},
		{"public address, development", "http://8.8.8.8:5173", true, false},
		{"public name, development", "https://evil.example", true, false},
		{"garbage, development", "://nope", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewOriginPolicy(listed, tc.allowPrivate).Allows(tc.origin); got != tc.want {
				t.Fatalf("Allows(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}
