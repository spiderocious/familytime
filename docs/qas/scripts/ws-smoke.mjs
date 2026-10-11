// Live smoke test: plays a whole quiz against a RUNNING server with the real
// question bank, and checks every revealed answer against MongoDB.
//
//   node docs/qas/scripts/ws-smoke.mjs                       # everything, mixed
//   node docs/qas/scripts/ws-smoke.mjs bible,nigeria hard    # filtered
//
// Needs Node 22+ (global WebSocket and fetch) and mongosh on PATH.
import { execFileSync } from 'node:child_process';

const BASE = process.env.API_URL ?? 'http://localhost:4100';
const DB = process.env.MONGODB_DB ?? 'familytime';
const categories = (process.argv[2] ?? '').split(',').filter(Boolean);
const difficulty = process.argv[3] ?? 'mixed';
const ROUNDS = 2;
const PER_ROUND = 4;

const fail = (message) => {
  console.error(`FAIL: ${message}`);
  process.exit(1);
};
const check = (condition, message) => {
  if (!condition) fail(message);
};

// The truth, read straight from the database rather than from the server.
//
// Keyed by the question AND its four options. The prompt alone is not enough:
// thousands of puzzles share one ("Which of these is spelled correctly?"), and
// only the full set of options identifies which question was asked.
const optionKey = (category, prompt, options) => `${category}|${prompt}|${[...options].sort().join('|')}`;
const truth = new Map();
const rows = execFileSync('mongosh', ['--quiet', DB, '--eval',
  'db.questions.find({}, {_id:0, prompt:1, correct:1, wrong:1, category:1, difficulty:1}).forEach(q => print(JSON.stringify(q)))',
], { maxBuffer: 512 * 1024 * 1024 }).toString().split('\n');
for (const row of rows) {
  if (row.trim() === '') continue;
  const q = JSON.parse(row);
  truth.set(optionKey(q.category, q.prompt, [q.correct, ...q.wrong]), q);
}

const api = async (method, path, body) => {
  const res = await fetch(`${BASE}/api/v1${path}`, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const json = await res.json();
  if (!res.ok) fail(`${method} ${path} -> ${res.status} ${JSON.stringify(json.error)}`);
  return json.data;
};

const room = await api('POST', '/rooms', {
  game_id: 'quiz',
  config: { categories, difficulty, rounds: ROUNDS, questions_per_round: PER_ROUND, seconds_per_question: 10, pacing: 'host' },
});
console.log(`room ${room.code} · categories=${categories.join('+') || 'all'} · difficulty=${difficulty}`);

// Ada is always right and instant. Cy is always right but slow. Bem is always wrong.
const bots = [
  { name: 'Ada', right: true, delayMs: 0 },
  { name: 'Bem', right: false, delayMs: 200 },
  { name: 'Cy', right: true, delayMs: 1500 },
];
for (const bot of bots) {
  Object.assign(bot, await api('POST', `/rooms/${room.code}/players`, { name: bot.name }));
}

const wsURL = (role, token) => `${BASE.replace(/^http/, 'ws')}/ws?code=${room.code}&role=${role}&token=${token}`;
const send = (ws, type, payload) => ws.send(JSON.stringify({ type, payload }));

const correctOptionIndex = (question) => {
  const stored = truth.get(optionKey(question.category, question.prompt, question.options));
  check(stored !== undefined, `server asked a question that is not in the database: ${question.prompt} [${question.options.join(', ')}]`);
  return { index: question.options.indexOf(stored.correct), stored };
};

for (const bot of bots) {
  const ws = new WebSocket(wsURL('player', bot.player_token));
  let answeredSeq = 0;
  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    const game = msg.state?.game;
    if (!game || game.phase !== 'question' || game.question_seq === answeredSeq) return;
    answeredSeq = game.question_seq;
    check(game.reveal === null, 'a player received the reveal while the question was open');
    const { index } = correctOptionIndex(game.question);
    const choice = bot.right ? index : (index + 1) % 4;
    setTimeout(() => send(ws, 'quiz.answer', { question_seq: game.question_seq, choice }), bot.delayMs);
  };
  bot.ws = ws;
  await new Promise((resolve) => (ws.onopen = resolve));
}

const host = new WebSocket(wsURL('host', room.host_token));
let started = false;
let lastStep = '';
let revealed = 0;

const finished = new Promise((resolve) => {
  host.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === 'server.error') fail(`host got ${msg.error.code}: ${msg.error.message}`);
    const state = msg.state;
    if (!state) return;

    if (state.room.phase === 'lobby') {
      const ready = state.room.players.length === bots.length && state.room.players.every((p) => p.connected);
      if (ready && !started) {
        started = true;
        console.log('lobby facts:', state.room.facts.map((f) => `${f.label}=${f.value}`).join(' · '));
        send(host, 'host.start');
      }
      return;
    }

    const game = state.game;
    const step = `${game.phase}:${game.round}:${game.question_seq}`;
    if (step === lastStep) return;

    if (game.phase === 'question') {
      lastStep = step;
      return; // the bots answer; the reveal follows on its own once all three have
    }
    lastStep = step;

    if (game.phase === 'reveal') {
      revealed += 1;
      const q = game.question;
      const { stored } = correctOptionIndex(q);
      check(q.options[game.reveal.answer_idx] === stored.correct, `WRONG ANSWER REVEALED for "${q.prompt}": server said "${q.options[game.reveal.answer_idx]}", database says "${stored.correct}"`);
      if (categories.length > 0) check(categories.includes(q.category), `question from ${q.category} leaked through the category filter`);
      if (difficulty !== 'mixed') check(q.difficulty === difficulty, `a ${q.difficulty} question leaked through the ${difficulty} filter`);
      check(game.reveal.counts.reduce((a, b) => a + b, 0) === 3, 'reveal counts do not add up to three answers');
      check(game.reveal.fastest?.name === 'Ada', `fastest should be Ada, got ${game.reveal.fastest?.name}`);
      const row = Object.fromEntries(game.board.map((r) => [r.name, r]));
      check(row.Ada.result === 'correct' && row.Cy.result === 'correct' && row.Bem.result === 'wrong', 'per-player results are wrong');
      check(row.Ada.delta > row.Cy.delta && row.Cy.delta >= 500 && row.Bem.delta === 0, `speed scoring is off: Ada +${row.Ada.delta}, Cy +${row.Cy.delta}, Bem +${row.Bem.delta}`);
      console.log(`  R${game.round} Q${game.question_in_round} [${q.category}/${q.difficulty}] ${q.prompt} -> ${stored.correct}   (Ada +${row.Ada.delta}, Cy +${row.Cy.delta})`);
    }

    if (game.phase === 'finished') {
      resolve(game);
      return;
    }
    setTimeout(() => send(host, 'host.next'), 60);
  };
});

const game = await finished;
const total = ROUNDS * PER_ROUND;
check(revealed === total, `expected ${total} reveals, saw ${revealed}`);
check(game.final.total_questions === total && game.final.rounds_played === ROUNDS, 'final totals are wrong');

const stats = Object.fromEntries(game.final.stats.map((s) => [s.name, s]));
check(stats.Ada.rank === 1 && stats.Cy.rank === 2 && stats.Bem.rank === 3, 'final ranking should be Ada, Cy, Bem');
check(stats.Ada.correct === total && stats.Ada.accuracy_pct === 100 && stats.Ada.best_streak === total, 'Ada stats are wrong');
check(stats.Cy.correct === total && stats.Cy.avg_ms >= 1400 && stats.Cy.avg_ms > stats.Ada.avg_ms, `Cy should average ~1.5s, got ${stats.Cy.avg_ms}ms`);
check(stats.Bem.score === 0 && stats.Bem.correct === 0 && stats.Bem.answered === total && stats.Bem.accuracy_pct === 0, 'Bem stats are wrong');
check(game.board.reduce((sum, r) => sum + r.score, 0) === stats.Ada.score + stats.Cy.score, 'board total does not equal the sum of the scorers');

const awards = Object.fromEntries(game.final.awards.map((a) => [a.key, a]));
check(awards.speed_demon?.name === 'Ada' && awards.sharpshooter?.name === 'Ada' && awards.on_fire?.name === 'Ada', 'awards went to the wrong player');

console.log('\nfinal standings');
for (const s of game.final.stats) {
  console.log(`  ${s.rank}. ${s.name.padEnd(4)} ${String(s.score).padStart(6)} pts · ${s.correct}/${game.final.total_questions} right · ${s.accuracy_pct}% · avg ${(s.avg_ms / 1000).toFixed(2)}s · best streak ${s.best_streak}`);
}
console.log('awards:', game.final.awards.map((a) => `${a.title} → ${a.name} (${a.value})`).join(' · '));

// The finished game must be recorded.
await new Promise((resolve) => setTimeout(resolve, 400));
// Other games may be finishing at the same moment, so find this room's row
// rather than assuming it is the newest.
const res = await fetch(`${BASE}/api/v1/results?limit=50`);
const page = await res.json();
const recorded = page.data.find((r) => r.room_code === room.code);
check(recorded !== undefined && recorded.players[0].name === 'Ada' && recorded.total_questions === total, 'the finished game was not recorded');
check(typeof page.meta.has_more === 'boolean' && 'next_cursor' in page.meta, 'results page is missing its cursor meta');

send(host, 'host.close_room');
console.log(`\nPASS · ${total} questions verified against MongoDB · result ${recorded.id} recorded`);
setTimeout(() => process.exit(0), 100);
