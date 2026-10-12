package app_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"familytime/backend/internal/app"
	"familytime/backend/internal/env"
	"familytime/backend/internal/features/platform"
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/features/results"
	"familytime/backend/internal/games/quiz"
	"familytime/backend/internal/lib/vatar"
)

// ---- harness ---------------------------------------------------------------

type alwaysUp struct{}

func (alwaysUp) Ping(context.Context) bool { return true }

type memoryStore struct {
	mu   sync.Mutex
	rows []results.GameResult
}

func (m *memoryStore) Insert(_ context.Context, r results.GameResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, r)
	return nil
}

func (m *memoryStore) ListBefore(context.Context, string, int) ([]results.GameResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]results.GameResult{}, m.rows...), nil
}

func (m *memoryStore) snapshot() []results.GameResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]results.GameResult{}, m.rows...)
}

const webhookSecret = "whsec-for-tests"

func boot(t *testing.T) (*httptest.Server, *memoryStore) {
	return bootWith(t, nil)
}

func bootWith(t *testing.T, platformClient platform.Platform) (*httptest.Server, *memoryStore) {
	t.Helper()
	bank := questions.NewBank()
	all := []questions.Question{}
	for i := 0; i < 12; i++ {
		all = append(all, questions.Question{
			ID: fmt.Sprintf("q_%d", i), Category: questions.CategoryGeneral, Difficulty: questions.DifficultyEasy,
			Prompt: fmt.Sprintf("Question %d?", i), Correct: fmt.Sprintf("right-%d", i),
			Wrong: []string{fmt.Sprintf("a-%d", i), fmt.Sprintf("b-%d", i), fmt.Sprintf("c-%d", i)},
		})
	}
	bank.Replace(all)

	store := &memoryStore{}
	application := app.Build(app.Deps{
		Env:      env.Env{AppEnv: env.EnvTest, AppURL: "http://web.test", CORSOrigins: []string{"http://web.test"}, VatarWebhookSecret: webhookSecret},
		DB:       alwaysUp{},
		Bank:     bank,
		Results:  results.NewService(store),
		Platform: platformClient,
	})
	server := httptest.NewServer(application.Engine)
	t.Cleanup(server.Close)
	return server, store
}

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code        string              `json:"code"`
		Message     string              `json:"message"`
		Severity    int                 `json:"severity"`
		FieldErrors map[string][]string `json:"field_errors"`
	} `json:"error"`
}

func call(t *testing.T, method, url string, body string) (int, envelope) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var env envelope
	_ = json.NewDecoder(res.Body).Decode(&env)
	return res.StatusCode, env
}

type wsMessage struct {
	Type  string `json:"type"`
	State *struct {
		Now  int64 `json:"now"`
		Room struct {
			Phase   string `json:"phase"`
			Players []struct {
				Name      string `json:"name"`
				Connected bool   `json:"connected"`
			} `json:"players"`
			StarterID  string `json:"starter_id"`
			MinToStart int    `json:"min_to_start"`
		} `json:"room"`
		Game *quiz.View `json:"game"`
	} `json:"state"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	raw string
}

type socket struct {
	t    *testing.T
	conn *websocket.Conn
}

func dial(t *testing.T, server *httptest.Server, code, role, token string) *socket {
	t.Helper()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + fmt.Sprintf("/ws?code=%s&role=%s&token=%s", code, role, token)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", role, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &socket{t: t, conn: conn}
}

func (s *socket) send(msgType string, payload any) {
	s.t.Helper()
	if err := s.conn.WriteJSON(map[string]any{"type": msgType, "payload": payload}); err != nil {
		s.t.Fatal(err)
	}
}

// until reads messages until one satisfies want, failing after a deadline.
func (s *socket) until(what string, want func(wsMessage) bool) wsMessage {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_ = s.conn.SetReadDeadline(deadline)
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			s.t.Fatalf("waiting for %s: %v", what, err)
		}
		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.t.Fatalf("bad message %s: %v", raw, err)
		}
		msg.raw = string(raw)
		if want(msg) {
			return msg
		}
	}
}

func inPhase(phase quiz.Phase) func(wsMessage) bool {
	return func(m wsMessage) bool { return m.State != nil && m.State.Game != nil && m.State.Game.Phase == phase }
}

func rightIndex(view *quiz.View) int {
	for i, option := range view.Question.Options {
		if strings.HasPrefix(option, "right-") {
			return i
		}
	}
	return -1
}

// ---- tests -----------------------------------------------------------------

func TestREST_EnvelopeAndErrors(t *testing.T) {
	server, _ := boot(t)
	api := server.URL + "/api/v1"

	status, env := call(t, http.MethodGet, server.URL+"/health", "")
	if status != 200 || env.Error != nil {
		t.Fatalf("health: %d", status)
	}

	status, env = call(t, http.MethodGet, api+"/games", "")
	var games []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(env.Data, &games)
	if status != 200 || len(games) != 1 || games[0].ID != quiz.GameID {
		t.Fatalf("games: %d %s", status, env.Data)
	}

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{"unknown path uses the envelope", http.MethodGet, "/nope", "", 404, "not_found", ""},
		{"unknown game options", http.MethodGet, "/games/chess/options", "", 404, "game_not_found", ""},
		{"unknown room", http.MethodGet, "/rooms/ZZZZZ", "", 404, "room_not_found", ""},
		{"malformed json is a client error", http.MethodPost, "/rooms", "{not json", 400, "malformed_json", ""},
		{"missing game id", http.MethodPost, "/rooms", `{}`, 422, "validation_error", "game_id"},
		{"unknown game", http.MethodPost, "/rooms", `{"game_id":"chess"}`, 404, "game_not_found", ""},
		{"bad config", http.MethodPost, "/rooms", `{"game_id":"quiz","config":{"rounds":99}}`, 422, "validation_error", "rounds"},
		{"join unknown room", http.MethodPost, "/rooms/ZZZZZ/players", `{"name":"Ada"}`, 404, "room_not_found", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, env := call(t, tc.method, api+tc.path, tc.body)
			if status != tc.wantStatus || env.Error == nil || env.Error.Code != tc.wantCode {
				t.Fatalf("got %d %+v, want %d %s", status, env.Error, tc.wantStatus, tc.wantCode)
			}
			if env.Error.Message == "" || env.Error.Severity == 0 {
				t.Fatal("every error must resolve a message and a severity")
			}
			if tc.wantField != "" && len(env.Error.FieldErrors[tc.wantField]) == 0 {
				t.Fatalf("no field error for %s: %+v", tc.wantField, env.Error.FieldErrors)
			}
		})
	}
}

func TestFullGame_OverRESTAndWebsocket(t *testing.T) {
	server, store := boot(t)
	api := server.URL + "/api/v1"

	// Host opens a room: one round of three questions, host-paced.
	status, env := call(t, http.MethodPost, api+"/rooms", `{"game_id":"quiz","config":{"rounds":1,"questions_per_round":3,"seconds_per_question":30,"pacing":"host"}}`)
	var room struct {
		Code      string `json:"code"`
		HostToken string `json:"host_token"`
		JoinURL   string `json:"join_url"`
	}
	_ = json.Unmarshal(env.Data, &room)
	if status != 201 || len(room.Code) != 5 || room.HostToken == "" || room.JoinURL != "http://web.test/join/"+room.Code {
		t.Fatalf("create room: %d %s", status, env.Data)
	}

	join := func(name string) (string, string) {
		status, env := call(t, http.MethodPost, api+"/rooms/"+strings.ToLower(room.Code)+"/players", fmt.Sprintf(`{"name":%q,"avatar":"🐼"}`, name))
		var joined struct {
			PlayerID    string `json:"player_id"`
			PlayerToken string `json:"player_token"`
		}
		_ = json.Unmarshal(env.Data, &joined)
		if status != 201 || joined.PlayerToken == "" {
			t.Fatalf("join %s: %d %+v", name, status, env.Error)
		}
		return joined.PlayerID, joined.PlayerToken
	}
	adaID, adaToken := join("Ada")
	_, bemToken := join("  Bem  ")

	// Names are unique whatever the case, and validated.
	if status, env := call(t, http.MethodPost, api+"/rooms/"+room.Code+"/players", `{"name":"ada"}`); status != 409 || env.Error.Code != "name_taken" {
		t.Fatalf("duplicate name: %d %+v", status, env.Error)
	}
	if status, env := call(t, http.MethodPost, api+"/rooms/"+room.Code+"/players", `{"name":"   ","avatar":"💩"}`); status != 422 || len(env.Error.FieldErrors) != 2 {
		t.Fatalf("invalid join: %d %+v", status, env.Error)
	}

	// A bad token is refused before the socket ever opens.
	badURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?code=" + room.Code + "&role=host&token=wrong"
	if _, res, err := websocket.DefaultDialer.Dial(badURL, nil); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatal("a wrong host token must be rejected with 401")
	}

	hostWS := dial(t, server, room.Code, "host", room.HostToken)
	ada := dial(t, server, room.Code, "player", adaToken)
	bem := dial(t, server, room.Code, "player", bemToken)

	lobby := hostWS.until("both players connected", func(m wsMessage) bool {
		if m.State == nil || len(m.State.Room.Players) != 2 {
			return false
		}
		return m.State.Room.Players[0].Connected && m.State.Room.Players[1].Connected
	})
	if lobby.State.Room.Phase != "lobby" || lobby.State.Room.Players[1].Name != "Bem" {
		t.Fatalf("lobby = %+v", lobby.State.Room)
	}

	// Only the host may start.
	ada.send("host.start", nil)
	if msg := ada.until("not_host error", func(m wsMessage) bool { return m.Error != nil }); msg.Error.Code != "not_host" {
		t.Fatalf("player start: %+v", msg.Error)
	}

	hostWS.send("host.start", nil)
	hostWS.until("intro", inPhase(quiz.PhaseIntro))

	for q := 1; q <= 3; q++ {
		hostWS.send("host.next", nil) // intro or leaderboard → question
		asked := ada.until("question", func(m wsMessage) bool { return inPhase(quiz.PhaseQuestion)(m) && m.State.Game.QuestionSeq == q })

		// Answer secrecy on the wire, not just in the struct.
		if strings.Contains(asked.raw, "answer_idx") {
			t.Fatal("the answer was sent to a player while the question was open")
		}
		bemAsked := bem.until("question", func(m wsMessage) bool { return inPhase(quiz.PhaseQuestion)(m) && m.State.Game.QuestionSeq == q })

		right := rightIndex(asked.State.Game)
		ada.send("quiz.answer", map[string]int{"question_seq": q, "choice": right})
		bem.send("quiz.answer", map[string]int{"question_seq": q, "choice": (rightIndex(bemAsked.State.Game) + 1) % 4})

		reveal := hostWS.until("reveal", func(m wsMessage) bool { return inPhase(quiz.PhaseReveal)(m) && m.State.Game.QuestionSeq == q })
		game := reveal.State.Game
		if game.Reveal == nil || game.Reveal.AnswerIdx != right {
			t.Fatalf("q%d reveal = %+v, want answer %d", q, game.Reveal, right)
		}
		if game.Board[0].Name != "Ada" || game.Board[0].Result != quiz.ResultCorrect || game.Board[1].Result != quiz.ResultWrong {
			t.Fatalf("q%d board = %+v", q, game.Board)
		}
		hostWS.send("host.next", nil) // reveal → leaderboard, or round end after the last
	}

	hostWS.until("round end", inPhase(quiz.PhaseRoundEnd))
	hostWS.send("host.next", nil)
	finished := ada.until("finished", inPhase(quiz.PhaseFinished))

	final := finished.State.Game.Final
	if final == nil || final.TotalQuestions != 3 || final.RoundsPlayed != 1 {
		t.Fatalf("final = %+v", final)
	}
	if final.Stats[0].PlayerID != adaID || final.Stats[0].Correct != 3 || final.Stats[0].AccuracyPct != 100 || final.Stats[0].BestStreak != 3 {
		t.Fatalf("winner stats = %+v", final.Stats[0])
	}
	if final.Stats[1].Score != 0 || final.Stats[1].Answered != 3 {
		t.Fatalf("second place stats = %+v", final.Stats[1])
	}
	if finished.State.Game.You.Rank != 1 || finished.State.Game.You.Score != final.Stats[0].Score {
		t.Fatalf("Ada's own view = %+v", finished.State.Game.You)
	}

	// The finished game is recorded (write-behind, so allow it a moment).
	var recorded []results.GameResult
	for i := 0; i < 50 && len(recorded) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		recorded = store.snapshot()
	}
	if len(recorded) != 1 || recorded[0].RoomCode != room.Code || recorded[0].TotalQuestions != 3 || recorded[0].Players[0].Name != "Ada" {
		t.Fatalf("recorded = %+v", recorded)
	}

	// Play again starts a fresh game in the same room; scores reset.
	hostWS.send("host.play_again", nil)
	again := bem.until("second game intro", inPhase(quiz.PhaseIntro))
	if again.State.Game.Board[0].Score != 0 {
		t.Fatal("a new game must start from zero")
	}

	// Closing the room tells everyone and frees the code.
	hostWS.send("host.close_room", nil)
	ada.until("closed", func(m wsMessage) bool { return m.Type == "server.closed" })
	if status, _ := call(t, http.MethodGet, api+"/rooms/"+room.Code, ""); status != 404 {
		// Removal is asynchronous; give it a beat before asserting.
		time.Sleep(100 * time.Millisecond)
		if status, _ := call(t, http.MethodGet, api+"/rooms/"+room.Code, ""); status != 404 {
			t.Fatalf("closed room still resolvable: %d", status)
		}
	}
}

func TestReconnect_PlayerKeepsSeatAndScore(t *testing.T) {
	server, _ := boot(t)
	api := server.URL + "/api/v1"

	_, env := call(t, http.MethodPost, api+"/rooms", `{"game_id":"quiz","config":{"rounds":1,"questions_per_round":3,"pacing":"host"}}`)
	var room struct {
		Code      string `json:"code"`
		HostToken string `json:"host_token"`
	}
	_ = json.Unmarshal(env.Data, &room)
	_, env = call(t, http.MethodPost, api+"/rooms/"+room.Code+"/players", `{"name":"Ada"}`)
	var joined struct {
		PlayerToken string `json:"player_token"`
	}
	_ = json.Unmarshal(env.Data, &joined)

	hostWS := dial(t, server, room.Code, "host", room.HostToken)
	ada := dial(t, server, room.Code, "player", joined.PlayerToken)
	hostWS.until("ada connected", func(m wsMessage) bool {
		return m.State != nil && len(m.State.Room.Players) == 1 && m.State.Room.Players[0].Connected
	})

	hostWS.send("host.start", nil)
	hostWS.send("host.next", nil)
	asked := ada.until("question", inPhase(quiz.PhaseQuestion))
	ada.send("quiz.answer", map[string]int{"question_seq": 1, "choice": rightIndex(asked.State.Game)})
	scored := ada.until("reveal", inPhase(quiz.PhaseReveal))
	before := scored.State.Game.You.Score
	if before < quiz.BasePoints {
		t.Fatalf("score before drop = %d", before)
	}

	// The phone drops. The host sees it; the seat is kept.
	_ = ada.conn.Close()
	hostWS.until("ada disconnected", func(m wsMessage) bool {
		return m.State != nil && len(m.State.Room.Players) == 1 && !m.State.Room.Players[0].Connected
	})

	// Same token, new socket: the very first message is the full current state.
	back := dial(t, server, room.Code, "player", joined.PlayerToken)
	resumed := back.until("state on reconnect", func(m wsMessage) bool { return m.State != nil && m.State.Game != nil })
	if resumed.State.Game.Phase != quiz.PhaseReveal || resumed.State.Game.You.Score != before {
		t.Fatalf("after reconnect: phase=%s score=%d, want reveal and %d", resumed.State.Game.Phase, resumed.State.Game.You.Score, before)
	}
}

// ---- solo and Lagos Life duels -----------------------------------------------

// fakePlatform stands in for Lagos Life: it knows two sessions and one match,
// and records the result it is sent.
type fakePlatform struct {
	mu        sync.Mutex
	sessions  map[string]vatar.Session
	match     vatar.Match
	results   []string
	purchases map[string]vatar.Purchase
	delivered []string
}

func (f *fakePlatform) Configured() bool { return true }
func (f *fakePlatform) DevMode() bool    { return false }
func (f *fakePlatform) VerifySession(_ context.Context, token string) (vatar.Session, error) {
	if session, ok := f.sessions[token]; ok {
		return session, nil
	}
	return vatar.Session{}, vatar.ErrInvalidSession
}
func (f *fakePlatform) GetMatch(_ context.Context, id string) (vatar.Match, error) {
	if id != f.match.ID {
		return vatar.Match{}, &vatar.APIError{Status: 404, Code: "not_found"}
	}
	return f.match, nil
}
func (f *fakePlatform) PostResult(_ context.Context, match vatar.Match, winner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, match.ID+"|"+match.Nonce+"|"+winner)
	return nil
}
func (f *fakePlatform) GetPurchase(_ context.Context, id string) (vatar.Purchase, error) {
	if purchase, ok := f.purchases[id]; ok {
		return purchase, nil
	}
	return vatar.Purchase{}, &vatar.APIError{Status: 404, Code: "not_found"}
}
func (f *fakePlatform) DeliverPurchase(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delivered = append(f.delivered, id)
	return nil
}
func (f *fakePlatform) deliveries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.delivered...)
}
func (f *fakePlatform) reported() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.results...)
}

type seat struct {
	Code        string `json:"code"`
	PlayerID    string `json:"player_id"`
	PlayerToken string `json:"player_token"`
	Name        string `json:"name"`
}

func TestSolo_OnePlayerOwnsAndPlaysAQuickRun(t *testing.T) {
	server, _ := boot(t)
	api := server.URL + "/api/v1"

	// Easy questions on a 10-second clock: 80% x 120% = 96% of the usual points.
	// The player asks for 5 rounds of 30; a solo run is one round of ten regardless.
	status, env := call(t, http.MethodPost, api+"/solo", `{"name":"Ada","config":{"difficulty":"easy","seconds_per_question":10,"rounds":5,"questions_per_round":30,"pacing":"host"}}`)
	var mine seat
	_ = json.Unmarshal(env.Data, &mine)
	if status != 201 || mine.PlayerToken == "" {
		t.Fatalf("solo start: %d %+v", status, env.Error)
	}

	// The room is one person's game: nobody else can walk in.
	if status, env := call(t, http.MethodPost, api+"/rooms/"+mine.Code+"/players", `{"name":"Intruder"}`); status != 403 || env.Error.Code != "room_private" {
		t.Fatalf("public join to a solo room: %d %+v", status, env.Error)
	}
	if status, env := call(t, http.MethodGet, api+"/rooms/"+mine.Code, ""); status != 200 || strings.Contains(string(env.Data), `"joinable":true`) {
		t.Fatalf("a solo room must not advertise itself as joinable: %s", env.Data)
	}

	me := dial(t, server, mine.Code, "player", mine.PlayerToken)
	me.until("seated", func(m wsMessage) bool {
		return m.State != nil && len(m.State.Room.Players) == 1 && m.State.Room.Players[0].Connected
	})
	// There is no host screen; the owner starts their own game.
	me.send("host.start", nil)

	total := 0
	for q := 1; q <= quiz.SoloQuestions; q++ {
		asked := me.until("question", func(m wsMessage) bool { return inPhase(quiz.PhaseQuestion)(m) && m.State.Game.QuestionSeq == q })
		game := asked.State.Game
		if !game.Quick || game.TotalRounds != 1 || game.QuestionsPerRound != quiz.SoloQuestions || game.MultiplierPct != 96 {
			t.Fatalf("solo shape = quick:%v rounds:%d per:%d multiplier:%d", game.Quick, game.TotalRounds, game.QuestionsPerRound, game.MultiplierPct)
		}
		me.send("quiz.answer", map[string]int{"question_seq": q, "choice": rightIndex(game)})
		revealed := me.until("reveal", func(m wsMessage) bool { return inPhase(quiz.PhaseReveal)(m) && m.State.Game.QuestionSeq == q })
		if revealed.State.Game.You.Points < quiz.BasePoints*96/100 {
			t.Fatalf("q%d points = %d", q, revealed.State.Game.You.Points)
		}
		total += revealed.State.Game.You.Points
		// Quick tempo: the reveal moves on by itself, straight to the next
		// question — never through a separate scoreboard.
	}

	finished := me.until("finished", func(m wsMessage) bool {
		if m.State != nil && m.State.Game != nil && m.State.Game.Phase == quiz.PhaseLeaderboard {
			t.Fatal("a quick game must not show a separate scoreboard")
		}
		return inPhase(quiz.PhaseFinished)(m)
	})
	final := finished.State.Game.Final
	if final.TotalQuestions != quiz.SoloQuestions || final.Stats[0].Correct != quiz.SoloQuestions || final.Stats[0].Score != total {
		t.Fatalf("final = %+v, want %d right and %d points", final.Stats[0], quiz.SoloQuestions, total)
	}

	// The owner can go again without a host.
	me.send("host.play_again", nil)
	me.until("second run", inPhase(quiz.PhaseIntro))
}

func TestMultiplier_HarderSettingsPayMore(t *testing.T) {
	cases := []struct {
		difficulty string
		seconds    int
		want       int
	}{
		{"mixed", 20, 100}, // the defaults are the baseline
		{"easy", 30, 72},   // easiest questions, longest clock: least
		{"easy", 20, 80},
		{"medium", 15, 121},
		{"hard", 20, 130},
		{"hard", 10, 156}, // hardest questions, shortest clock: most
	}
	for _, tc := range cases {
		raw := fmt.Sprintf(`{"difficulty":%q,"seconds_per_question":%d}`, tc.difficulty, tc.seconds)
		parsed, errs := quiz.NewGame(questions.NewBank()).ParseConfig(json.RawMessage(raw))
		if len(errs) > 0 {
			t.Fatalf("%s: %v", raw, errs)
		}
		if got := parsed.(quiz.Config).MultiplierPct(); got != tc.want {
			t.Errorf("%s at %ds = %d%%, want %d%%", tc.difficulty, tc.seconds, got, tc.want)
		}
	}
}

func TestDuel_PlatformMatchIsPlayedAndTheWinnerReported(t *testing.T) {
	lagos := &fakePlatform{
		sessions: map[string]vatar.Session{
			"sess-ada": {PlayerID: "p_ada", Name: "Ada", Mode: "test"},
			"sess-bem": {PlayerID: "p_bem", Name: "Ada", Mode: "test"}, // same display name on purpose
			"sess-cy":  {PlayerID: "p_cy", Name: "Cy", Mode: "test"},
		},
		match: vatar.Match{ID: "m_1", Status: vatar.MatchPlaying, Nonce: "nonce-1", Stake: 500,
			Players: []vatar.MatchPlayer{{PlayerID: "p_ada", Name: "Ada"}, {PlayerID: "p_bem", Name: "Ada"}}},
	}
	server, _ := bootWith(t, lagos)
	api := server.URL + "/api/v1/platform"

	join := func(session string) (int, envelope, seat) {
		status, env := call(t, http.MethodPost, api+"/matches/m_1/join", fmt.Sprintf(`{"session":%q}`, session))
		var s seat
		_ = json.Unmarshal(env.Data, &s)
		return status, env, s
	}

	// Who may sit is decided by the platform's record, not by asking nicely.
	if status, env, _ := join("forged"); status != 401 || env.Error.Code != "session_invalid" {
		t.Fatalf("forged session: %d %+v", status, env.Error)
	}
	if status, env, _ := join("sess-cy"); status != 403 || env.Error.Code != "not_in_match" {
		t.Fatalf("a player outside the match: %d %+v", status, env.Error)
	}
	if status, env := call(t, http.MethodPost, api+"/matches/m_other/join", `{"session":"sess-ada"}`); status != 502 || env.Error.Code != "platform_unavailable" {
		t.Fatalf("unknown match: %d %+v", status, env.Error)
	}

	status, _, ada := join("sess-ada")
	if status != 200 || ada.PlayerToken == "" {
		t.Fatalf("ada join: %d", status)
	}
	// Joining twice (a refresh) returns the same seat, not a second one.
	if _, _, again := join("sess-ada"); again.PlayerToken != ada.PlayerToken {
		t.Fatal("a rejoin must return the same seat")
	}
	_, _, bem := join("sess-bem")
	if bem.Code != ada.Code || bem.Name == ada.Name {
		t.Fatalf("both players share one room and keep distinct names: %+v / %+v", ada, bem)
	}

	adaWS := dial(t, server, ada.Code, "player", ada.PlayerToken)
	bemWS := dial(t, server, bem.Code, "player", bem.PlayerToken)

	// Nothing starts on its own. The first player in holds the Start button;
	// the second is refused, and so is anyone starting a duel alone.
	lobby := bemWS.until("both seated", func(m wsMessage) bool {
		return m.State != nil && len(m.State.Room.Players) == 2 && m.State.Room.Players[1].Connected
	})
	if lobby.State.Game != nil || lobby.State.Room.StarterID != ada.PlayerID || lobby.State.Room.MinToStart != 2 {
		t.Fatalf("lobby should wait for Ada to start: starter=%q min=%d", lobby.State.Room.StarterID, lobby.State.Room.MinToStart)
	}
	bemWS.send("host.start", nil)
	if msg := bemWS.until("refused", func(m wsMessage) bool { return m.Error != nil }); msg.Error.Code != "not_host" {
		t.Fatalf("the second player started the match: %+v", msg.Error)
	}
	adaWS.send("host.start", nil)
	for q := 1; q <= quiz.DuelQuestions; q++ {
		asked := adaWS.until("question", func(m wsMessage) bool { return inPhase(quiz.PhaseQuestion)(m) && m.State.Game.QuestionSeq == q })
		bemAsked := bemWS.until("question", func(m wsMessage) bool { return inPhase(quiz.PhaseQuestion)(m) && m.State.Game.QuestionSeq == q })
		// A player in a duel is not its owner and cannot skip or end it.
		if q == 1 {
			bemWS.send("host.end_game", nil)
			if msg := bemWS.until("refused", func(m wsMessage) bool { return m.Error != nil }); msg.Error.Code != "not_host" {
				t.Fatalf("a duellist ended the match: %+v", msg.Error)
			}
		}
		adaWS.send("quiz.answer", map[string]int{"question_seq": q, "choice": rightIndex(asked.State.Game)})
		bemWS.send("quiz.answer", map[string]int{"question_seq": q, "choice": (rightIndex(bemAsked.State.Game) + 1) % 4})
	}
	adaWS.until("finished", inPhase(quiz.PhaseFinished))

	var reported []string
	for i := 0; i < 100 && len(reported) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		reported = lagos.reported()
	}
	if len(reported) != 1 || reported[0] != "m_1|nonce-1|p_ada" {
		t.Fatalf("reported = %v, want Ada as the winner of m_1 with its nonce", reported)
	}
}

func TestDuel_NotStartedUntilThePlatformSaysPlaying(t *testing.T) {
	lagos := &fakePlatform{
		sessions: map[string]vatar.Session{"sess-ada": {PlayerID: "p_ada", Name: "Ada"}},
		match:    vatar.Match{ID: "m_2", Status: "open", Players: []vatar.MatchPlayer{{PlayerID: "p_ada"}}},
	}
	server, _ := bootWith(t, lagos)
	status, env := call(t, http.MethodPost, server.URL+"/api/v1/platform/matches/m_2/join", `{"session":"sess-ada"}`)
	if status != 409 || env.Error.Code != "match_not_ready" {
		t.Fatalf("an open match must not be playable yet: %d %+v", status, env.Error)
	}
}

func TestPlatform_DevSessionsWorkOutsideProduction(t *testing.T) {
	server, _ := boot(t) // no secret configured, test environment
	api := server.URL + "/api/v1/platform"

	status, env := call(t, http.MethodGet, api+"/status", "")
	if status != 200 || !strings.Contains(string(env.Data), `"dev_mode":true`) {
		t.Fatalf("status = %d %s", status, env.Data)
	}
	status, env = call(t, http.MethodPost, api+"/solo", `{"session":"dev:abc:Tunde","config":{}}`)
	var mine seat
	_ = json.Unmarshal(env.Data, &mine)
	if status != 201 || mine.Name != "Tunde" {
		t.Fatalf("dev solo: %d %+v %+v", status, env.Error, mine)
	}
	if status, env := call(t, http.MethodPost, api+"/solo", `{"config":{}}`); status != 422 || len(env.Error.FieldErrors["session"]) == 0 {
		t.Fatalf("missing session: %d %+v", status, env.Error)
	}
}

// ---- paid answer reveals ------------------------------------------------------

func TestReveal_BoughtWithGemsFromAVerifiedPurchase(t *testing.T) {
	lagos := &fakePlatform{
		sessions: map[string]vatar.Session{
			"sess-ada": {PlayerID: "p_ada", Name: "Ada", Mode: "test"},
			"sess-bem": {PlayerID: "p_bem", Name: "Bem", Mode: "test"},
		},
		purchases: map[string]vatar.Purchase{
			"pur_1":      {ID: "pur_1", ProductID: "gems_10", PlayerID: "p_ada", Amount: 100, Status: vatar.PurchasePaid},
			"pur_2":      {ID: "pur_2", ProductID: "gems_10", PlayerID: "p_ada", Amount: 100, Status: vatar.PurchasePaid},
			"pur_refund": {ID: "pur_refund", ProductID: "gems_10", PlayerID: "p_ada", Status: vatar.PurchaseRefunded},
			"pur_other":  {ID: "pur_other", ProductID: "mystery_box", PlayerID: "p_ada", Status: vatar.PurchasePaid},
		},
	}
	server, _ := bootWith(t, lagos)
	api := server.URL + "/api/v1/platform"
	post := func(path, body string) (int, envelope) { return call(t, http.MethodPost, api+path, body) }
	gems := func(env envelope) int {
		var out struct {
			Gems int `json:"gems"`
		}
		_ = json.Unmarshal(env.Data, &out)
		return out.Gems
	}

	if status, env := post("/wallet", `{"session":"sess-ada"}`); status != 200 || gems(env) != 0 {
		t.Fatalf("a new player starts with no gems: %d %s", status, env.Data)
	}

	// A purchase is real only when the platform says so, and only for its buyer.
	if status, env := post("/purchases/pur_made_up/claim", `{"session":"sess-ada"}`); status != 404 || env.Error.Code != "purchase_not_found" {
		t.Fatalf("a made-up purchase: %d %+v", status, env.Error)
	}
	if status, env := post("/purchases/pur_1/claim", `{"session":"sess-bem"}`); status != 404 || env.Error.Code != "purchase_not_found" {
		t.Fatalf("someone else's purchase: %d %+v", status, env.Error)
	}
	if status, env := post("/purchases/pur_refund/claim", `{"session":"sess-ada"}`); status != 409 || env.Error.Code != "purchase_refunded" {
		t.Fatalf("a refunded purchase: %d %+v", status, env.Error)
	}
	if status, _ := post("/purchases/pur_other/claim", `{"session":"sess-ada"}`); status != 404 {
		t.Fatalf("a product this server does not sell: %d", status)
	}
	if len(lagos.deliveries()) != 0 {
		t.Fatalf("nothing refused may be marked delivered: %v", lagos.deliveries())
	}

	if status, env := post("/purchases/pur_1/claim", `{"session":"sess-ada"}`); status != 200 || gems(env) != 10 {
		t.Fatalf("claim: %d %s", status, env.Data)
	}
	// Claiming again (a reload, a retry) gives nothing more.
	if status, env := post("/purchases/pur_1/claim", `{"session":"sess-ada"}`); status != 200 || gems(env) != 10 {
		t.Fatalf("second claim of one purchase: %d %s", status, env.Data)
	}
	if got := lagos.deliveries(); len(got) != 2 || got[0] != "pur_1" {
		t.Fatalf("delivered = %v, want pur_1 marked (repeat is harmless)", got)
	}

	// The webhook settles a purchase too, but only when it is really signed.
	event := []byte(`{"id":"evt_1","type":"purchase.paid","mode":"test","data":{"purchaseId":"pur_2","productId":"gems_10","playerId":"p_ada","amount":100,"currency":"NGN"}}`)
	hook := func(signature string) int {
		req, _ := http.NewRequest(http.MethodPost, api+"/webhook", bytes.NewReader(event))
		req.Header.Set("X-Vatar-Signature", signature)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if status := hook("sha256=" + strings.Repeat("0", 64)); status != 401 {
		t.Fatalf("a forged webhook: %d, want 401", status)
	}
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(event)
	if status := hook("sha256=" + hex.EncodeToString(mac.Sum(nil))); status != 204 {
		t.Fatalf("a signed webhook: %d, want 204", status)
	}
	if _, env := post("/wallet", `{"session":"sess-ada"}`); gems(env) != 20 {
		t.Fatalf("after the webhook: %s, want 20 gems", env.Data)
	}

	// A mixed solo run: the first reveal costs 2 gems.
	status, env := post("/solo", `{"session":"sess-ada","config":{"difficulty":"mixed","seconds_per_question":20}}`)
	var mine seat
	_ = json.Unmarshal(env.Data, &mine)
	if status != 201 {
		t.Fatalf("solo start: %d %+v", status, env.Error)
	}
	me := dial(t, server, mine.Code, "player", mine.PlayerToken)
	me.until("seated", func(m wsMessage) bool {
		return m.State != nil && len(m.State.Room.Players) == 1 && m.State.Room.Players[0].Connected
	})
	me.send("host.start", nil)
	asked := me.until("question", func(m wsMessage) bool { return inPhase(quiz.PhaseQuestion)(m) && m.State.Game.QuestionSeq == 1 })
	if asked.State.Game.RevealCost != 2 || asked.State.Game.You.HintIdx != nil {
		t.Fatalf("question 1 on mixed: cost %d, hint %v", asked.State.Game.RevealCost, asked.State.Game.You.HintIdx)
	}

	// A reveal cannot be asked for over the socket, nor for someone else's game.
	me.send("quiz.reveal", map[string]int{"question_seq": 1})
	if msg := me.until("refused", func(m wsMessage) bool { return m.Error != nil }); msg.Error.Code != "invalid_action" {
		t.Fatalf("a reveal over the socket: %+v", msg.Error)
	}
	if status, env := post("/solo/"+mine.Code+"/reveal", `{"session":"sess-bem","question_seq":1}`); status != 403 {
		t.Fatalf("a reveal in someone else's game: %d %+v", status, env.Error)
	}

	status, env = post("/solo/"+mine.Code+"/reveal", `{"session":"sess-ada","question_seq":1}`)
	var shown struct {
		AnswerIdx int `json:"answer_idx"`
		Cost      int `json:"cost"`
		Gems      int `json:"gems"`
	}
	_ = json.Unmarshal(env.Data, &shown)
	if status != 200 || shown.Cost != 2 || shown.Gems != 18 || shown.AnswerIdx != rightIndex(asked.State.Game) {
		t.Fatalf("reveal: %d %+v (right answer %d)", status, shown, rightIndex(asked.State.Game))
	}
	hinted := me.until("hint", func(m wsMessage) bool {
		return m.State != nil && m.State.Game != nil && m.State.Game.You != nil && m.State.Game.You.HintIdx != nil
	})
	if *hinted.State.Game.You.HintIdx != shown.AnswerIdx {
		t.Fatal("the hint in the game state must be the answer that was paid for")
	}
	// Asking twice for the same question charges once: the second is refused
	// and its gems come straight back.
	if status, _ := post("/solo/"+mine.Code+"/reveal", `{"session":"sess-ada","question_seq":1}`); status == 200 {
		t.Fatal("a second reveal of one question must be refused")
	}
	if _, env := post("/wallet", `{"session":"sess-ada"}`); gems(env) != 18 {
		t.Fatalf("a refused reveal must cost nothing: %s", env.Data)
	}

	me.send("quiz.answer", map[string]int{"question_seq": 1, "choice": shown.AnswerIdx})
	revealed := me.until("reveal", func(m wsMessage) bool { return inPhase(quiz.PhaseReveal)(m) && m.State.Game.QuestionSeq == 1 })
	if got := revealed.State.Game.You.Points; got != quiz.BasePoints {
		t.Fatalf("a revealed answer scored %d, want the base %d and no speed bonus", got, quiz.BasePoints)
	}

	// Bem has no gems: asking is refused, and costs nothing.
	status, env = post("/solo", `{"session":"sess-bem","config":{}}`)
	var theirs seat
	_ = json.Unmarshal(env.Data, &theirs)
	bem := dial(t, server, theirs.Code, "player", theirs.PlayerToken)
	bem.until("seated", func(m wsMessage) bool {
		return m.State != nil && len(m.State.Room.Players) == 1 && m.State.Room.Players[0].Connected
	})
	bem.send("host.start", nil)
	bem.until("question", func(m wsMessage) bool { return inPhase(quiz.PhaseQuestion)(m) && m.State.Game.QuestionSeq == 1 })
	if status, env := post("/solo/"+theirs.Code+"/reveal", `{"session":"sess-bem","question_seq":1}`); status != 409 || env.Error.Code != "not_enough_gems" {
		t.Fatalf("a reveal without gems: %d %+v", status, env.Error)
	}
}
