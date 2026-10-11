// Package questions owns the question bank: its storage, and the in-memory
// cache every game draws from.
package questions

import "time"

type Difficulty string

const (
	DifficultyEasy   Difficulty = "easy"
	DifficultyMedium Difficulty = "medium"
	DifficultyHard   Difficulty = "hard"
)

var Difficulties = []Difficulty{DifficultyEasy, DifficultyMedium, DifficultyHard}

func IsDifficulty(value string) bool {
	for _, d := range Difficulties {
		if string(d) == value {
			return true
		}
	}
	return false
}

// Question is one multiple-choice question as stored. The correct answer is
// kept apart from the wrong ones; the order shown to players is decided when
// the question is dealt, so the right answer has no fixed position.
type Question struct {
	ID         string     `bson:"_id"`
	Category   string     `bson:"category"`
	Difficulty Difficulty `bson:"difficulty"`
	Prompt     string     `bson:"prompt"`
	Correct    string     `bson:"correct"`
	Wrong      []string   `bson:"wrong"`
	Source     string     `bson:"source"`
	CreatedAt  time.Time  `bson:"created_at"`
}

// Category is static metadata for a question category.
type Category struct {
	Slug   string
	Label  string
	Emoji  string
	Blurb  string
	Colour string
}

// Filter narrows the bank. Empty Categories means every category; an empty
// Difficulty means every difficulty.
type Filter struct {
	Categories []string
	Difficulty Difficulty
}

// CategoryStats is how many questions a category holds, by difficulty.
type CategoryStats struct {
	Category Category
	Total    int
	Easy     int
	Medium   int
	Hard     int
}
