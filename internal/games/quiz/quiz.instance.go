package quiz

import (
	"encoding/json"
	"math/rand"
	"sort"
	"strings"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

type playerState struct {
	ref       engine.PlayerRef
	connected bool
	order     int

	score      int
	roundScore int
	streak     int
	bestStreak int
	correct    int
	answered   int
	// Time taken over right answers only: the speed award is for being fast
	// AND right, not for guessing quickly.
	correctMs int64

	prevRank   int
	lastDelta  int
	lastResult AnswerResult
	lastMs     int
}

type answer struct {
	choice int
	at     engine.EpochMs
}

type dealt struct {
	prompt     string
	options    []string
	answerIdx  int
	category   questions.Category
	difficulty questions.Difficulty
}

// instance is one running quiz. The room holds its lock around every call, so
// nothing here locks. It never reads the clock: `now` always arrives as an
// argument, which is what lets a whole game be replayed in a test.
type instance struct {
	cfg  Config
	deck *questions.Deck
	rng  *rand.Rand

	players map[string]*playerState
	phase   Phase

	round          int
	questionInRnd  int
	questionSeq    int
	questionsAsked int
	roundsPlayed   int

	current        *dealt
	answers        map[string]answer
	phaseStartedAt engine.EpochMs
	deadline       engine.EpochMs
	reveal         *RevealView

	// Players shown the answer to the question in play.
	hints map[string]bool
	// A paused game remembers when it stopped and how long its phase had left.
	paused       bool
	pausedAt     engine.EpochMs
	pausedLeftMs int64
}

// alone reports a one-player game: the only kind that can be paused, and the
// only kind where a reveal can be bought.
func (g *instance) alone() bool { return len(g.players) == 1 }

func (g *instance) pause(now engine.EpochMs) []engine.Effect {
	if g.paused || g.phase == PhaseFinished || g.deadline <= 0 {
		return nil
	}
	g.paused = true
	g.pausedAt = now
	g.pausedLeftMs = max(0, g.deadline-now)
	return []engine.Effect{engine.ClearTimer(timerPhase), engine.Broadcast()}
}

// resume restarts the clock with exactly the time it had left, and moves the
// phase start forward by the length of the pause so that time is not counted
// against the player's answer speed.
func (g *instance) resume(now engine.EpochMs) []engine.Effect {
	if !g.paused {
		return nil
	}
	g.paused = false
	g.phaseStartedAt += now - g.pausedAt
	g.deadline = now + g.pausedLeftMs
	return []engine.Effect{engine.StartTimer(timerPhase, g.deadline), engine.Broadcast()}
}

// onReveal marks the right answer as shown to this player.
func (g *instance) onReveal(payload json.RawMessage, ctx engine.ActionCtx) ([]engine.Effect, *result.Failure) {
	var body struct {
		QuestionSeq int `json:"question_seq"`
	}
	_ = json.Unmarshal(payload, &body)
	if !ctx.Trusted || !g.alone() || g.players[ctx.Actor.ID] == nil {
		return nil, invalidAction()
	}
	if g.phase != PhaseQuestion || g.current == nil || body.QuestionSeq != g.questionSeq {
		return nil, invalidAction()
	}
	if _, answered := g.answers[ctx.Actor.ID]; answered || g.hints[ctx.Actor.ID] {
		return nil, invalidAction()
	}
	g.hints[ctx.Actor.ID] = true
	return []engine.Effect{engine.Broadcast()}, nil
}

// enter moves to a phase. A zero duration means "wait for the host".
func (g *instance) enter(phase Phase, now engine.EpochMs, durationMs int64) []engine.Effect {
	g.phase = phase
	g.phaseStartedAt = now
	if durationMs <= 0 {
		g.deadline = 0
		return []engine.Effect{engine.ClearTimer(timerPhase), engine.Broadcast()}
	}
	g.deadline = now + durationMs
	return []engine.Effect{engine.StartTimer(timerPhase, g.deadline), engine.Broadcast()}
}

// paced returns the duration for a phase the host may be driving by hand.
func (g *instance) paced(durationMs int64) int64 {
	if g.cfg.Pacing == PacingHost {
		return 0
	}
	return durationMs
}

func (g *instance) syncRoster(roster []engine.RosterEntry) {
	for _, entry := range roster {
		state, ok := g.players[entry.Player.ID]
		if !ok {
			state = &playerState{ref: entry.Player, order: len(g.players), lastResult: ResultNone}
			g.players[entry.Player.ID] = state
		}
		state.connected = entry.Connected
	}
}

func (g *instance) OnRoster(roster []engine.RosterEntry, now engine.EpochMs) []engine.Effect {
	g.syncRoster(roster)
	// The last player we were waiting on may have just dropped off.
	if g.phase == PhaseQuestion && g.everyoneAnswered() {
		return g.closeQuestion(now)
	}
	return nil
}

func (g *instance) OnTimer(key string, now engine.EpochMs) []engine.Effect {
	if key != timerPhase {
		return nil
	}
	return g.advance(now)
}

func invalidAction() *result.Failure {
	return &result.Failure{
		Code:       constants.CodeInvalidAction,
		MessageKey: messages.PlayInvalidAction,
		Status:     constants.StatusUnprocessable,
	}
}

func (g *instance) OnAction(action engine.Action, ctx engine.ActionCtx) ([]engine.Effect, *result.Failure) {
	switch action.Type {
	case ActionNext:
		if !ctx.IsHost {
			return nil, &result.Failure{Code: constants.CodeNotHost, MessageKey: messages.RoomsNotHost, Status: constants.StatusForbidden}
		}
		return g.advance(ctx.Now), nil

	case ActionEndGame:
		if !ctx.IsHost {
			return nil, &result.Failure{Code: constants.CodeNotHost, MessageKey: messages.RoomsNotHost, Status: constants.StatusForbidden}
		}
		return g.finish(ctx.Now), nil

	case ActionPause, ActionResume:
		if !ctx.IsHost || !g.alone() {
			return nil, &result.Failure{Code: constants.CodeNotHost, MessageKey: messages.RoomsNotHost, Status: constants.StatusForbidden}
		}
		if action.Type == ActionPause {
			return g.pause(ctx.Now), nil
		}
		return g.resume(ctx.Now), nil

	case ActionReveal:
		return g.onReveal(action.Payload, ctx)

	case ActionAnswer:
		return g.onAnswer(action.Payload, ctx)
	}
	return nil, invalidAction()
}

func (g *instance) onAnswer(payload json.RawMessage, ctx engine.ActionCtx) ([]engine.Effect, *result.Failure) {
	var body struct {
		QuestionSeq int  `json:"question_seq"`
		Choice      *int `json:"choice"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.Choice == nil {
		return nil, invalidAction()
	}
	if ctx.Role != engine.AudiencePlayer || g.players[ctx.Actor.ID] == nil {
		return nil, invalidAction()
	}
	if *body.Choice < 0 || *body.Choice >= OptionsPerQuery {
		return nil, invalidAction()
	}

	// Late, duplicate and stale answers are ordinary at a party — a tap that
	// lands just after the buzzer. They are dropped without an error.
	if g.phase != PhaseQuestion || body.QuestionSeq != g.questionSeq {
		return nil, nil
	}
	if _, already := g.answers[ctx.Actor.ID]; already {
		return nil, nil
	}

	g.answers[ctx.Actor.ID] = answer{choice: *body.Choice, at: ctx.Now}
	if g.everyoneAnswered() {
		return g.closeQuestion(ctx.Now), nil
	}
	return []engine.Effect{engine.Broadcast()}, nil
}

func (g *instance) everyoneAnswered() bool {
	active := 0
	for id, p := range g.players {
		if !p.connected {
			continue
		}
		active++
		if _, ok := g.answers[id]; !ok {
			return false
		}
	}
	return active > 0
}

// advance is what a timer firing and the host pressing Next both do.
func (g *instance) advance(now engine.EpochMs) []engine.Effect {
	switch g.phase {
	case PhaseIntro, PhaseLeaderboard:
		return g.ask(now)
	case PhaseQuestion:
		return g.closeQuestion(now)
	case PhaseReveal:
		lastRound := !g.cfg.Endless && g.round >= g.cfg.Rounds
		if g.questionInRnd >= g.cfg.QuestionsPerRound {
			g.roundsPlayed = g.round
			if g.cfg.Quick && lastRound {
				// On one phone there is nothing to gain from a "round done"
				// screen straight before the final one.
				return g.finish(now)
			}
			return g.enter(PhaseRoundEnd, now, g.paced(g.tempo(roundEndMs, quickRoundEndMs)))
		}
		if g.cfg.Quick {
			// The standings ride along on the reveal; no separate scoreboard.
			return g.ask(now)
		}
		return g.enter(PhaseLeaderboard, now, g.paced(leaderboardMs))
	case PhaseRoundEnd:
		if !g.cfg.Endless && g.round >= g.cfg.Rounds {
			return g.finish(now)
		}
		g.round++
		g.questionInRnd = 0
		for _, p := range g.players {
			p.roundScore = 0
		}
		return g.enter(PhaseIntro, now, g.tempo(introMs, quickIntroMs))
	}
	return nil
}

// tempo picks the party length or the quick one.
func (g *instance) tempo(party, quick int64) int64 {
	if g.cfg.Quick {
		return quick
	}
	return party
}

func (g *instance) ask(now engine.EpochMs) []engine.Effect {
	question, ok := g.deck.Draw()
	if !ok {
		return g.finish(now)
	}

	// The right answer gets a fresh random position every time it is asked.
	options := append([]string{question.Correct}, question.Wrong...)
	g.rng.Shuffle(len(options), func(i, j int) { options[i], options[j] = options[j], options[i] })
	answerIdx := 0
	for i, option := range options {
		if option == question.Correct {
			answerIdx = i
		}
	}

	category, _ := questions.FindCategory(question.Category)
	g.current = &dealt{
		prompt:     question.Prompt,
		options:    options,
		answerIdx:  answerIdx,
		category:   category,
		difficulty: question.Difficulty,
	}
	g.answers = map[string]answer{}
	g.hints = map[string]bool{}
	g.reveal = nil
	g.questionInRnd++
	g.questionSeq++
	for _, p := range g.players {
		p.lastDelta = 0
		p.lastResult = ResultNone
		p.lastMs = 0
	}

	return g.enter(PhaseQuestion, now, int64(g.cfg.SecondsPerQuestion)*1000)
}

// pointsFor scores one right answer. elapsedMs is how long the player took.
func pointsFor(elapsedMs, windowMs int64, streak int) int {
	if windowMs <= 0 {
		return BasePoints
	}
	elapsedMs = max(0, min(elapsedMs, windowMs))
	speed := int((windowMs - elapsedMs) * SpeedPoints / windowMs)
	bonus := min(max(0, streak-1)*StreakStep, StreakBonusCap)
	return BasePoints + speed + bonus
}

func (g *instance) closeQuestion(now engine.EpochMs) []engine.Effect {
	if g.phase != PhaseQuestion || g.current == nil {
		return nil
	}

	for _, row := range g.board() {
		g.players[row.PlayerID].prevRank = row.Rank
	}

	windowMs := int64(g.cfg.SecondsPerQuestion) * 1000
	counts := make([]int, len(g.current.options))
	var fastest *FastestView

	for id, p := range g.players {
		given, answered := g.answers[id]
		if !answered {
			// Sitting one out breaks a streak; it does not count as an attempt.
			p.streak = 0
			p.lastResult = ResultNone
			continue
		}
		p.answered++
		counts[given.choice]++
		elapsed := max(0, given.at-g.phaseStartedAt)
		p.lastMs = int(elapsed)

		if given.choice != g.current.answerIdx {
			p.streak = 0
			p.lastResult = ResultWrong
			continue
		}

		p.streak++
		p.bestStreak = max(p.bestStreak, p.streak)
		p.correct++
		p.correctMs += elapsed
		points := pointsFor(elapsed, windowMs, p.streak)
		if g.hints[id] {
			// A bought answer is worth the base only: no speed, no streak
			// bonus. Knowing it still has to pay better than buying it.
			points = BasePoints
		}
		p.lastDelta = points * g.cfg.MultiplierPct() / 100
		p.score += p.lastDelta
		p.roundScore += p.lastDelta
		p.lastResult = ResultCorrect

		if fastest == nil || int(elapsed) < fastest.Ms || (int(elapsed) == fastest.Ms && p.ref.Name < fastest.Name) {
			fastest = &FastestView{PlayerID: id, Name: p.ref.Name, Avatar: p.ref.Avatar, Ms: int(elapsed)}
		}
	}

	g.questionsAsked++
	g.reveal = &RevealView{AnswerIdx: g.current.answerIdx, Counts: counts, Fastest: fastest}
	return g.enter(PhaseReveal, now, g.paced(g.tempo(revealMs, quickRevealMs)))
}

func (g *instance) finish(now engine.EpochMs) []engine.Effect {
	if g.phase == PhaseFinished {
		return nil
	}
	if g.questionsAsked > 0 && g.roundsPlayed < g.round && g.questionInRnd > 0 {
		g.roundsPlayed = g.round
	}
	g.phase = PhaseFinished
	g.phaseStartedAt = now
	g.deadline = 0
	g.current = nil
	g.reveal = nil
	return []engine.Effect{engine.ClearTimer(timerPhase), engine.GameEnded()}
}

func (g *instance) IsOver() bool { return g.phase == PhaseFinished }

// board is every player, best first. Ties share a rank (1, 2, 2, 4) and are
// ordered by name so the list does not shuffle between broadcasts.
func (g *instance) board() []BoardRow {
	rows := make([]BoardRow, 0, len(g.players))
	for id, p := range g.players {
		rows = append(rows, BoardRow{
			PlayerID:   id,
			Name:       p.ref.Name,
			Avatar:     p.ref.Avatar,
			Score:      p.score,
			RoundScore: p.roundScore,
			Delta:      p.lastDelta,
			PrevRank:   p.prevRank,
			Streak:     p.streak,
			Result:     p.lastResult,
			Connected:  p.connected,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		a, b := strings.ToLower(rows[i].Name), strings.ToLower(rows[j].Name)
		if a != b {
			return a < b
		}
		return g.players[rows[i].PlayerID].order < g.players[rows[j].PlayerID].order
	})
	for i := range rows {
		rows[i].Rank = i + 1
		if i > 0 && rows[i].Score == rows[i-1].Score {
			rows[i].Rank = rows[i-1].Rank
		}
		if rows[i].PrevRank == 0 {
			rows[i].PrevRank = rows[i].Rank
		}
	}
	return rows
}

func (g *instance) View(audience engine.Audience) engine.View {
	board := g.board()
	totalRounds := g.cfg.Rounds
	if g.cfg.Endless {
		totalRounds = 0
	}

	view := View{
		Game:              GameID,
		Phase:             g.phase,
		Pacing:            g.cfg.Pacing,
		Round:             g.round,
		TotalRounds:       totalRounds,
		QuestionInRound:   g.questionInRnd,
		QuestionsPerRound: g.cfg.QuestionsPerRound,
		QuestionSeq:       g.questionSeq,
		QuestionsAsked:    g.questionsAsked,
		MultiplierPct:     g.cfg.MultiplierPct(),
		Quick:             g.cfg.Quick,
		PhaseStartedAt:    g.phaseStartedAt,
		Deadline:          g.deadline,
		AnsweredIDs:       []string{},
		Board:             board,
		Paused:            g.paused,
	}
	if g.paused {
		// A frozen clock: the deadline is shown as if the pause began now.
		view.PausedLeftMs = g.pausedLeftMs
	}
	if g.alone() && g.cfg.Quick && g.phase == PhaseQuestion {
		view.RevealCost = g.cfg.RevealCost(g.questionInRnd)
	}

	for id, p := range g.players {
		if p.connected {
			view.ActiveCount++
		}
		if _, ok := g.answers[id]; ok && (g.phase == PhaseQuestion || g.phase == PhaseReveal) {
			view.AnsweredIDs = append(view.AnsweredIDs, id)
		}
	}
	sort.Strings(view.AnsweredIDs)

	if g.current != nil && (g.phase == PhaseQuestion || g.phase == PhaseReveal) {
		view.Question = &QuestionView{
			Prompt:        g.current.prompt,
			Options:       g.current.options,
			Category:      g.current.category.Slug,
			CategoryLabel: g.current.category.Label,
			CategoryEmoji: g.current.category.Emoji,
			Difficulty:    string(g.current.difficulty),
		}
	}
	// Answer secrecy: the reveal is attached only once the question is closed.
	if g.phase == PhaseReveal {
		view.Reveal = g.reveal
	}
	if g.phase == PhaseFinished {
		view.Final = g.final(board)
	}

	if audience.Kind == engine.AudiencePlayer {
		if p, ok := g.players[audience.PlayerID]; ok {
			you := &YouView{Result: ResultNone, Score: p.score, Streak: p.streak}
			for _, row := range board {
				if row.PlayerID == audience.PlayerID {
					you.Rank = row.Rank
				}
			}
			if given, answered := g.answers[audience.PlayerID]; answered && (g.phase == PhaseQuestion || g.phase == PhaseReveal) {
				choice := given.choice
				you.Answered = true
				you.Choice = &choice
			}
			if g.hints[audience.PlayerID] && g.current != nil && (g.phase == PhaseQuestion || g.phase == PhaseReveal) {
				you.Hinted = true
				if g.phase == PhaseQuestion {
					idx := g.current.answerIdx
					you.HintIdx = &idx
				}
			}
			// Whether it was right is as secret as the answer itself.
			if g.phase != PhaseQuestion && g.phase != PhaseIntro {
				you.Result = p.lastResult
				you.Points = p.lastDelta
				you.SpeedMs = p.lastMs
			}
			view.You = you
		}
	}
	return view
}

func (g *instance) stats(board []BoardRow) []StatRow {
	rows := make([]StatRow, 0, len(board))
	for _, row := range board {
		p := g.players[row.PlayerID]
		stat := StatRow{
			PlayerID:   row.PlayerID,
			Name:       row.Name,
			Avatar:     row.Avatar,
			Rank:       row.Rank,
			Score:      p.score,
			Correct:    p.correct,
			Answered:   p.answered,
			BestStreak: p.bestStreak,
		}
		// Accuracy is out of every question asked: skipping one is not a way
		// to keep a perfect record.
		if g.questionsAsked > 0 {
			stat.AccuracyPct = p.correct * 100 / g.questionsAsked
		}
		if p.correct > 0 {
			stat.AvgMs = int(p.correctMs / int64(p.correct))
		}
		rows = append(rows, stat)
	}
	return rows
}

func (g *instance) final(board []BoardRow) *FinalView {
	stats := g.stats(board)
	final := &FinalView{
		TotalQuestions: g.questionsAsked,
		RoundsPlayed:   g.roundsPlayed,
		Awards:         []Award{},
		Stats:          stats,
	}
	if g.questionsAsked == 0 {
		return final
	}

	// stats is in board order, so on a tie the higher-placed player wins.
	var fastest, sharpest, hottest *StatRow
	for i := range stats {
		s := &stats[i]
		if s.Correct > 0 && (fastest == nil || s.AvgMs < fastest.AvgMs) {
			fastest = s
		}
		if s.Correct > 0 && (sharpest == nil || s.Correct > sharpest.Correct) {
			sharpest = s
		}
		if s.BestStreak >= 2 && (hottest == nil || s.BestStreak > hottest.BestStreak) {
			hottest = s
		}
	}

	if fastest != nil {
		final.Awards = append(final.Awards, award(AwardSpeedDemon, "Speed Demon", "Quickest on the right answers", fastest, formatSeconds(fastest.AvgMs)+" average"))
	}
	if sharpest != nil {
		final.Awards = append(final.Awards, award(AwardSharpshooter, "Sharpshooter", "Most answers right", sharpest, formatRatio(sharpest.Correct, g.questionsAsked)))
	}
	if hottest != nil {
		final.Awards = append(final.Awards, award(AwardOnFire, "On Fire", "Longest run of right answers", hottest, formatStreak(hottest.BestStreak)))
	}
	return final
}

func award(key, title, blurb string, s *StatRow, value string) Award {
	return Award{Key: key, Title: title, Blurb: blurb, PlayerID: s.PlayerID, Name: s.Name, Avatar: s.Avatar, Value: value}
}

func (g *instance) Summary() engine.Summary {
	stats := g.stats(g.board())
	summary := engine.Summary{TotalQuestions: g.questionsAsked, RoundsPlayed: g.roundsPlayed}
	for _, s := range stats {
		summary.Players = append(summary.Players, engine.SummaryPlayer{
			PlayerID:    s.PlayerID,
			Name:        s.Name,
			Avatar:      s.Avatar,
			Rank:        s.Rank,
			Score:       s.Score,
			Correct:     s.Correct,
			Answered:    s.Answered,
			AvgAnswerMs: s.AvgMs,
			BestStreak:  s.BestStreak,
		})
	}
	return summary
}
