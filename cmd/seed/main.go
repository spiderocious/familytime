// Command seed loads the question bank into MongoDB.
//
//	go run ./cmd/seed           validate, then make the collection match the files
//	go run ./cmd/seed -check    validate only; touches nothing
//
// It is idempotent: ids are derived from the question itself, so re-running
// after an edit updates in place and removes anything no longer in the files.
package main

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"familytime/backend/internal/env"
	"familytime/backend/internal/features/questions"
	"familytime/backend/internal/lib/db"
)

const (
	fieldCount      = 6
	maxPromptLength = 150
	maxOptionLength = 60
)

var difficultyByCode = map[string]questions.Difficulty{
	"E": questions.DifficultyEasy,
	"M": questions.DifficultyMedium,
	"H": questions.DifficultyHard,
}

func main() {
	checkOnly := flag.Bool("check", false, "validate the question files without writing to the database")
	dir := flag.String("dir", "seeds/questions", "directory holding the question files")
	flag.Parse()

	all, problems := load(*dir)
	all = append(all, mathsQuestions()...)
	problems = append(problems, validate(all)...)

	report(all)
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d problem(s):\n", len(problems))
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  - "+p)
		}
		os.Exit(1)
	}
	if *checkOnly {
		fmt.Println("\nAll questions are valid. Nothing was written (-check).")
		return
	}

	cfg, err := env.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid environment configuration:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	database, err := db.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not connect to MongoDB:", err)
		os.Exit(1)
	}
	defer func() { _ = database.Close(ctx) }()

	repo := questions.NewRepo(database.DB)
	if err := repo.EnsureIndexes(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Could not create indexes:", err)
		os.Exit(1)
	}
	written, removed, err := repo.ReplaceAll(ctx, all)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not write questions:", err)
		os.Exit(1)
	}
	fmt.Printf("\nSeeded database %q: %d questions written, %d stale removed.\n", cfg.MongoDB, written, removed)
}

// questionID is derived from the content, so the same question always gets the
// same id and a re-seed updates rather than duplicates.
func questionID(category, prompt, correct string) string {
	sum := sha1.Sum([]byte(category + "|" + prompt + "|" + correct))
	return "q_" + hex.EncodeToString(sum[:])[:20]
}

func load(dir string) ([]questions.Question, []string) {
	all := []questions.Question{}
	problems := []string{}
	now := time.Now().UTC()

	for _, category := range questions.Categories {
		if category.Slug == questions.CategoryMaths {
			continue // generated, see maths.go
		}
		path := filepath.Join(dir, category.Slug+".txt")
		file, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue // a puzzle category: imported in bulk, no hand-written file
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", path, err))
			continue
		}

		scanner := bufio.NewScanner(file)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Split(line, "|")
			if len(parts) != fieldCount {
				problems = append(problems, fmt.Sprintf("%s:%d: expected %d fields, found %d", path, lineNo, fieldCount, len(parts)))
				continue
			}
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			difficulty, ok := difficultyByCode[parts[0]]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s:%d: unknown difficulty %q", path, lineNo, parts[0]))
				continue
			}
			all = append(all, questions.Question{
				ID:         questionID(category.Slug, parts[1], parts[2]),
				Category:   category.Slug,
				Difficulty: difficulty,
				Prompt:     parts[1],
				Correct:    parts[2],
				Wrong:      parts[3:],
				Source:     questions.SourceSeed,
				CreatedAt:  now,
			})
		}
		_ = file.Close()
	}
	return all, problems
}

func validate(all []questions.Question) []string {
	problems := []string{}
	seen := map[string]bool{}

	for _, q := range all {
		where := fmt.Sprintf("[%s] %q", q.Category, q.Prompt)

		if q.Prompt == "" || utf8.RuneCountInString(q.Prompt) > maxPromptLength {
			problems = append(problems, where+": prompt is empty or too long")
		}
		if len(q.Wrong) != 3 {
			problems = append(problems, where+": needs exactly three wrong answers")
		}

		options := map[string]bool{}
		for _, option := range append([]string{q.Correct}, q.Wrong...) {
			if option == "" || utf8.RuneCountInString(option) > maxOptionLength {
				problems = append(problems, where+": an answer is empty or too long")
			}
			key := strings.ToLower(option)
			if options[key] {
				problems = append(problems, where+": answer "+option+" appears twice")
			}
			options[key] = true
		}

		if seen[q.ID] {
			problems = append(problems, where+": duplicate question")
		}
		seen[q.ID] = true
	}
	return problems
}

func report(all []questions.Question) {
	type tally struct{ easy, medium, hard int }
	byCategory := map[string]*tally{}
	for _, q := range all {
		t, ok := byCategory[q.Category]
		if !ok {
			t = &tally{}
			byCategory[q.Category] = t
		}
		switch q.Difficulty {
		case questions.DifficultyEasy:
			t.easy++
		case questions.DifficultyMedium:
			t.medium++
		case questions.DifficultyHard:
			t.hard++
		}
	}

	slugs := []string{}
	for slug := range byCategory {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	fmt.Printf("%-12s %6s %6s %6s %6s\n", "category", "easy", "medium", "hard", "total")
	for _, slug := range slugs {
		t := byCategory[slug]
		fmt.Printf("%-12s %6d %6d %6d %6d\n", slug, t.easy, t.medium, t.hard, t.easy+t.medium+t.hard)
	}
	fmt.Printf("%-12s %27d\n", "all", len(all))
}
