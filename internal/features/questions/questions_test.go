package questions

import (
	"fmt"
	"testing"
)

func fixtures() []Question {
	all := []Question{}
	add := func(category string, difficulty Difficulty, n int) {
		for i := 0; i < n; i++ {
			all = append(all, Question{
				ID:         fmt.Sprintf("%s-%s-%d", category, difficulty, i),
				Category:   category,
				Difficulty: difficulty,
				Prompt:     fmt.Sprintf("%s %s %d?", category, difficulty, i),
				Correct:    "yes",
				Wrong:      []string{"a", "b", "c"},
			})
		}
	}
	add(CategoryGeneral, DifficultyEasy, 4)
	add(CategoryGeneral, DifficultyHard, 2)
	add(CategoryBible, DifficultyEasy, 3)
	return all
}

func TestBank_CountsByFilter(t *testing.T) {
	bank := NewBank()
	bank.Replace(fixtures())

	cases := []struct {
		name   string
		filter Filter
		want   int
	}{
		{"everything", Filter{}, 9},
		{"one category", Filter{Categories: []string{CategoryGeneral}}, 6},
		{"one difficulty", Filter{Difficulty: DifficultyEasy}, 7},
		{"category and difficulty", Filter{Categories: []string{CategoryGeneral}, Difficulty: DifficultyHard}, 2},
		{"two categories", Filter{Categories: []string{CategoryGeneral, CategoryBible}}, 9},
		{"nothing matches", Filter{Categories: []string{CategorySports}}, 0},
		{"category with no hard questions", Filter{Categories: []string{CategoryBible}, Difficulty: DifficultyHard}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bank.Count(tc.filter); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBank_StatsCoverEveryCategoryInOrder(t *testing.T) {
	bank := NewBank()
	bank.Replace(fixtures())

	stats := bank.Stats()
	if len(stats) != len(Categories) {
		t.Fatalf("got %d categories, want %d", len(stats), len(Categories))
	}
	if stats[0].Category.Slug != CategoryGeneral || stats[0].Total != 6 || stats[0].Easy != 4 || stats[0].Hard != 2 {
		t.Fatalf("general = %+v", stats[0])
	}
}

func TestDeck_NoRepeatsUntilExhaustedThenKeepsDealing(t *testing.T) {
	bank := NewBank()
	bank.Replace(fixtures())
	deck := bank.NewDeck(Filter{Categories: []string{CategoryGeneral}}, 7)

	seen := map[string]bool{}
	for i := 0; i < deck.Size(); i++ {
		q, ok := deck.Draw()
		if !ok {
			t.Fatal("deck ran dry early")
		}
		if q.Category != CategoryGeneral {
			t.Fatalf("deck dealt a %s question through a general filter", q.Category)
		}
		if seen[q.ID] {
			t.Fatalf("question %s repeated before the deck was exhausted", q.ID)
		}
		seen[q.ID] = true
	}

	// Endless play: the deck reshuffles, and never deals the same question twice running.
	last := ""
	for i := 0; i < 50; i++ {
		q, ok := deck.Draw()
		if !ok {
			t.Fatal("a non-empty deck must deal forever")
		}
		if q.ID == last {
			t.Fatal("the same question was dealt twice in a row")
		}
		last = q.ID
	}
}

func TestDeck_SameSeedSameOrder(t *testing.T) {
	bank := NewBank()
	bank.Replace(fixtures())
	a, b := bank.NewDeck(Filter{}, 99), bank.NewDeck(Filter{}, 99)
	for i := 0; i < 9; i++ {
		qa, _ := a.Draw()
		qb, _ := b.Draw()
		if qa.ID != qb.ID {
			t.Fatal("two decks with the same seed dealt different orders")
		}
	}
}

func TestDeck_EmptyFilterDealsNothing(t *testing.T) {
	bank := NewBank()
	bank.Replace(fixtures())
	if _, ok := bank.NewDeck(Filter{Categories: []string{CategorySports}}, 1).Draw(); ok {
		t.Fatal("an empty deck dealt a question")
	}
}

func TestDeck_MixedGameStaysMixedWhenOneCategoryIsHuge(t *testing.T) {
	all := fixtures() // 6 general, 3 bible
	for i := 0; i < 5000; i++ {
		all = append(all, Question{
			ID: fmt.Sprintf("maths-%d", i), Category: CategoryMaths, Difficulty: DifficultyEasy,
			Prompt: fmt.Sprintf("sum %d?", i), Correct: "1", Wrong: []string{"2", "3", "4"},
		})
	}
	bank := NewBank()
	bank.Replace(all)
	deck := bank.NewDeck(Filter{}, 11)

	seen := map[string]int{}
	const draws = 600
	for i := 0; i < draws; i++ {
		q, _ := deck.Draw()
		seen[q.Category]++
	}
	// Maths is 99.8% of the bank. Dealt question-first it would take ~599 of
	// 600 draws; dealt category-first each of the three gets about a third.
	for _, category := range []string{CategoryGeneral, CategoryBible, CategoryMaths} {
		if seen[category] < draws/5 {
			t.Fatalf("%s got %d of %d draws; a mixed game should share them out: %v", category, seen[category], draws, seen)
		}
	}
}

func TestDeck_NoRepeatInsideACategoryUntilItIsExhausted(t *testing.T) {
	bank := NewBank()
	bank.Replace(fixtures())
	deck := bank.NewDeck(Filter{}, 5)

	// 6 general questions: the first six general draws must all differ, however
	// the bible draws are interleaved between them.
	seen := map[string]bool{}
	for len(seen) < 6 {
		q, _ := deck.Draw()
		if q.Category != CategoryGeneral {
			continue
		}
		if seen[q.ID] {
			t.Fatalf("general question %s repeated before the category was exhausted", q.ID)
		}
		seen[q.ID] = true
	}
}
