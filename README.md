# Family Time / backend

Go + Gin API with a websocket gateway. MongoDB for the question bank and the
record of finished games. Live rooms are held in memory.

## Quick start

```bash
cp .env.example .env
go run ./cmd/seed          # validate the question files and load them
go run ./cmd/seed -check   # validate only; writes nothing
go run ./cmd/server        # http://localhost:4100
```

## Layout

```
cmd/server            boot, cache warm-up, graceful shutdown
cmd/seed              question loader + the generated Quick Maths set
seeds/questions/      the bank, one file per category: difficulty|question|correct|wrong|wrong|wrong

internal/
  app/                Build() — middleware and feature registration, in order
  env/                parsed once, fails loudly

  engine/             rooms, the runtime, and the contract a game implements
  games/quiz/         the quiz: config, state machine, scoring, views

  features/<name>/    the unit of work is a feature, not a layer
    *.routes.go         Register(app) — owns its own route order
    *.controller.go     thin: read the request, call the service, respond
    *.service.go        business logic; returns result.Result[T], never sees the request
    *.repo.go           data access only
    *.schema.go         request validation
    *.types.go          stored types and wire types

  lib/                response, result, ids, db, logger, reqctx
  shared/             constants (error codes, severity), messages, middleware
```

## The rules that matter here

- **Services never return an error for a domain failure.** They return
  `result.Result[T]`. The controller hands a failure to `response.Bail`, so one
  middleware renders every error.
- **Services never see the HTTP request.** Request identity travels in the
  `context.Context`.
- **Never `c.JSON` in a handler.** Everything goes through `lib/response`.
- **Never an inline response string.** Copy resolves from `shared/messages`.
- **Every variant string is a named constant** — error codes, phases, message
  types, effect kinds.
- **The error envelope has three jobs, one field each:** `code` to branch on,
  `message` to display, `severity` to measure. `rejection_reason` is an
  operator diagnostic and not part of the contract.
- **A game owns no clocks, sockets or storage.** It returns `Effect`s and the
  room executes them; `now` is always passed in. That is what lets a whole game
  be replayed in a unit test.
- **The answer is withheld server-side** until the reveal. It is not in the
  payload a client receives while the question is open.

## Why starting a game is instant

The whole question bank is read into memory at boot and indexed by category and
difficulty (about 10 ms for 1,056 questions). Dealing a deck is slice work with
no I/O, the setup screen's category counts are precomputed, and finished games
are written behind — the podium shows first, the database catches up.

## API

| Method | Path | |
|---|---|---|
| GET | `/health`, `/health/ready` | probes; ready reports the database and the cache |
| GET | `/api/v1/games` | games that can be hosted |
| GET | `/api/v1/games/:gameId/options` | what the setup screen can offer |
| POST | `/api/v1/rooms` | open a room → `code`, `host_token` |
| GET | `/api/v1/rooms/:code` | public summary, for a joiner |
| POST | `/api/v1/rooms/:code/players` | take a seat → `player_token` |
| GET | `/api/v1/results?cursor=&limit=` | finished games, newest first, cursor-paged |
| GET | `/ws?code=&role=host\|player&token=` | the realtime gateway |

Over the socket the server sends the **whole state** on every change
(`server.state`), so a reconnect needs no replay. Clients send
`{ "type": ..., "payload": ... }`: `host.configure`, `host.start`, `host.next`,
`host.end_game`, `host.play_again`, `host.back_to_lobby`, `host.close_room`,
`quiz.answer`, `player.react`.

Testing and the full verification record: [`docs/qas/backend-qa-handoff.md`](docs/qas/backend-qa-handoff.md).

## The bulk puzzle set (207,684 questions)

`seeds/bulk/questions-bulk.jsonl` is a mongoimport-ready file of generated
puzzle questions for seven categories: Quick Maths, Number Puzzles, Time &
Money, Word Scramble, Missing Letters, Spelling Bee and ABC Order.

```bash
mongoimport --db familytime --collection questions \
  --file seeds/bulk/questions-bulk.jsonl --mode upsert
# then restart the server: the bank is cached at boot
```

- Nothing in it is typed-in trivia. Every answer is computed, or derived from
  the spelling of a word, by `seeds/bulk/generate.py` (fixed seed, so the
  output and the ids are identical on every run — re-importing updates, never
  duplicates).
- `seeds/bulk/verify.py` re-derives every answer from the question text by a
  separate route and fails if any differs, or if any wrong option is also right.
- Imported questions carry `source: "import"`. `go run ./cmd/seed` only ever
  replaces `source: "seed"` questions, so re-seeding leaves the import alone.
- A mixed game deals a category first, then a question, so these large
  categories do not crowd out the hand-written ones.

## Table-driven categories

`seeds/bulk/categories/<slug>.jsonl` — one mongoimport-ready file per category,
each derived from a single hand-checked reference table in `seeds/bulk/data/`:

| File | From |
|---|---|
| `countries`, `capitals`, `currencies`, `country-scramble` | `data/countries.txt` |
| `synonyms`, `antonyms` | `data/thesaurus.txt` |
| `nigerian-acronyms` | `data/nigerian-acronyms.txt` |
| `definitions` | `data/definitions.txt` |

```bash
for f in seeds/bulk/categories/*.jsonl; do
  mongoimport --db familytime --collection questions --file "$f" --mode upsert
done
```

To change a fact, edit the table and re-run its generator (`gen_countries.py`,
`gen_thesaurus.py`, `gen_acronyms.py`, `gen_definitions.py`), then
`verify_tables.py`, which re-reads the tables on its own and confirms that in
every question the marked answer is true and all three wrong answers are false.

Nigerian History and Nigerian Governance are hand-written, in
`seeds/questions/`, and load with `go run ./cmd/seed` like the original ten.

## Solo mode and Lagos Life

One phone, no big screen. Two more ways to play, both on the same engine:

| Method | Path | |
|---|---|---|
| POST | `/api/v1/solo` | standalone solo: `{ name, avatar?, config }` → a seat |
| GET | `/api/v1/platform/status` | is the server on real Lagos Life keys, or accepting dev sessions? |
| POST | `/api/v1/platform/solo` | Lagos Life solo: `{ session, config }` → a seat |
| POST | `/api/v1/platform/matches/:matchId/join` | Lagos Life duel: `{ session }` → this player's seat in the match's room |

- **A solo room** is private and owned by its one player, who may send the
  host messages (`host.start`, `host.play_again`). A run is always one quick
  round of ten questions; the player chooses only difficulty, topics and the clock.
- **A duel** is a Lagos Life match (2–4 players). The room is created when the
  first player arrives, the game starts by itself once everyone is seated (or
  after 20 seconds), and when it ends the server reports the winner to Lagos
  Life with a signed request — `internal/lib/vatar`. A tie for first is reported
  as a draw, which refunds every stake.
- **Scores scale with the settings**: Easy 80%, Mixed 100%, Medium 110%, Hard
  130%, multiplied by the clock (30s 90%, 20s 100%, 15s 110%, 10s 120%).
- **Identity is never taken from the request.** A platform player is whoever
  Lagos Life says the session token belongs to; who may sit in a duel is
  decided by Lagos Life's own record of the match.
- **Without `VATAR_SECRET`, outside production,** the server accepts stand-in
  `dev:<id>:<name>` sessions so the game can be built and tested off-platform.
  In production with no secret, the platform endpoints answer 503.

Not built: purchases. The game sells nothing.
