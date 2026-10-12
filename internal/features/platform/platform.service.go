package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/features/rooms"
	"familytime/backend/internal/features/solo"
	"familytime/backend/internal/games/quiz"
	"familytime/backend/internal/lib/logger"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/lib/vatar"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

const (
	maxNameRunes = 16
	// A duel is started by one of its players, and never by a player alone.
	duelMinPlayers  = 2
	resultAttempts  = 4
	resultRetryWait = 3 * time.Second
	duelLinger      = 10 * time.Minute
)

// Platform is the part of the Vatar client this service uses.
type Platform interface {
	Configured() bool
	DevMode() bool
	VerifySession(ctx context.Context, token string) (vatar.Session, error)
	GetMatch(ctx context.Context, matchID string) (vatar.Match, error)
	PostResult(ctx context.Context, match vatar.Match, winner string) error
}

// Registry is the part of the engine a duel drives.
type Registry interface {
	Game(id string) (engine.Game, bool)
	Create(game engine.Game, config engine.Config) *engine.Room
}

// duel is one platform match being played in one of our rooms.
type duel struct {
	match vatar.Match
	room  *engine.Room
	// platform player id → the seat we gave them, and back again.
	seats    map[string]*engine.Player
	platform map[string]string
}

type Service struct {
	platform Platform
	registry Registry
	solo     *solo.Service

	mu     sync.Mutex
	duels  map[string]*duel // by platform match id
	byRoom map[string]*duel // by our room code
}

func NewService(platform Platform, registry Registry, soloService *solo.Service) *Service {
	return &Service{platform: platform, registry: registry, solo: soloService, duels: map[string]*duel{}, byRoom: map[string]*duel{}}
}

func (s *Service) Status() result.Result[StatusView] {
	return result.Ok(StatusView{Configured: s.platform.Configured(), DevMode: s.platform.DevMode()})
}

// identify turns a session token into a player, or into the failure to show.
func (s *Service) identify(ctx context.Context, token string) (vatar.Session, *result.Failure) {
	session, err := s.platform.VerifySession(ctx, token)
	switch {
	case err == nil:
		return session, nil
	case errors.Is(err, vatar.ErrInvalidSession):
		return vatar.Session{}, &result.Failure{Code: constants.CodeSessionInvalid, MessageKey: messages.PlatformSession, Status: constants.StatusUnauthorized}
	case errors.Is(err, vatar.ErrNotConfigured):
		return vatar.Session{}, &result.Failure{Code: constants.CodePlatformUnavailable, MessageKey: messages.PlatformUnavailable, Status: constants.StatusUnavailable, RejectionReason: "not_configured"}
	default:
		logger.With(ctx).Error("platform session check failed", "error", err.Error())
		return vatar.Session{}, &result.Failure{Code: constants.CodePlatformUnavailable, MessageKey: messages.PlatformUnavailable, Status: constants.StatusBadGateway, RejectionReason: "session_verify_failed"}
	}
}

// StartSolo starts a solo game for a platform player. Their name comes from
// the platform, never from the request: a player cannot pick who they are.
func (s *Service) StartSolo(ctx context.Context, token string, rawConfig json.RawMessage) result.Result[SeatView] {
	session, failure := s.identify(ctx, token)
	if failure != nil {
		return result.FailWith[SeatView](failure)
	}
	started := s.solo.Start(displayName(session.Name), avatarFor(session.PlayerID), rawConfig)
	if !started.OK() {
		return result.FailWith[SeatView](started.Failure)
	}
	return result.Ok(SeatView{StartedView: started.Data, Mode: session.Mode})
}

// JoinMatch seats a platform player in the room for their platform match,
// creating the room the first time anyone from that match arrives.
func (s *Service) JoinMatch(ctx context.Context, matchID, token string) result.Result[SeatView] {
	session, failure := s.identify(ctx, token)
	if failure != nil {
		return result.FailWith[SeatView](failure)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.duels[matchID]
	if !ok {
		created, failure := s.openDuel(ctx, matchID)
		if failure != nil {
			return result.FailWith[SeatView](failure)
		}
		d = created
	}

	// Membership is decided by the platform's own record of the match, never
	// by the request. (A dev duel has no such record, so anyone may sit.)
	if !s.platform.DevMode() && !inMatch(d.match, session.PlayerID) {
		return result.Fail[SeatView](constants.CodeNotInMatch, messages.PlatformNotInMatch, constants.StatusForbidden)
	}

	player, seated := d.seats[session.PlayerID]
	if !seated {
		var failure *result.Failure
		player, failure = d.room.Seat(uniqueName(d, displayName(session.Name)), avatarFor(session.PlayerID))
		if failure != nil {
			return result.FailWith[SeatView](failure)
		}
		d.seats[session.PlayerID] = player
		d.platform[player.ID] = session.PlayerID
	}

	return result.Ok(SeatView{
		StartedView: solo.StartedView{Code: d.room.Code(), PlayerID: player.ID, PlayerToken: player.Token, Name: player.Name, Avatar: player.Avatar},
		Mode:        session.Mode,
	})
}

// openDuel reads the match from the platform and opens a room for it. Called
// with the lock held.
func (s *Service) openDuel(ctx context.Context, matchID string) (*duel, *result.Failure) {
	match := vatar.Match{ID: matchID, Status: vatar.MatchPlaying, Mode: vatar.ModeDev}

	if !s.platform.DevMode() {
		fetched, err := s.platform.GetMatch(ctx, matchID)
		if err != nil {
			logger.With(ctx).Error("could not read platform match", "match_id", matchID, "error", err.Error())
			return nil, &result.Failure{Code: constants.CodePlatformUnavailable, MessageKey: messages.PlatformUnavailable, Status: constants.StatusBadGateway, RejectionReason: "match_fetch_failed"}
		}
		// Stakes are only locked in once the platform says the match is
		// playing. Starting our game earlier could play a match that is later
		// cancelled and refunded.
		if fetched.Status != vatar.MatchPlaying {
			return nil, &result.Failure{Code: constants.CodeMatchNotReady, MessageKey: messages.PlatformMatchWait, Status: constants.StatusConflict, RejectionReason: "status_" + fetched.Status}
		}
		match = fetched
	}

	game, ok := s.registry.Game(quiz.GameID)
	if !ok {
		return nil, &result.Failure{Code: constants.CodeGameNotFound, MessageKey: messages.GamesNotFound, Status: constants.StatusNotFound}
	}
	room := s.registry.Create(game, quiz.DuelConfig())
	room.MakePrivate()
	// Nothing starts on its own: the first player in the room presses Start
	// once at least one opponent has arrived.
	room.LetPlayersStart(duelMinPlayers)

	d := &duel{match: match, room: room, seats: map[string]*engine.Player{}, platform: map[string]string{}}
	s.duels[matchID] = d
	s.byRoom[room.Code()] = d
	return d, nil
}

// OnFinished is the engine's finished-game hook. For a duel it works out the
// winner from the final standings and reports it to the platform, which then
// pays out the stakes.
func (s *Service) OnFinished(finished engine.FinishedGame) {
	s.mu.Lock()
	d, ok := s.byRoom[finished.RoomCode]
	s.mu.Unlock()
	if !ok {
		return // a party game or a solo run: nothing to report
	}

	winner := decideWinner(finished.Summary, d.platform)
	logger.Info("duel finished", "match_id", d.match.ID, "winner", winner, "players", len(finished.Summary.Players))

	if s.platform.DevMode() {
		s.forgetLater(d)
		return
	}

	// Reporting must not be lost to a blip: the players' stakes are locked
	// until it lands. The platform settles once and treats a repeat of the
	// same result as harmless, so retrying is safe.
	for attempt := 1; attempt <= resultAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.platform.PostResult(ctx, d.match, winner)
		cancel()
		if err == nil {
			logger.Info("duel result reported", "match_id", d.match.ID, "winner", winner, "attempt", attempt)
			break
		}
		var apiErr *vatar.APIError
		if errors.As(err, &apiErr) && apiErr.Code == vatar.ErrCodeAlreadySettled {
			logger.Warn("duel was already settled with a different result", "match_id", d.match.ID)
			break
		}
		logger.Error("could not report duel result", "match_id", d.match.ID, "attempt", attempt, "error", err.Error())
		time.Sleep(resultRetryWait * time.Duration(attempt))
	}
	s.forgetLater(d)
}

func (s *Service) forgetLater(d *duel) {
	time.AfterFunc(duelLinger, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.duels, d.match.ID)
		delete(s.byRoom, d.room.Code())
	})
}

// decideWinner returns the platform id of the winner, or a draw. The summary
// is in rank order. A tie for first is a draw — every stake is refunded —
// rather than a coin toss with other people's money.
func decideWinner(summary engine.Summary, platformID map[string]string) string {
	players := summary.Players
	if len(players) == 0 {
		return vatar.WinnerDraw
	}
	if len(players) > 1 && players[0].Score == players[1].Score {
		return vatar.WinnerDraw
	}
	if id, ok := platformID[players[0].PlayerID]; ok {
		return id
	}
	return vatar.WinnerDraw
}

func inMatch(match vatar.Match, playerID string) bool {
	for _, p := range match.Players {
		if p.PlayerID == playerID {
			return true
		}
	}
	return false
}

// displayName fits a platform name to what our screens can show.
func displayName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "Player"
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		name = string([]rune(name)[:maxNameRunes])
	}
	return name
}

// uniqueName keeps two players called "Ada" apart within one duel.
func uniqueName(d *duel, name string) string {
	taken := func(candidate string) bool {
		for _, p := range d.seats {
			if strings.EqualFold(p.Name, candidate) {
				return true
			}
		}
		return false
	}
	if !taken(name) {
		return name
	}
	for n := 2; ; n++ {
		suffix := fmt.Sprintf(" %d", n)
		base := []rune(name)
		if len(base)+len(suffix) > maxNameRunes {
			base = base[:maxNameRunes-len(suffix)]
		}
		if candidate := string(base) + suffix; !taken(candidate) {
			return candidate
		}
	}
}

// avatarFor gives a platform player the same animal every time they play.
func avatarFor(playerID string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(playerID))
	return rooms.Avatars[int(hash.Sum32())%len(rooms.Avatars)]
}
