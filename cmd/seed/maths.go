package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"familytime/backend/internal/features/questions"
)

// Quick Maths is generated rather than typed: the answer is computed, so it
// cannot be wrong, and the wrong answers are the mistakes people actually make
// (off by one, off by ten, forgetting the order of operations).
//
// The seed is fixed, so the same questions come out on every run and the
// content-derived ids stay stable.
const mathsSeed = 20261011

type mathsBuilder struct {
	rng  *rand.Rand
	seen map[string]bool
	out  []questions.Question
	now  time.Time
}

func mathsQuestions() []questions.Question {
	b := &mathsBuilder{rng: rand.New(rand.NewSource(mathsSeed)), seen: map[string]bool{}, now: time.Now().UTC()}

	b.repeat(14, func() bool {
		x, y := b.between(12, 60), b.between(3, 30)
		return b.add(questions.DifficultyEasy, fmt.Sprintf("What is %d + %d?", x, y), x+y, x+y+10, x+y-10, x+y+1, x+y-1, x+y+2)
	})
	b.repeat(13, func() bool {
		x := b.between(20, 90)
		y := b.between(3, x-5)
		return b.add(questions.DifficultyEasy, fmt.Sprintf("What is %d − %d?", x, y), x-y, x-y+10, x-y-10, x-y+1, x-y-1, x+y)
	})
	b.repeat(13, func() bool {
		x, y := b.between(2, 10), b.between(2, 10)
		return b.add(questions.DifficultyEasy, fmt.Sprintf("What is %d × %d?", x, y), x*y, x*y+x, x*y-x, x*y+y, x*y-y, x+y)
	})

	b.repeat(10, func() bool {
		x, y := b.between(12, 25), b.between(3, 9)
		return b.add(questions.DifficultyMedium, fmt.Sprintf("What is %d × %d?", x, y), x*y, x*y+y, x*y-y, x*y+10, x*y-10, x*y+x)
	})
	b.repeat(9, func() bool {
		divisor, quotient := b.between(3, 12), b.between(4, 12)
		return b.add(questions.DifficultyMedium, fmt.Sprintf("What is %d ÷ %d?", divisor*quotient, divisor), quotient, quotient+1, quotient-1, quotient+2, quotient-2, divisor)
	})
	b.repeat(9, func() bool {
		percents := []int{10, 20, 25, 50, 75}
		percent := percents[b.rng.Intn(len(percents))]
		whole := b.between(2, 20) * 20
		answer := whole * percent / 100
		return b.add(questions.DifficultyMedium, fmt.Sprintf("What is %d%% of %d?", percent, whole), answer, answer+5, answer-5, answer+10, answer*2, whole-answer)
	})
	b.repeat(8, func() bool {
		x := b.between(4, 15)
		return b.add(questions.DifficultyMedium, fmt.Sprintf("What is %d squared?", x), x*x, x*2, x*x+x, x*x-x, (x+1)*(x+1), x*x+1)
	})

	b.repeat(8, func() bool {
		x, y := b.between(11, 19), b.between(11, 19)
		return b.add(questions.DifficultyHard, fmt.Sprintf("What is %d × %d?", x, y), x*y, x*y+10, x*y-10, x*y+x, x*y-y, x*y+20)
	})
	b.repeat(6, func() bool {
		root := b.between(11, 20)
		return b.add(questions.DifficultyHard, fmt.Sprintf("What is the square root of %d?", root*root), root, root+1, root-1, root+2, root-2, root+4)
	})
	b.repeat(6, func() bool {
		x, y, z := b.between(3, 12), b.between(2, 9), b.between(2, 9)
		// (x + y) × z is the classic mistake: adding before multiplying.
		return b.add(questions.DifficultyHard, fmt.Sprintf("What is %d + %d × %d?", x, y, z), x+y*z, (x+y)*z, x+y+z, x*y+z, x+y*z+1, x+y*z-1)
	})
	b.repeat(4, func() bool {
		whole := b.between(3, 25) * 4
		answer := whole * 3 / 4
		return b.add(questions.DifficultyHard, fmt.Sprintf("What is three-quarters of %d?", whole), answer, whole/4, whole/2, answer+4, answer-4, answer+2)
	})

	return b.out
}

func (b *mathsBuilder) between(low, high int) int {
	if high <= low {
		return low
	}
	return low + b.rng.Intn(high-low+1)
}

// repeat calls build until it has produced n questions.
func (b *mathsBuilder) repeat(n int, build func() bool) {
	made := 0
	for attempts := 0; made < n && attempts < n*50; attempts++ {
		if build() {
			made++
		}
	}
}

// add records a question, choosing three distinct wrong answers from the
// candidates. It reports false when the prompt is a repeat or there are not
// enough usable wrong answers, so the caller tries another.
func (b *mathsBuilder) add(difficulty questions.Difficulty, prompt string, answer int, candidates ...int) bool {
	if b.seen[prompt] {
		return false
	}

	wrong := []string{}
	used := map[int]bool{answer: true}
	for _, candidate := range candidates {
		if candidate <= 0 || used[candidate] {
			continue
		}
		used[candidate] = true
		wrong = append(wrong, strconv.Itoa(candidate))
		if len(wrong) == 3 {
			break
		}
	}
	if len(wrong) < 3 {
		return false
	}

	b.seen[prompt] = true
	correct := strconv.Itoa(answer)
	b.out = append(b.out, questions.Question{
		ID:         questionID(questions.CategoryMaths, prompt, correct),
		Category:   questions.CategoryMaths,
		Difficulty: difficulty,
		Prompt:     prompt,
		Correct:    correct,
		Wrong:      wrong,
		Source:     questions.SourceSeed,
		CreatedAt:  b.now,
	})
	return true
}
