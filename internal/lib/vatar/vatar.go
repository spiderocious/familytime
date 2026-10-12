// Package vatar is the client for the Vatar Games API — the platform that
// hosts our game inside Lagos Life.
//
// It does three things: checks that a session token really came from Vatar,
// reads a match, and reports a match's result. The result is the sensitive
// one: it moves the players' stakes, so it is signed with a second secret that
// never leaves this server.
//
// Reference: https://lagoslife.app/developers/docs
package vatar

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://lagoslife.app/api/games/v1"
	requestTimeout = 8 * time.Second

	// A dev session stands in for a real one when no secret is configured.
	// Format: "dev:<player id>:<display name>". Never accepted in production.
	devSessionPrefix = "dev:"
	ModeTest         = "test"
	ModeLive         = "live"
	ModeDev          = "dev"

	WinnerDraw = "draw"

	MatchPlaying = "playing"

	PurchasePaid      = "paid"
	PurchaseDelivered = "delivered"
	PurchaseRefunded  = "refunded"

	// A dev purchase stands in for a real one when no secret is configured.
	// Format: "dev_<product id>_<anything>". Never accepted in production.
	devPurchasePrefix = "dev_"

	EventPurchasePaid = "purchase.paid"
)

// Error codes Vatar returns that callers branch on.
const (
	ErrCodeAlreadySettled = "already_settled"
)

var (
	// ErrNotConfigured means there is no secret and dev sessions are off.
	ErrNotConfigured = errors.New("vatar: no secret configured")
	// ErrInvalidSession means Vatar refused the token: forged, expired, or for
	// another game.
	ErrInvalidSession = errors.New("vatar: session rejected")
)

// APIError is a non-2xx answer from Vatar.
type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string { return fmt.Sprintf("vatar: %d %s", e.Status, e.Code) }

type Session struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
	GameID   string `json:"gameId"`
	Mode     string `json:"mode"`
}

type MatchPlayer struct {
	PlayerID string `json:"playerId"`
	Name     string `json:"name"`
}

type Match struct {
	ID      string        `json:"id"`
	Status  string        `json:"status"`
	Stake   int           `json:"stake"`
	Seats   int           `json:"seats"`
	Players []MatchPlayer `json:"players"`
	Winner  string        `json:"winner"`
	Nonce   string        `json:"nonce"`
	Mode    string        `json:"mode"`
}

// Purchase is one paid purchase in our game, as Vatar records it. Amount is in
// naira; it is never used to decide what to give.
type Purchase struct {
	ID        string `json:"id"`
	ProductID string `json:"productId"`
	PlayerID  string `json:"playerId"`
	Amount    int    `json:"amount"`
	Status    string `json:"status"`
	Mode      string `json:"mode"`
}

type Config struct {
	BaseURL       string
	Secret        string
	ResultsSecret string
	// AllowDev accepts "dev:" sessions and fakes matches when no secret is
	// set, so the game can be developed and tested outside Lagos Life.
	AllowDev bool
}

type Client struct {
	cfg  Config
	base *url.URL
	http *http.Client
	now  func() time.Time
}

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("vatar base url: %w", err)
	}
	return &Client{cfg: cfg, base: base, http: &http.Client{Timeout: requestTimeout}, now: time.Now}, nil
}

// Configured reports whether real Vatar calls can be made.
func (c *Client) Configured() bool { return c.cfg.Secret != "" }

// DevMode reports whether dev sessions are being accepted in place of real ones.
func (c *Client) DevMode() bool { return !c.Configured() && c.cfg.AllowDev }

// VerifySession asks Vatar who a session token belongs to. The token is the
// only proof of identity the game ever has, so nothing is done for a player
// before this succeeds.
func (c *Client) VerifySession(ctx context.Context, token string) (Session, error) {
	if !c.Configured() {
		if !c.cfg.AllowDev {
			return Session{}, ErrNotConfigured
		}
		rest, ok := strings.CutPrefix(token, devSessionPrefix)
		id, name, found := strings.Cut(rest, ":")
		if !ok || !found || id == "" || name == "" {
			return Session{}, ErrInvalidSession
		}
		return Session{PlayerID: "t_" + id, Name: name, Mode: ModeDev}, nil
	}

	var session Session
	err := c.call(ctx, http.MethodPost, "/session/verify", map[string]string{"session": token}, nil, &session)
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden) {
		return Session{}, ErrInvalidSession
	}
	return session, err
}

// GetMatch reads a match, including the nonce its result must be signed with.
func (c *Client) GetMatch(ctx context.Context, matchID string) (Match, error) {
	if !c.Configured() {
		return Match{}, ErrNotConfigured
	}
	var match Match
	err := c.call(ctx, http.MethodGet, "/matches/"+url.PathEscape(matchID), nil, nil, &match)
	return match, err
}

// GetPurchase asks Vatar whether a purchase is real. An unpaid, unknown or
// other game's purchase comes back as a 404 APIError.
func (c *Client) GetPurchase(ctx context.Context, purchaseID string) (Purchase, error) {
	if !c.Configured() {
		if !c.cfg.AllowDev {
			return Purchase{}, ErrNotConfigured
		}
		rest, ok := strings.CutPrefix(purchaseID, devPurchasePrefix)
		cut := strings.LastIndex(rest, "_")
		if !ok || cut <= 0 {
			return Purchase{}, &APIError{Status: http.StatusNotFound, Code: "not_found"}
		}
		return Purchase{ID: purchaseID, ProductID: rest[:cut], Status: PurchasePaid, Mode: ModeDev}, nil
	}
	var purchase Purchase
	err := c.call(ctx, http.MethodGet, "/purchases/"+url.PathEscape(purchaseID), nil, nil, &purchase)
	return purchase, err
}

// DeliverPurchase tells Vatar the item has been given. Safe to repeat.
func (c *Client) DeliverPurchase(ctx context.Context, purchaseID string) error {
	if !c.Configured() {
		if c.cfg.AllowDev {
			return nil
		}
		return ErrNotConfigured
	}
	return c.call(ctx, http.MethodPost, "/purchases/"+url.PathEscape(purchaseID)+"/deliver", map[string]string{}, nil, nil)
}

// WebhookSigned checks a webhook's X-Vatar-Signature header: "sha256=" and
// the hex HMAC-SHA256 of the raw body, keyed with the webhook signing secret.
// Compared in constant time. An empty secret never verifies.
func WebhookSigned(webhookSecret string, rawBody []byte, header string) bool {
	if webhookSecret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(rawBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(header))
}

// PostResult reports who won. winner is a Vatar player id, or WinnerDraw.
//
// Vatar settles a match once. Sending the same result again is harmless; a
// DIFFERENT winner is refused with already_settled. So this is safe to retry.
func (c *Client) PostResult(ctx context.Context, match Match, winner string) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	if c.cfg.ResultsSecret == "" {
		return errors.New("vatar: no results secret configured")
	}
	body, err := json.Marshal(map[string]string{"winner": winner})
	if err != nil {
		return err
	}
	path := "/matches/" + url.PathEscape(match.ID) + "/result"
	timestamp := strconv.FormatInt(c.now().Unix(), 10)
	headers := map[string]string{
		"X-Vatar-Timestamp": timestamp,
		"X-Vatar-Signature": SignResult(c.cfg.ResultsSecret, http.MethodPost, c.base.Path+path, timestamp, match.Nonce, body),
	}
	return c.call(ctx, http.MethodPost, path, body, headers, nil)
}

// SignResult builds the X-Vatar-Signature header value for a match result:
// HMAC-SHA256, keyed with the results secret, over five newline-joined fields
// — method, full path, timestamp, the match's nonce, and the hex SHA-256 of
// the exact body bytes.
func SignResult(resultsSecret, method, fullPath, timestamp, nonce string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(resultsSecret))
	mac.Write([]byte(strings.Join([]string{method, fullPath, timestamp, nonce, hex.EncodeToString(bodyHash[:])}, "\n")))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// call makes one request. body may be a value to encode or raw bytes that
// must be sent exactly as they were signed.
func (c *Client) call(ctx context.Context, method, path string, body any, headers map[string]string, out any) error {
	var payload []byte
	switch b := body.(type) {
	case nil:
	case []byte:
		payload = b
	default:
		encoded, err := json.Marshal(b)
		if err != nil {
			return err
		}
		payload = encoded
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Secret)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	if res.StatusCode < 200 || res.StatusCode > 299 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		return &APIError{Status: res.StatusCode, Code: failure.Error}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}
