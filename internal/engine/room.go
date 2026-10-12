package engine

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"familytime/backend/internal/lib/ids"
	"familytime/backend/internal/lib/response"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

const reactionCooldownMs = 350

// Player is a seat in a room. The token is what lets a refreshed phone take
// the same seat back, with its score intact.
type Player struct {
	ID       string
	Name     string
	Avatar   string
	Token    string
	JoinedAt EpochMs

	conns       int
	lastReactAt EpochMs
}

func (p *Player) ref() PlayerRef { return PlayerRef{ID: p.ID, Name: p.Name, Avatar: p.Avatar} }

// Subscriber is one live connection, described without naming a transport.
type Subscriber struct {
	ID       string
	Audience Audience
	Send     func(Envelope)
}

// FinishedGame is handed to the persistence hook when a game ends.
type FinishedGame struct {
	RoomCode   string
	GameID     string
	InstanceID string
	Config     Config
	StartedAt  EpochMs
	EndedAt    EpochMs
	Summary    Summary
}

type deps struct {
	now        func() EpochMs
	joinURL    func(code string) string
	onFinished func(FinishedGame)
	onClosed   func(code string)
}

// Room is the resting and active container. Every exported method takes the
// lock, so the game instance inside never needs one.
type Room struct {
	mu sync.Mutex

	code      string
	hostToken string
	game      Game
	config    Config
	phase     RoomPhase
	players   []*Player
	subs      map[string]*Subscriber
	hostConns int

	// ownerID names a player who may do what the host screen does. Set for
	// rooms with no host screen: a solo game, or a duel run by the server.
	ownerID string
	// private rooms are not open to the public join endpoint; their seats are
	// handed out by the server to players it has already identified.
	private bool

	instance   Instance
	instanceID string
	startedAt  EpochMs
	reported   bool

	timers   map[string]*time.Timer
	timerGen map[string]int

	lastActivity EpochMs
	deps         deps
}

func newRoom(code string, game Game, config Config, d deps) *Room {
	return &Room{
		code:         code,
		hostToken:    ids.Token(),
		game:         game,
		config:       config,
		phase:        PhaseLobby,
		subs:         map[string]*Subscriber{},
		timers:       map[string]*time.Timer{},
		timerGen:     map[string]int{},
		lastActivity: d.now(),
		deps:         d,
	}
}

func (r *Room) Code() string      { return r.code }
func (r *Room) HostToken() string { return r.hostToken }

func fail(code constants.ErrorCode, key messages.Key, status int) *result.Failure {
	return &result.Failure{Code: code, MessageKey: key, Status: status}
}

// Info is the public summary: enough to tell a joiner whether the code is good.
func (r *Room) Info() RoomInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	manifest := r.game.Manifest()
	return RoomInfo{
		Code:        r.code,
		Phase:       r.phase,
		GameID:      manifest.ID,
		GameTitle:   manifest.Title,
		PlayerCount: len(r.players),
		MaxPlayers:  manifest.MaxPlayers,
		Joinable:    !r.private && r.phase != PhaseClosed && len(r.players) < manifest.MaxPlayers,
	}
}

// AddPlayer seats a new player. Joining mid-game is allowed: someone walking
// into the room should be able to pick up a phone and play from the next
// question.
func (r *Room) AddPlayer(name, avatar string) (*Player, *result.Failure) {
	return r.seat(name, avatar, false)
}

// Seat adds a player the server has already identified, to a private room.
func (r *Room) Seat(name, avatar string) (*Player, *result.Failure) {
	return r.seat(name, avatar, true)
}

// MakePrivate closes the room to the public join endpoint.
func (r *Room) MakePrivate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.private = true
}

// SetOwner lets one player run the room as the host screen would.
func (r *Room) SetOwner(playerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ownerID = playerID
}

// StartNow starts the game on the server's own authority (a duel begins when
// its players are seated, not when one of them presses a button).
func (r *Room) StartNow() *result.Failure {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase == PhaseClosed {
		return fail(constants.CodeRoomClosed, messages.RoomsClosed, constants.StatusGone)
	}
	return r.start(r.deps.now())
}

// Close ends the room for everyone, on the server's own authority.
func (r *Room) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase != PhaseClosed {
		r.close()
	}
}

func (r *Room) seat(name, avatar string, trusted bool) (*Player, *result.Failure) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.phase == PhaseClosed {
		return nil, fail(constants.CodeRoomClosed, messages.RoomsClosed, constants.StatusGone)
	}
	if r.private && !trusted {
		return nil, fail(constants.CodeRoomPrivate, messages.RoomsPrivate, constants.StatusForbidden)
	}
	if len(r.players) >= r.game.Manifest().MaxPlayers {
		return nil, fail(constants.CodeRoomFull, messages.RoomsFull, constants.StatusConflict)
	}
	for _, p := range r.players {
		if strings.EqualFold(p.Name, name) {
			f := fail(constants.CodeNameTaken, messages.RoomsNameTaken, constants.StatusConflict)
			f.FieldErrors = map[string][]string{"name": {messages.Resolve(messages.RoomsNameTaken)}}
			return nil, f
		}
	}

	now := r.deps.now()
	player := &Player{ID: ids.New(ids.Player), Name: name, Avatar: avatar, Token: ids.Token(), JoinedAt: now}
	r.players = append(r.players, player)
	r.lastActivity = now

	r.rosterChanged(now)
	return player, nil
}

// Authenticate resolves a token to an audience. The host token controls the
// room; a player token is one seat.
func (r *Room) Authenticate(role AudienceKind, token string) (Audience, *result.Failure) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.phase == PhaseClosed {
		return Audience{}, fail(constants.CodeRoomClosed, messages.RoomsClosed, constants.StatusGone)
	}
	if role == AudienceHost {
		if token != "" && token == r.hostToken {
			return Audience{Kind: AudienceHost}, nil
		}
		return Audience{}, fail(constants.CodeInvalidToken, messages.RoomsInvalidToken, constants.StatusUnauthorized)
	}
	for _, p := range r.players {
		if token != "" && p.Token == token {
			return Audience{Kind: AudiencePlayer, PlayerID: p.ID}, nil
		}
	}
	return Audience{}, fail(constants.CodeInvalidToken, messages.RoomsInvalidToken, constants.StatusUnauthorized)
}

// Attach registers a live connection and sends it the current state at once,
// so a client that connects late never waits for the next event.
func (r *Room) Attach(sub *Subscriber) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.subs[sub.ID] = sub
	now := r.deps.now()
	r.lastActivity = now

	if sub.Audience.Kind == AudienceHost {
		r.hostConns++
	} else if p := r.findPlayer(sub.Audience.PlayerID); p != nil {
		p.conns++
	}
	r.rosterChanged(now)
}

func (r *Room) Detach(subID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sub, ok := r.subs[subID]
	if !ok {
		return
	}
	delete(r.subs, subID)

	if sub.Audience.Kind == AudienceHost {
		r.hostConns = max(0, r.hostConns-1)
	} else if p := r.findPlayer(sub.Audience.PlayerID); p != nil {
		p.conns = max(0, p.conns-1)
	}
	if r.phase != PhaseClosed {
		r.rosterChanged(r.deps.now())
	}
}

// Handle processes one client message. A returned failure is sent back to the
// sender only.
func (r *Room) Handle(audience Audience, msgType string, payload json.RawMessage) *result.Failure {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.phase == PhaseClosed {
		return fail(constants.CodeRoomClosed, messages.RoomsClosed, constants.StatusGone)
	}
	now := r.deps.now()
	r.lastActivity = now
	isHost := audience.Kind == AudienceHost || (r.ownerID != "" && audience.PlayerID == r.ownerID)

	switch msgType {
	case ClientHostConfigure:
		if !isHost {
			return notHost()
		}
		return r.configure(payload)
	case ClientHostStart, ClientHostPlayAgain:
		if !isHost {
			return notHost()
		}
		return r.start(now)
	case ClientHostBackToLobby:
		if !isHost {
			return notHost()
		}
		r.stopGame()
		r.phase = PhaseLobby
		r.broadcast()
		return nil
	case ClientHostCloseRoom:
		if !isHost {
			return notHost()
		}
		r.close()
		return nil
	case ClientPlayerReact:
		return r.react(audience, payload, now)
	}

	if r.instance == nil {
		return fail(constants.CodeNoActiveGame, messages.PlayNoActiveGame, constants.StatusConflict)
	}
	ctx := ActionCtx{Role: audience.Kind, IsHost: isHost, Now: now}
	if p := r.findPlayer(audience.PlayerID); p != nil {
		ctx.Actor = p.ref()
	}
	effects, failure := r.instance.OnAction(Action{Type: msgType, Payload: payload}, ctx)
	if failure != nil {
		return failure
	}
	r.execute(effects)
	return nil
}

func notHost() *result.Failure {
	return fail(constants.CodeNotHost, messages.RoomsNotHost, constants.StatusForbidden)
}

func (r *Room) configure(payload json.RawMessage) *result.Failure {
	if r.phase != PhaseLobby {
		return fail(constants.CodeGameAlreadyRunning, messages.PlayAlreadyRunning, constants.StatusConflict)
	}
	config, fieldErrors := r.game.ParseConfig(payload)
	if len(fieldErrors) > 0 {
		f := fail(constants.CodeValidation, messages.CommonValidation, constants.StatusUnprocessable)
		f.FieldErrors = fieldErrors
		return f
	}
	r.config = config
	r.broadcast()
	return nil
}

func (r *Room) start(now EpochMs) *result.Failure {
	if r.instance != nil && !r.instance.IsOver() {
		return fail(constants.CodeGameAlreadyRunning, messages.PlayAlreadyRunning, constants.StatusConflict)
	}
	if len(r.players) < r.game.Manifest().MinPlayers {
		return fail(constants.CodeNotEnoughPlayers, messages.PlayNotEnoughPlayers, constants.StatusConflict)
	}
	if failure := r.game.CanStart(r.config); failure != nil {
		return failure
	}

	r.stopGame()
	instance, effects := r.game.New(InitInput{
		Config: r.config,
		Roster: r.roster(),
		Seed:   time.Now().UnixNano(),
		Now:    now,
	})
	r.instance = instance
	r.instanceID = ids.New(ids.Game)
	r.startedAt = now
	r.reported = false
	r.phase = PhasePlaying
	r.execute(effects)
	return nil
}

func (r *Room) react(audience Audience, payload json.RawMessage, now EpochMs) *result.Failure {
	player := r.findPlayer(audience.PlayerID)
	if player == nil {
		return fail(constants.CodeInvalidAction, messages.PlayInvalidAction, constants.StatusUnprocessable)
	}
	var body struct {
		Emoji string `json:"emoji"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || !slices.Contains(Reactions, body.Emoji) {
		return fail(constants.CodeInvalidAction, messages.PlayInvalidAction, constants.StatusUnprocessable)
	}
	// Mashing the button is the expected use; excess taps are dropped quietly.
	if now-player.lastReactAt < reactionCooldownMs {
		return nil
	}
	player.lastReactAt = now
	envelope := Envelope{Type: ServerReaction, Reaction: &ReactionPayload{PlayerID: player.ID, Emoji: body.Emoji}}
	for _, sub := range r.subs {
		if sub.Audience.Kind == AudienceHost {
			sub.Send(envelope)
		}
	}
	return nil
}

// execute runs a game's effects. Fan-out is coalesced: however many broadcast
// effects a step emits, each connection gets one state message.
func (r *Room) execute(effects []Effect) {
	broadcastAll := false
	targeted := map[string]bool{}

	for _, effect := range effects {
		switch effect.Kind {
		case EffectBroadcast:
			broadcastAll = true
		case EffectToPlayer:
			targeted[effect.PlayerID] = true
		case EffectStartTimer:
			r.armTimer(effect.Key, effect.FireAt)
		case EffectClearTimer:
			r.clearTimer(effect.Key)
		case EffectGameEnded:
			r.reportFinished()
			broadcastAll = true
		}
	}

	if broadcastAll {
		r.broadcast()
		return
	}
	for _, sub := range r.subs {
		if sub.Audience.Kind == AudiencePlayer && targeted[sub.Audience.PlayerID] {
			r.sendState(sub)
		}
	}
}

func (r *Room) armTimer(key string, fireAt EpochMs) {
	r.clearTimer(key)
	gen := r.timerGen[key] + 1
	r.timerGen[key] = gen
	delay := time.Duration(max(0, fireAt-r.deps.now())) * time.Millisecond
	r.timers[key] = time.AfterFunc(delay, func() { r.fire(key, gen) })
}

func (r *Room) clearTimer(key string) {
	if t, ok := r.timers[key]; ok {
		t.Stop()
		delete(r.timers, key)
	}
	// Bumping the generation retires a timer that already fired but is still
	// waiting on the lock.
	r.timerGen[key]++
}

func (r *Room) fire(key string, gen int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.instance == nil || r.timerGen[key] != gen {
		return
	}
	delete(r.timers, key)
	r.execute(r.instance.OnTimer(key, r.deps.now()))
}

func (r *Room) stopGame() {
	for key := range r.timers {
		r.clearTimer(key)
	}
	r.instance = nil
	r.instanceID = ""
}

func (r *Room) reportFinished() {
	if r.reported || r.instance == nil || r.deps.onFinished == nil {
		return
	}
	r.reported = true
	finished := FinishedGame{
		RoomCode:   r.code,
		GameID:     r.game.Manifest().ID,
		InstanceID: r.instanceID,
		Config:     r.config,
		StartedAt:  r.startedAt,
		EndedAt:    r.deps.now(),
		Summary:    r.instance.Summary(),
	}
	// Write-behind: the family sees the podium now; the database catches up.
	go r.deps.onFinished(finished)
}

func (r *Room) close() {
	r.stopGame()
	r.phase = PhaseClosed
	for _, sub := range r.subs {
		sub.Send(Envelope{Type: ServerClosed})
	}
	if r.deps.onClosed != nil {
		go r.deps.onClosed(r.code)
	}
}

func (r *Room) rosterChanged(now EpochMs) {
	if r.instance != nil {
		effects := r.instance.OnRoster(r.roster(), now)
		r.execute(append(effects, Broadcast()))
		return
	}
	r.broadcast()
}

func (r *Room) roster() []RosterEntry {
	out := make([]RosterEntry, 0, len(r.players))
	for _, p := range r.players {
		out = append(out, RosterEntry{Player: p.ref(), Connected: p.conns > 0})
	}
	return out
}

func (r *Room) findPlayer(id string) *Player {
	if id == "" {
		return nil
	}
	for _, p := range r.players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (r *Room) broadcast() {
	for _, sub := range r.subs {
		r.sendState(sub)
	}
}

func (r *Room) sendState(sub *Subscriber) {
	state := &StatePayload{
		Now:  r.deps.now(),
		Room: r.view(),
		You:  YouView{Role: sub.Audience.Kind, PlayerID: sub.Audience.PlayerID},
	}
	if r.instance != nil {
		state.Game = r.instance.View(sub.Audience)
	}
	sub.Send(Envelope{Type: ServerState, State: state})
}

func (r *Room) view() RoomView {
	manifest := r.game.Manifest()
	players := make([]PlayerView, 0, len(r.players))
	for _, p := range r.players {
		players = append(players, PlayerView{ID: p.ID, Name: p.Name, Avatar: p.Avatar, Connected: p.conns > 0})
	}
	return RoomView{
		Code:          r.code,
		Phase:         r.phase,
		GameID:        manifest.ID,
		GameTitle:     manifest.Title,
		JoinURL:       r.deps.joinURL(r.code),
		HostConnected: r.hostConns > 0,
		Players:       players,
		Config:        r.config,
		Facts:         r.config.Facts(),
	}
}

// idleSince reports the last activity, whether anyone is still connected, and
// how long the room may sit empty. A private room is one person's game or one
// duel: once they have gone there is nobody to come back to it.
func (r *Room) idleSince() (EpochMs, bool, EpochMs) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ttl := EpochMs(roomIdleMs)
	if r.private {
		ttl = privateRoomIdleMs
	}
	return r.lastActivity, len(r.subs) > 0, ttl
}

// ErrorEnvelope builds the server.error message for a failure.
func ErrorEnvelope(failure *result.Failure) Envelope {
	body := response.BodyOf(failure)
	return Envelope{Type: ServerError, Error: &body}
}
