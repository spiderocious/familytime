package app_test

import (
	"bytes"
	"context"
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
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/features/results"
	"familytime/backend/internal/games/quiz"
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

func boot(t *testing.T) (*httptest.Server, *memoryStore) {
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
		Env:     env.Env{AppEnv: env.EnvTest, AppURL: "http://web.test", CORSOrigins: []string{"http://web.test"}},
		DB:      alwaysUp{},
		Bank:    bank,
		Results: results.NewService(store),
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
