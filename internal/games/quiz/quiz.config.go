// Package quiz is the family quiz: everyone answers the same multiple-choice
// question at once, and a right answer scores more the faster it lands.
package quiz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"familytime/backend/internal/engine"
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/shared/messages"
)

const GameID = "quiz"

type Pacing string

const (
	// The game moves itself along on timers.
	PacingAuto Pacing = "auto"
	// The reveal and scoreboards wait for the host to press Next.
	PacingHost Pacing = "host"
)

// DifficultyMixed draws from every difficulty.
const DifficultyMixed = "mixed"

const (
	MinRounds          = 1
	MaxRounds          = 20
	MinPerRound        = 3
	MaxPerRound        = 30
	MinSeconds         = 5
	MaxSeconds         = 60
	DefaultRounds      = 3
	DefaultPerRound    = 10
	DefaultSeconds     = 20
	maxCategoriesShown = 2
)

// SecondsChoices are the timer lengths the setup screen offers.
var SecondsChoices = []int{10, 15, 20, 30}

type Config struct {
	Difficulty string `json:"difficulty"`
	// Empty means every category, mixed together.
	Categories        []string `json:"categories"`
	Rounds            int      `json:"rounds"`
	QuestionsPerRound int      `json:"questions_per_round"`
	// Endless ignores Rounds: the game runs until the host ends it.
	Endless            bool   `json:"endless"`
	SecondsPerQuestion int    `json:"seconds_per_question"`
	Pacing             Pacing `json:"pacing"`
	// Quick is the tempo for one phone with no big screen: shorter pauses and
	// no separate scoreboard between questions. Solo games and duels use it.
	Quick bool `json:"quick"`
}

// Score multipliers, in percent. Harder settings pay more, easier ones less,
// so a score earned on Easy with a long clock is worth less than the same
// answers on Hard against a short one. The defaults (Mixed, 20s) are 100%.
var difficultyPct = map[string]int{"easy": 80, DifficultyMixed: 100, "medium": 110, "hard": 130}

func timerPct(seconds int) int {
	switch {
	case seconds <= 10:
		return 120
	case seconds <= 15:
		return 110
	case seconds <= 20:
		return 100
	default:
		return 90
	}
}

// MultiplierPct is what every point this configuration earns is scaled by.
func (c Config) MultiplierPct() int {
	pct, ok := difficultyPct[c.Difficulty]
	if !ok {
		pct = 100
	}
	return pct * timerPct(c.SecondsPerQuestion) / 100
}

// What one answer reveal costs, in gems. Harder settings cost more, and so
// does the back half of a run, where one wrong answer undoes the most.
var revealBase = map[string]int{"easy": 1, DifficultyMixed: 2, "medium": 2, "hard": 3}

const (
	revealLateStep    = 1
	revealMidPercent  = 40
	revealLatePercent = 70
)

// RevealCost prices a reveal for the question at this position in its round.
func (c Config) RevealCost(questionInRound int) int {
	cost, ok := revealBase[c.Difficulty]
	if !ok {
		cost = revealBase[DifficultyMixed]
	}
	if c.QuestionsPerRound <= 0 {
		return cost
	}
	// Position of the question just before this one: the first is 0%.
	progress := (questionInRound - 1) * 100 / c.QuestionsPerRound
	if progress >= revealMidPercent {
		cost += revealLateStep
	}
	if progress >= revealLatePercent {
		cost += revealLateStep
	}
	return cost
}

func defaultConfig() Config {
	return Config{
		Difficulty:         DifficultyMixed,
		Categories:         []string{},
		Rounds:             DefaultRounds,
		QuestionsPerRound:  DefaultPerRound,
		Endless:            false,
		SecondsPerQuestion: DefaultSeconds,
		Pacing:             PacingAuto,
	}
}

// parseConfig applies defaults for anything missing, then validates. Every
// invalid field is reported, not just the first.
func parseConfig(raw json.RawMessage) (Config, map[string][]string) {
	config := defaultConfig()
	fieldErrors := map[string][]string{}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &config); err != nil {
			fieldErrors["config"] = []string{messages.Resolve(messages.FieldConfigMalformed)}
			return defaultConfig(), fieldErrors
		}
	}

	if config.Difficulty != DifficultyMixed && !questions.IsDifficulty(config.Difficulty) {
		fieldErrors["difficulty"] = []string{messages.Resolve(messages.FieldDifficultyUnknown)}
	}

	seen := map[string]bool{}
	categories := []string{}
	for _, slug := range config.Categories {
		if _, ok := questions.FindCategory(slug); !ok {
			fieldErrors["categories"] = []string{messages.Resolve(messages.FieldCategoryUnknown)}
			break
		}
		if !seen[slug] {
			seen[slug] = true
			categories = append(categories, slug)
		}
	}
	// Picking every category is the same request as picking none.
	if len(categories) == len(questions.Categories) {
		categories = []string{}
	}
	config.Categories = categories

	if config.Rounds < MinRounds || config.Rounds > MaxRounds {
		fieldErrors["rounds"] = []string{messages.Resolve(messages.FieldRoundsRange)}
	}
	if config.QuestionsPerRound < MinPerRound || config.QuestionsPerRound > MaxPerRound {
		fieldErrors["questions_per_round"] = []string{messages.Resolve(messages.FieldPerRoundRange)}
	}
	if config.SecondsPerQuestion < MinSeconds || config.SecondsPerQuestion > MaxSeconds {
		fieldErrors["seconds_per_question"] = []string{messages.Resolve(messages.FieldSecondsRange)}
	}
	if config.Pacing != PacingAuto && config.Pacing != PacingHost {
		fieldErrors["pacing"] = []string{messages.Resolve(messages.FieldPacingUnknown)}
	}

	return config, fieldErrors
}

func (c Config) filter() questions.Filter {
	filter := questions.Filter{Categories: c.Categories}
	if c.Difficulty != DifficultyMixed {
		filter.Difficulty = questions.Difficulty(c.Difficulty)
	}
	return filter
}

// Facts is the short, public description of the settings shown in the lobby.
func (c Config) Facts() []engine.Fact {
	length := fmt.Sprintf("%d rounds × %d questions", c.Rounds, c.QuestionsPerRound)
	if c.Rounds == 1 {
		length = fmt.Sprintf("%d questions", c.QuestionsPerRound)
	}
	if c.Endless {
		length = fmt.Sprintf("Endless · rounds of %d", c.QuestionsPerRound)
	}

	categories := "Everything"
	if len(c.Categories) > 0 {
		labels := []string{}
		for _, slug := range c.Categories {
			if category, ok := questions.FindCategory(slug); ok {
				labels = append(labels, category.Label)
			}
		}
		if len(labels) > maxCategoriesShown {
			categories = fmt.Sprintf("%s + %d more", labels[0], len(labels)-1)
		} else {
			categories = strings.Join(labels, " & ")
		}
	}

	pace := "Moves on its own"
	if c.Pacing == PacingHost {
		pace = "Host presses Next"
	}

	return []engine.Fact{
		{Label: "Length", Value: length},
		{Label: "Topics", Value: categories},
		{Label: "Difficulty", Value: strings.ToUpper(c.Difficulty[:1]) + c.Difficulty[1:]},
		{Label: "Timer", Value: fmt.Sprintf("%ds per question", c.SecondsPerQuestion)},
		{Label: "Pace", Value: pace},
	}
}
