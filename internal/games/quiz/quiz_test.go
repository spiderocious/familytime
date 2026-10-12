package quiz

import (
	"encoding/json"
	"fmt"
	"testing"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/shared/constants"
)

const t0 engine.EpochMs = 1_000_000

func fixtureBank(n int) *questions.Bank {
	bank := questions.NewBank()
	all := []questions.Question{}
	for i := 0; i < n; i++ {
		difficulty := questions.Difficulties[i%len(questions.Difficulties)]
		category := questions.CategoryGeneral
		if i%2 == 1 {
			category = questions.CategoryScience
		}
		all = append(all, questions.Question{
			ID:         fmt.Sprintf("q_%03d", i),
			Category:   category,
			Difficulty: difficulty,
			Prompt:     fmt.Sprintf("Question %d?", i),
			Correct:    fmt.Sprintf("right-%d", i),
			Wrong:      []string{fmt.Sprintf("a-%d", i), fmt.Sprintf("b-%d", i), fmt.Sprintf("c-%d", i)},
		})
	}
	bank.Replace(all)
	return bank
}

var (
	ada = engine.PlayerRef{ID: "pl_ada", Name: "Ada", Avatar: "🦁"}
	bem = engine.PlayerRef{ID: "pl_bem", Name: "Bem", Avatar: "🐼"}
)

func newGame(t *testing.T, mutate func(*Config)) *instance {
	t.Helper()
	cfg := defaultConfig()
	cfg.Rounds = 2
	cfg.QuestionsPerRound = 3
	cfg.SecondsPerQuestion = 20
	if mutate != nil {
		mutate(&cfg)
	}
	game := NewGame(fixtureBank(30))
	inst, effects := game.New(engine.InitInput{
		Config: cfg,
		Roster: []engine.RosterEntry{{Player: ada, Connected: true}, {Player: bem, Connected: true}},
		Seed:   42,
		Now:    t0,
	})
	if len(effects) == 0 {
		t.Fatal("a new game must arm its first timer")
	}
	return inst.(*instance)
}

func answerPayload(seq, choice int) json.RawMessage {
	raw, _ := json.Marshal(map[string]int{"question_seq": seq, "choice": choice})
	return raw
}

func (g *instance) wrongChoice() int { return (g.current.answerIdx + 1) % OptionsPerQuery }

func submit(t *testing.T, g *instance, who engine.PlayerRef, choice int, at engine.EpochMs) {
	t.Helper()
	_, failure := g.OnAction(
		engine.Action{Type: ActionAnswer, Payload: answerPayload(g.questionSeq, choice)},
		engine.ActionCtx{Role: engine.AudiencePlayer, Actor: who, Now: at},
	)
	if failure != nil {
		t.Fatalf("answer rejected: %s", failure.Code)
	}
}

func host(now engine.EpochMs) engine.ActionCtx {
	return engine.ActionCtx{Role: engine.AudienceHost, IsHost: true, Now: now}
}

func score(g *instance, who engine.PlayerRef) int { return g.players[who.ID].score }

func TestPointsReward_SpeedAsMuchAsCorrectness(t *testing.T) {
	cases := []struct {
		name      string
		elapsed   int64
		streak    int
		wantScore int
	}{
		{"instant answer earns everything", 0, 1, 1000},
		{"half the clock earns half the speed points", 10_000, 1, 750},
		{"the very last moment still earns the base", 20_000, 1, 500},
		{"past the clock is clamped, never negative", 99_000, 1, 500},
		{"second in a row adds one streak step", 0, 2, 1100},
		{"streak bonus is capped", 0, 50, 1500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pointsFor(tc.elapsed, 20_000, tc.streak); got != tc.wantScore {
				t.Fatalf("got %d, want %d", got, tc.wantScore)
			}
		})
	}
}

func TestQuestion_ScoresAndRevealsOnceEveryoneAnswers(t *testing.T) {
	g := newGame(t, nil)
	g.OnTimer(timerPhase, t0+introMs) // intro → first question
	if g.phase != PhaseQuestion {
		t.Fatalf("phase = %s, want question", g.phase)
	}
	start := g.phaseStartedAt

	// While the question is open, nobody may learn the answer.
	if view := g.View(engine.Audience{Kind: engine.AudienceHost}).(View); view.Reveal != nil {
		t.Fatal("the host view leaked the answer during the question")
	}

	submit(t, g, ada, g.current.answerIdx, start+2_000)
	if g.phase != PhaseQuestion {
		t.Fatal("the question closed before everyone answered")
	}
	you := g.View(engine.Audience{Kind: engine.AudiencePlayer, PlayerID: ada.ID}).(View).You
	if !you.Answered || you.Result != ResultNone || you.Points != 0 {
		t.Fatalf("a player learned their result early: %+v", you)
	}

	submit(t, g, bem, g.wrongChoice(), start+3_000)
	if g.phase != PhaseReveal {
		t.Fatalf("phase = %s, want reveal once everyone has answered", g.phase)
	}

	// 2s of a 20s clock: 500 base + 450 speed.
	if got := score(g, ada); got != 950 {
		t.Fatalf("Ada scored %d, want 950", got)
	}
	if got := score(g, bem); got != 0 {
		t.Fatalf("Bem scored %d for a wrong answer, want 0", got)
	}

	view := g.View(engine.Audience{Kind: engine.AudiencePlayer, PlayerID: bem.ID}).(View)
	if view.Reveal == nil || view.Reveal.AnswerIdx != g.current.answerIdx {
		t.Fatal("the reveal must carry the right answer")
	}
	if view.Reveal.Fastest == nil || view.Reveal.Fastest.PlayerID != ada.ID || view.Reveal.Fastest.Ms != 2_000 {
		t.Fatalf("fastest = %+v, want Ada at 2000ms", view.Reveal.Fastest)
	}
	if view.You.Result != ResultWrong || view.You.Rank != 2 {
		t.Fatalf("Bem's view = %+v, want wrong and rank 2", view.You)
	}
	if view.Board[0].PlayerID != ada.ID || view.Board[0].Delta != 950 {
		t.Fatalf("board leader = %+v, want Ada +950", view.Board[0])
	}
}

func TestFasterRightAnswer_BeatsSlowerRightAnswer(t *testing.T) {
	g := newGame(t, nil)
	g.OnTimer(timerPhase, t0+introMs)
	start := g.phaseStartedAt

	submit(t, g, ada, g.current.answerIdx, start+1_000)
	submit(t, g, bem, g.current.answerIdx, start+15_000)

	if score(g, ada) <= score(g, bem) {
		t.Fatalf("Ada (%d) answered first and should outscore Bem (%d)", score(g, ada), score(g, bem))
	}
	if score(g, bem) < BasePoints {
		t.Fatal("a right answer must always earn at least the base points")
	}
}

func TestStreak_BuildsAndBreaksOnAMiss(t *testing.T) {
	g := newGame(t, func(c *Config) { c.QuestionsPerRound = 5; c.Rounds = 1 })
	g.OnTimer(timerPhase, t0+introMs)
	now := g.phaseStartedAt

	play := func(adaRight bool) {
		choice := g.current.answerIdx
		if !adaRight {
			choice = g.wrongChoice()
		}
		submit(t, g, ada, choice, g.phaseStartedAt)
		submit(t, g, bem, g.wrongChoice(), g.phaseStartedAt)
		now += 10_000
		g.OnTimer(timerPhase, now) // reveal → leaderboard
		now += 10_000
		g.OnTimer(timerPhase, now) // leaderboard → next question
	}

	play(true)
	play(true)
	if got := score(g, ada); got != 1000+1100 {
		t.Fatalf("after two instant right answers Ada has %d, want 2100", got)
	}
	play(false)
	if g.players[ada.ID].streak != 0 {
		t.Fatal("a wrong answer must reset the streak")
	}
	play(true)
	if got := score(g, ada); got != 2100+1000 {
		t.Fatalf("after the miss the streak bonus should be gone; Ada has %d", got)
	}
	if g.players[ada.ID].bestStreak != 2 {
		t.Fatalf("best streak = %d, want 2", g.players[ada.ID].bestStreak)
	}
}

func TestRounds_RunToTheFinalSummary(t *testing.T) {
	g := newGame(t, nil) // 2 rounds × 3 questions
	now := t0 + introMs
	g.OnTimer(timerPhase, now)

	for g.phase != PhaseFinished {
		switch g.phase {
		case PhaseQuestion:
			submit(t, g, ada, g.current.answerIdx, g.phaseStartedAt+1_000)
			submit(t, g, bem, g.wrongChoice(), g.phaseStartedAt+1_000)
		default:
			now += 30_000
			g.OnTimer(timerPhase, now)
		}
	}

	summary := g.Summary()
	if summary.TotalQuestions != 6 || summary.RoundsPlayed != 2 {
		t.Fatalf("summary = %d questions over %d rounds, want 6 over 2", summary.TotalQuestions, summary.RoundsPlayed)
	}
	winner := summary.Players[0]
	if winner.Name != "Ada" || winner.Rank != 1 || winner.Correct != 6 || winner.Answered != 6 || winner.BestStreak != 6 {
		t.Fatalf("winner = %+v", winner)
	}
	if winner.AvgAnswerMs != 1_000 {
		t.Fatalf("average answer time = %dms, want 1000", winner.AvgAnswerMs)
	}
	loser := summary.Players[1]
	if loser.Rank != 2 || loser.Score != 0 || loser.Correct != 0 || loser.Answered != 6 {
		t.Fatalf("second place = %+v", loser)
	}

	final := g.View(engine.Audience{Kind: engine.AudienceHost}).(View).Final
	if final == nil || len(final.Awards) != 3 {
		t.Fatalf("final awards = %+v, want three", final)
	}
	for _, award := range final.Awards {
		if award.PlayerID != ada.ID {
			t.Fatalf("award %s went to %s, want Ada", award.Key, award.Name)
		}
	}
	if final.Stats[0].AccuracyPct != 100 || final.Stats[1].AccuracyPct != 0 {
		t.Fatalf("accuracy = %d / %d", final.Stats[0].AccuracyPct, final.Stats[1].AccuracyPct)
	}
}

func TestHostPacing_WaitsForNext(t *testing.T) {
	g := newGame(t, func(c *Config) { c.Pacing = PacingHost })
	g.OnTimer(timerPhase, t0+introMs)
	submit(t, g, ada, g.current.answerIdx, g.phaseStartedAt)
	submit(t, g, bem, g.current.answerIdx, g.phaseStartedAt)

	if g.phase != PhaseReveal || g.deadline != 0 {
		t.Fatalf("host-paced reveal should have no deadline; phase=%s deadline=%d", g.phase, g.deadline)
	}

	_, failure := g.OnAction(engine.Action{Type: ActionNext}, engine.ActionCtx{Role: engine.AudiencePlayer, Actor: ada, Now: t0})
	if failure == nil || failure.Code != constants.CodeNotHost {
		t.Fatal("a player must not be able to press Next")
	}

	if _, failure := g.OnAction(engine.Action{Type: ActionNext}, host(t0+60_000)); failure != nil {
		t.Fatalf("host Next rejected: %s", failure.Code)
	}
	if g.phase != PhaseLeaderboard {
		t.Fatalf("phase = %s, want leaderboard after Next", g.phase)
	}
}

func TestEndless_RunsUntilTheHostEndsIt(t *testing.T) {
	g := newGame(t, func(c *Config) { c.Endless = true; c.Rounds = 1; c.QuestionsPerRound = 3 })
	now := t0 + introMs
	g.OnTimer(timerPhase, now)

	// Well past the configured single round, and past the 30-question bank.
	for g.questionsAsked < 40 {
		if g.phase == PhaseQuestion {
			submit(t, g, ada, g.current.answerIdx, g.phaseStartedAt)
			submit(t, g, bem, g.current.answerIdx, g.phaseStartedAt)
			continue
		}
		now += 30_000
		g.OnTimer(timerPhase, now)
		if g.phase == PhaseFinished {
			t.Fatal("an endless game finished on its own")
		}
	}
	if view := g.View(engine.Audience{Kind: engine.AudienceHost}).(View); view.TotalRounds != 0 {
		t.Fatalf("endless games report total_rounds 0, got %d", view.TotalRounds)
	}

	effects, failure := g.OnAction(engine.Action{Type: ActionEndGame}, host(now))
	if failure != nil || !g.IsOver() {
		t.Fatal("the host could not end an endless game")
	}
	ended := false
	for _, e := range effects {
		ended = ended || e.Kind == engine.EffectGameEnded
	}
	if !ended {
		t.Fatal("ending the game must emit GameEnded so the result is recorded")
	}
	if g.Summary().TotalQuestions != 40 {
		t.Fatalf("total questions = %d, want 40", g.Summary().TotalQuestions)
	}
}

func TestAnswers_LateDuplicateAndMalformed(t *testing.T) {
	g := newGame(t, nil)
	g.OnTimer(timerPhase, t0+introMs)
	start := g.phaseStartedAt

	submit(t, g, ada, g.wrongChoice(), start+1_000)
	// Changing your mind is not allowed: the first answer stands.
	submit(t, g, ada, g.current.answerIdx, start+2_000)
	if g.answers[ada.ID].choice == g.current.answerIdx {
		t.Fatal("a second answer overwrote the first")
	}

	for _, raw := range []string{`{"question_seq":1,"choice":9}`, `{"question_seq":1}`, `not json`} {
		_, failure := g.OnAction(engine.Action{Type: ActionAnswer, Payload: json.RawMessage(raw)}, engine.ActionCtx{Role: engine.AudiencePlayer, Actor: bem, Now: start})
		if failure == nil || failure.Code != constants.CodeInvalidAction {
			t.Fatalf("payload %s should be rejected as invalid_action", raw)
		}
	}

	// The clock runs out with Bem silent: no attempt recorded, streak stays 0.
	g.OnTimer(timerPhase, start+20_000)
	if g.phase != PhaseReveal || g.players[bem.ID].answered != 0 {
		t.Fatalf("phase=%s bem.answered=%d", g.phase, g.players[bem.ID].answered)
	}

	// An answer for the closed question arrives late: dropped, no error.
	_, failure := g.OnAction(engine.Action{Type: ActionAnswer, Payload: answerPayload(1, 0)}, engine.ActionCtx{Role: engine.AudiencePlayer, Actor: bem, Now: start + 21_000})
	if failure != nil {
		t.Fatal("a late answer should be a silent no-op, not an error")
	}
}

func TestRoster_LateJoinerPlaysAndDropoutDoesNotStallTheRoom(t *testing.T) {
	g := newGame(t, nil)
	g.OnTimer(timerPhase, t0+introMs)
	cy := engine.PlayerRef{ID: "pl_cy", Name: "Cy", Avatar: "🐸"}

	g.OnRoster([]engine.RosterEntry{{Player: ada, Connected: true}, {Player: bem, Connected: true}, {Player: cy, Connected: true}}, t0)
	if g.players[cy.ID] == nil {
		t.Fatal("a late joiner was not added")
	}

	submit(t, g, ada, g.current.answerIdx, g.phaseStartedAt)
	submit(t, g, cy, g.current.answerIdx, g.phaseStartedAt)
	if g.phase != PhaseQuestion {
		t.Fatal("still waiting on Bem")
	}
	// Bem's phone dies. Nobody connected is left to answer, so the room moves on.
	g.OnRoster([]engine.RosterEntry{{Player: ada, Connected: true}, {Player: bem, Connected: false}, {Player: cy, Connected: true}}, t0)
	if g.phase != PhaseReveal {
		t.Fatalf("phase = %s; a dropped player must not hold the question open", g.phase)
	}
}

func TestConfig_DefaultsAndEveryInvalidFieldReported(t *testing.T) {
	cfg, errs := parseConfig(nil)
	if len(errs) != 0 || cfg.Rounds != DefaultRounds || cfg.Pacing != PacingAuto || cfg.Difficulty != DifficultyMixed {
		t.Fatalf("defaults = %+v errs=%v", cfg, errs)
	}

	_, errs = parseConfig(json.RawMessage(`{"difficulty":"impossible","categories":["nope"],"rounds":0,"questions_per_round":99,"seconds_per_question":1,"pacing":"warp"}`))
	for _, field := range []string{"difficulty", "categories", "rounds", "questions_per_round", "seconds_per_question", "pacing"} {
		if len(errs[field]) == 0 {
			t.Errorf("no error reported for %s", field)
		}
	}

	all := []string{}
	for _, c := range questions.Categories {
		all = append(all, c.Slug)
	}
	raw, _ := json.Marshal(map[string][]string{"categories": all})
	cfg, _ = parseConfig(raw)
	if len(cfg.Categories) != 0 {
		t.Fatal("choosing every category should normalise to 'everything'")
	}
}

func TestCanStart_RefusesWhenNothingMatches(t *testing.T) {
	game := NewGame(fixtureBank(6))
	cfg := defaultConfig()
	cfg.Categories = []string{questions.CategoryBible}
	if failure := game.CanStart(cfg); failure == nil || failure.Code != constants.CodeNoQuestions {
		t.Fatal("a filter matching no questions must refuse to start")
	}
	cfg.Categories = []string{questions.CategoryScience}
	if failure := game.CanStart(cfg); failure != nil {
		t.Fatalf("science has questions but start was refused: %s", failure.Code)
	}
}
