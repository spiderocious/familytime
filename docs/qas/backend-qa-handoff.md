# Backend QA Handoff — Family Quiz

**Date:** 2026-10-11
**Build:** `go vet` ✅ · `go test -race ./...` ✅
**Base URL:** `http://localhost:4100/api/v1`
**Auth:** none. A room is controlled by its `host_token`; a seat by its `player_token`.

## How this repo tests

| Tier | Where | What it covers |
|---|---|---|
| Unit | `internal/games/quiz/quiz_test.go` | scoring, streaks, round flow, endless mode, host pacing, late/duplicate/malformed answers, late joiners, drop-outs, config validation |
| Unit | `internal/features/questions/questions_test.go` | bank filters, deck never repeats before exhaustion, deterministic by seed |
| Unit | `internal/shared/middleware/origin_test.go` | which browser origins are accepted, production vs development |
| Integration | `internal/app/app_test.go` | the real HTTP app + real websockets in-process: envelope and error cases, a full game, reconnect keeps seat and score. Storage is faked; no MongoDB needed |
| Live | `docs/qas/scripts/ws-smoke.mjs` | a running server + real MongoDB: three bots play a whole game and every revealed answer is checked against the database |

There is no Testcontainers tier. The only database behaviour is "load all
questions" and "insert a result", both exercised by the live script.

## Live script

```bash
node docs/qas/scripts/ws-smoke.mjs                      # everything, mixed
node docs/qas/scripts/ws-smoke.mjs bible,nigeria hard   # filtered
```

Ada answers right instantly, Cy answers right after 1.5 s, Bem answers wrong.
It asserts: the revealed answer equals the stored correct answer; no question
escapes the category or difficulty filter; Ada outscores Cy on speed alone;
final ranks, accuracy, average speed, streaks and all three awards; the game is
recorded and retrievable from `/results`.

## Verified on 2026-10-11

- Seed: 1,056 questions valid and loaded; cache warm in ~10 ms.
- REST: every endpoint, plus 404 / 400 / 409 / 422 cases, by curl.
- Live games: everything-mixed, `bible,nigeria hard`, `maths easy`, `animals,words,science medium`, `geography` — all pass.
- Concurrency: 25 games at once (75 players) — 25/25 pass, zero server errors logged.

## Edge cases to verify

| Scenario | Status | Identity |
|---|---|---|
| Unknown path | 404 | `not_found` |
| Unknown room | 404 | `room_not_found` |
| Unknown game | 404 | `game_not_found` |
| Malformed JSON body | 400 | `malformed_json` |
| Invalid config (every bad field reported) | 422 | `validation_error` + `field_errors` |
| Name already taken, any case | 409 | `name_taken` + `field_errors.name` |
| Bad name or avatar | 422 | `validation_error` + `field_errors` |
| Websocket with a wrong token | 401 before upgrade | `invalid_token` |
| Player sends a host message | socket error | `not_host` |
| Start with no players | socket error | `not_enough_players` |
| Start with no matching questions | socket error | `no_questions` |
| Malformed answer | socket error | `invalid_action` |
| Late or duplicate answer | silently ignored | — |

## State machine

| Phase | Moves to | On |
|---|---|---|
| `intro` | `question` | timer (4 s) or host Next |
| `question` | `reveal` | timer, everyone connected has answered, or host Next |
| `reveal` | `leaderboard`, or `round_end` after the round's last question | timer (6 s) or host Next |
| `leaderboard` | `question` | timer (6 s) or host Next |
| `round_end` | `intro` of the next round, or `finished` | timer (9 s) or host Next |
| any | `finished` | host End game |

With host pacing, `reveal`, `leaderboard` and `round_end` have no timer
(`deadline` is 0) and wait for Next.

## Pagination

`GET /results` is cursor-based. Meta is `{ "next_cursor": string | null, "has_more": boolean }`;
pass `?cursor=<next_cursor>` for the next page.

## Known limits and out of scope

- Rooms live in memory: a server restart ends every game in progress. Clients
  detect this and say the room is gone.
- One server process only. Running several would need shared room state.
- No accounts, by design.
- No rate limiting on the REST endpoints (the websocket is limited per connection).
- No admin screen for editing questions; the bank is the files in `seeds/questions/`.
