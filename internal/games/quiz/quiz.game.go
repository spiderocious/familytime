package quiz

import (
	"encoding/json"
	"math/rand"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

const (
	minPlayers = 1
	maxPlayers = 40
)

// Content is the part of the question bank the quiz needs.
type Content interface {
	Count(filter questions.Filter) int
	Total() int
	Stats() []questions.CategoryStats
	NewDeck(filter questions.Filter, seed int64) *questions.Deck
}

// Game is the static side of the quiz.
type Game struct {
	content Content
}

func NewGame(content Content) *Game { return &Game{content: content} }

func (g *Game) Manifest() engine.Manifest {
	return engine.Manifest{
		ID:         GameID,
		Title:      "Family Quiz",
		Tagline:    "Everyone answers at once. Right is good. Right and fast is better.",
		Emoji:      "🧠",
		MinPlayers: minPlayers,
		MaxPlayers: maxPlayers,
	}
}

func (g *Game) DefaultConfig() engine.Config { return defaultConfig() }

func (g *Game) Options() engine.Options {
	options := OptionsView{
		Game:              GameID,
		TotalQuestions:    g.content.Total(),
		Categories:        []CategoryOption{},
		Difficulties:      []string{string(questions.DifficultyEasy), string(questions.DifficultyMedium), string(questions.DifficultyHard), DifficultyMixed},
		SecondsChoices:    SecondsChoices,
		Rounds:            Range{Min: MinRounds, Max: MaxRounds},
		QuestionsPerRound: Range{Min: MinPerRound, Max: MaxPerRound},
		Pacings:           []Pacing{PacingAuto, PacingHost},
	}
	for _, stat := range g.content.Stats() {
		options.Categories = append(options.Categories, CategoryOption{
			Slug:   stat.Category.Slug,
			Label:  stat.Category.Label,
			Emoji:  stat.Category.Emoji,
			Blurb:  stat.Category.Blurb,
			Colour: stat.Category.Colour,
			Total:  stat.Total,
			Easy:   stat.Easy,
			Medium: stat.Medium,
			Hard:   stat.Hard,
		})
	}
	return options
}

func (g *Game) ParseConfig(raw json.RawMessage) (engine.Config, map[string][]string) {
	return parseConfig(raw)
}

func (g *Game) CanStart(config engine.Config) *result.Failure {
	cfg, ok := config.(Config)
	if !ok || g.content.Count(cfg.filter()) == 0 {
		return &result.Failure{
			Code:       constants.CodeNoQuestions,
			MessageKey: messages.PlayNoQuestions,
			Status:     constants.StatusConflict,
		}
	}
	return nil
}

func (g *Game) New(input engine.InitInput) (engine.Instance, []engine.Effect) {
	cfg, ok := input.Config.(Config)
	if !ok {
		cfg = defaultConfig()
	}

	inst := &instance{
		cfg:     cfg,
		deck:    g.content.NewDeck(cfg.filter(), input.Seed),
		rng:     rand.New(rand.NewSource(input.Seed)),
		players: map[string]*playerState{},
		answers: map[string]answer{},
		round:   1,
	}
	inst.syncRoster(input.Roster)

	return inst, inst.enter(PhaseIntro, input.Now, introMs)
}
