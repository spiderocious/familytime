package vatar

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The signature, worked out here by hand from the documented recipe rather
// than by calling the code under test.
func TestSignResult_MatchesTheDocumentedRecipe(t *testing.T) {
	body := []byte(`{"winner":"p_ada"}`)
	bodyHash := sha256.Sum256(body)
	signed := "POST\n/api/games/v1/matches/m_1/result\n1760000000\nnonce-abc\n" + hex.EncodeToString(bodyHash[:])
	mac := hmac.New(sha256.New, []byte("results-secret"))
	mac.Write([]byte(signed))
	want := "v1=" + hex.EncodeToString(mac.Sum(nil))

	got := SignResult("results-secret", "POST", "/api/games/v1/matches/m_1/result", "1760000000", "nonce-abc", body)
	if got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	if SignResult("results-secret", "POST", "/api/games/v1/matches/m_1/result", "1760000000", "nonce-abc", []byte(`{"winner":"p_bem"}`)) == want {
		t.Fatal("a different winner must not share a signature")
	}
}

func TestPostResult_SendsWhatVatarWillVerify(t *testing.T) {
	var seen *http.Request
	var seenBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := New(Config{BaseURL: server.URL + "/api/games/v1", Secret: "vtr_test_x", ResultsSecret: "rs"})
	client.now = func() time.Time { return time.Unix(1760000000, 0) }

	if err := client.PostResult(context.Background(), Match{ID: "m_1", Nonce: "n1"}, "p_ada"); err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/api/games/v1/matches/m_1/result" || seen.Header.Get("Authorization") != "Bearer vtr_test_x" {
		t.Fatalf("request = %s auth=%q", seen.URL.Path, seen.Header.Get("Authorization"))
	}
	// Verify as Vatar would: recompute from the bytes that actually arrived.
	want := SignResult("rs", "POST", seen.URL.Path, seen.Header.Get("X-Vatar-Timestamp"), "n1", seenBody)
	if seen.Header.Get("X-Vatar-Signature") != want || seen.Header.Get("X-Vatar-Timestamp") != "1760000000" {
		t.Fatalf("signature header = %q, want %q", seen.Header.Get("X-Vatar-Signature"), want)
	}
	var sent map[string]string
	_ = json.Unmarshal(seenBody, &sent)
	if sent["winner"] != "p_ada" {
		t.Fatalf("body = %s", seenBody)
	}
}

func TestVerifySession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["session"] != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
			return
		}
		_, _ = w.Write([]byte(`{"playerId":"p_1","name":"Ada","gameId":"g","mode":"test"}`))
	}))
	defer server.Close()
	live, _ := New(Config{BaseURL: server.URL, Secret: "s"})

	session, err := live.VerifySession(context.Background(), "good")
	if err != nil || session.PlayerID != "p_1" || session.Name != "Ada" {
		t.Fatalf("session = %+v err=%v", session, err)
	}
	if _, err := live.VerifySession(context.Background(), "forged"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("a rejected token must be ErrInvalidSession, got %v", err)
	}

	cases := []struct {
		name     string
		allowDev bool
		token    string
		wantErr  error
		wantID   string
	}{
		{"dev session accepted when allowed", true, "dev:abc:Ada", nil, "t_abc"},
		{"malformed dev session refused", true, "dev:abc", ErrInvalidSession, ""},
		{"real-looking token refused in dev mode", true, "eyJhbGciOi", ErrInvalidSession, ""},
		{"dev session refused when not allowed", false, "dev:abc:Ada", ErrNotConfigured, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := New(Config{AllowDev: tc.allowDev})
			got, err := client.VerifySession(context.Background(), tc.token)
			if !errors.Is(err, tc.wantErr) || got.PlayerID != tc.wantID {
				t.Fatalf("got %+v err=%v", got, err)
			}
		})
	}
}
