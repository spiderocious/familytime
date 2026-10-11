package questions

// Category slugs. Stable: they are stored on every question and sent by clients.
const (
	CategoryGeneral   = "general"
	CategoryScience   = "science"
	CategoryGeography = "geography"
	CategoryHistory   = "history"
	CategoryNigeria   = "nigeria"
	CategoryBible     = "bible"
	CategorySports    = "sports"
	CategoryMaths     = "maths"
	CategoryAnimals   = "animals"
	CategoryWords     = "words"

	// Puzzle categories. Their questions are generated and imported in bulk
	// (seeds/bulk), not written by hand: every answer is computed.
	CategoryNumbers  = "numbers"
	CategoryMeasures = "measures"
	CategoryScramble = "scramble"
	CategoryMissing  = "missing"
	CategorySpelling = "spelling"
	CategoryABC      = "abc"
)

// Where a question came from. The seed command owns "seed" questions and
// replaces them wholesale; it never touches imported ones.
const (
	SourceSeed   = "seed"
	SourceImport = "import"
)

// Categories is the ordered registry. The order here is the order hosts see.
var Categories = []Category{
	{Slug: CategoryGeneral, Label: "General Knowledge", Emoji: "🧠", Blurb: "A bit of everything", Colour: "#2D5BFF"},
	{Slug: CategoryScience, Label: "Science & Nature", Emoji: "🔬", Blurb: "Space, bodies and how things work", Colour: "#27B973"},
	{Slug: CategoryGeography, Label: "World Geography", Emoji: "🌍", Blurb: "Capitals, countries and landmarks", Colour: "#5BC0EB"},
	{Slug: CategoryHistory, Label: "History", Emoji: "🏛️", Blurb: "People and moments that shaped the world", Colour: "#B5651D"},
	{Slug: CategoryNigeria, Label: "Nigeria & Africa", Emoji: "🦅", Blurb: "Home turf", Colour: "#1F9A60"},
	{Slug: CategoryBible, Label: "Bible", Emoji: "📖", Blurb: "Stories and people of the Bible", Colour: "#7B4FBF"},
	{Slug: CategorySports, Label: "Sports", Emoji: "⚽", Blurb: "Football and far beyond", Colour: "#E85A4F"},
	{Slug: CategoryMaths, Label: "Quick Maths", Emoji: "➗", Blurb: "Sums against the clock", Colour: "#FF8A2A"},
	{Slug: CategoryAnimals, Label: "Animals", Emoji: "🦁", Blurb: "Creatures great and small", Colour: "#F7C948"},
	{Slug: CategoryWords, Label: "Words & English", Emoji: "🔤", Blurb: "Spelling, meanings and grammar", Colour: "#E0559C"},
	{Slug: CategoryNumbers, Label: "Number Puzzles", Emoji: "🔢", Blurb: "Patterns, Roman numerals and primes", Colour: "#7B4FBF"},
	{Slug: CategoryMeasures, Label: "Time & Money", Emoji: "⏰", Blurb: "Clocks, change, shapes and measures", Colour: "#27B973"},
	{Slug: CategoryScramble, Label: "Word Scramble", Emoji: "🔀", Blurb: "Unjumble the letters", Colour: "#FF8A2A"},
	{Slug: CategoryMissing, Label: "Missing Letters", Emoji: "🧩", Blurb: "Find the letter that fits", Colour: "#5BC0EB"},
	{Slug: CategorySpelling, Label: "Spelling Bee", Emoji: "🐝", Blurb: "Spot the right spelling", Colour: "#F7C948"},
	{Slug: CategoryABC, Label: "ABC Order", Emoji: "🔡", Blurb: "Which word comes first?", Colour: "#E85A4F"},
}

func FindCategory(slug string) (Category, bool) {
	for _, c := range Categories {
		if c.Slug == slug {
			return c, true
		}
	}
	return Category{}, false
}

const collectionName = "questions"
