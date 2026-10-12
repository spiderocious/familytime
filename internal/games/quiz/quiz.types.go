package quiz

import "familytime/backend/internal/engine"

type Phase string

const (
	PhaseIntro       Phase = "intro"
	PhaseQuestion    Phase = "question"
	PhaseReveal      Phase = "reveal"
	PhaseLeaderboard Phase = "leaderboard"
	PhaseRoundEnd    Phase = "round_end"
	PhaseFinished    Phase = "finished"
)

// Action types a client may send while a quiz is running.
const (
	ActionAnswer  = "quiz.answer"
	ActionNext    = "host.next"
	ActionEndGame = "host.end_game"
	// A one-player game may be paused by its player (the platform puts the
	// game in the background). Nobody else's clock is affected.
	ActionPause  = "host.pause"
	ActionResume = "host.resume"
	// ActionReveal shows a player the right answer. Only the server may raise
	// it, after the reveal has been paid for.
	ActionReveal = "quiz.reveal"
)

// AnswerResult is how a player's answer to the current question turned out.
type AnswerResult string

const (
	ResultCorrect AnswerResult = "correct"
	ResultWrong   AnswerResult = "wrong"
	ResultNone    AnswerResult = "none"
)

// Award keys. Stable: the client picks art and sound by them.
const (
	AwardSpeedDemon   = "speed_demon"
	AwardSharpshooter = "sharpshooter"
	AwardOnFire       = "on_fire"
)

// Scoring. A right answer is worth BasePoints however slow, plus up to
// SpeedPoints for how much of the clock was left — so being right always
// counts, and being right first counts for as much again. A run of right
// answers adds a streak bonus on top.
const (
	BasePoints      = 500
	SpeedPoints     = 500
	StreakStep      = 100
	StreakBonusCap  = 500
	OptionsPerQuery = 4
)

// Phase lengths when the game paces itself.
const (
	introMs       = 4000
	revealMs      = 6000
	leaderboardMs = 6000
	roundEndMs    = 9000

	// The quick tempo, for one phone with no big screen.
	quickIntroMs    = 2500
	quickRevealMs   = 2600
	quickRoundEndMs = 3500
)

const timerPhase = "phase"

// View is the quiz as one audience sees it. The right answer is only ever in
// here from the reveal onward — a client cannot receive it early.
type View struct {
	Game              string `json:"game"`
	Phase             Phase  `json:"phase"`
	Pacing            Pacing `json:"pacing"`
	Round             int    `json:"round"`
	TotalRounds       int    `json:"total_rounds"` // 0 when endless
	QuestionInRound   int    `json:"question_in_round"`
	QuestionsPerRound int    `json:"questions_per_round"`
	QuestionSeq       int    `json:"question_seq"`
	QuestionsAsked    int    `json:"questions_asked"`
	// What each point is scaled by under these settings, in percent.
	MultiplierPct  int            `json:"multiplier_pct"`
	Quick          bool           `json:"quick"`
	PhaseStartedAt engine.EpochMs `json:"phase_started_at"`
	// Absolute. 0 means the phase waits for the host.
	Deadline engine.EpochMs `json:"deadline"`
	// Paused freezes the clock at PausedLeftMs. Only a one-player game pauses.
	Paused       bool  `json:"paused"`
	PausedLeftMs int64 `json:"paused_left_ms"`
	// RevealCost is what showing the answer to this question costs, in gems.
	// Zero where reveals are not offered: any game with more than one player.
	RevealCost  int           `json:"reveal_cost"`
	Question    *QuestionView `json:"question"`
	AnsweredIDs []string      `json:"answered_ids"`
	ActiveCount int           `json:"active_count"`
	Reveal      *RevealView   `json:"reveal"`
	Board       []BoardRow    `json:"board"`
	You         *YouView      `json:"you"`
	Final       *FinalView    `json:"final"`
}

func (View) GameID() string { return GameID }

type QuestionView struct {
	Prompt        string   `json:"prompt"`
	Options       []string `json:"options"`
	Category      string   `json:"category"`
	CategoryLabel string   `json:"category_label"`
	CategoryEmoji string   `json:"category_emoji"`
	Difficulty    string   `json:"difficulty"`
}

type RevealView struct {
	AnswerIdx int          `json:"answer_idx"`
	Counts    []int        `json:"counts"`
	Fastest   *FastestView `json:"fastest"`
}

type FastestView struct {
	PlayerID string `json:"player_id"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar"`
	Ms       int    `json:"ms"`
}

type BoardRow struct {
	PlayerID   string       `json:"player_id"`
	Name       string       `json:"name"`
	Avatar     string       `json:"avatar"`
	Score      int          `json:"score"`
	RoundScore int          `json:"round_score"`
	Delta      int          `json:"delta"`
	Rank       int          `json:"rank"`
	PrevRank   int          `json:"prev_rank"`
	Streak     int          `json:"streak"`
	Result     AnswerResult `json:"result"`
	Connected  bool         `json:"connected"`
}

type YouView struct {
	Answered bool         `json:"answered"`
	Choice   *int         `json:"choice"`
	Result   AnswerResult `json:"result"`
	Points   int          `json:"points"`
	SpeedMs  int          `json:"speed_ms"`
	Score    int          `json:"score"`
	Rank     int          `json:"rank"`
	Streak   int          `json:"streak"`
	// HintIdx is the right answer, sent only to a player who has paid to see
	// it for the question in play.
	HintIdx *int `json:"hint_idx"`
	// Hinted is true when this question was answered with a reveal, which is
	// why it earned base points only.
	Hinted bool `json:"hinted"`
}

type FinalView struct {
	TotalQuestions int       `json:"total_questions"`
	RoundsPlayed   int       `json:"rounds_played"`
	Awards         []Award   `json:"awards"`
	Stats          []StatRow `json:"stats"`
}

type Award struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Blurb    string `json:"blurb"`
	PlayerID string `json:"player_id"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar"`
	Value    string `json:"value"`
}

type StatRow struct {
	PlayerID    string `json:"player_id"`
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
	Rank        int    `json:"rank"`
	Score       int    `json:"score"`
	Correct     int    `json:"correct"`
	Answered    int    `json:"answered"`
	AccuracyPct int    `json:"accuracy_pct"`
	AvgMs       int    `json:"avg_ms"`
	BestStreak  int    `json:"best_streak"`
}

// OptionsView is everything the setup screen offers.
type OptionsView struct {
	Game              string           `json:"game"`
	TotalQuestions    int              `json:"total_questions"`
	Categories        []CategoryOption `json:"categories"`
	Difficulties      []string         `json:"difficulties"`
	SecondsChoices    []int            `json:"seconds_choices"`
	Rounds            Range            `json:"rounds"`
	QuestionsPerRound Range            `json:"questions_per_round"`
	Pacings           []Pacing         `json:"pacings"`
}

func (OptionsView) GameID() string { return GameID }

type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type CategoryOption struct {
	Slug   string `json:"slug"`
	Label  string `json:"label"`
	Emoji  string `json:"emoji"`
	Blurb  string `json:"blurb"`
	Colour string `json:"colour"`
	Total  int    `json:"total"`
	Easy   int    `json:"easy"`
	Medium int    `json:"medium"`
	Hard   int    `json:"hard"`
}
