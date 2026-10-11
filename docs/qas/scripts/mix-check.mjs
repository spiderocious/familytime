// Plays one 40-question "everything, mixed" game with a silent player and
// reports how the questions were spread across categories. A mixed game should
// touch most categories, however lopsided the bank is.
const BASE = process.env.API_URL ?? 'http://localhost:4100';
const QUESTIONS = 40;

const post = async (path, body) => (await (await fetch(`${BASE}/api/v1${path}`, {
  method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
})).json()).data;

const room = await post('/rooms', { game_id: 'quiz', config: { rounds: 2, questions_per_round: QUESTIONS / 2, pacing: 'host', seconds_per_question: 5 } });
const seat = await post(`/rooms/${room.code}/players`, { name: 'Ada' });
const socket = (role, token) => new WebSocket(`${BASE.replace(/^http/, 'ws')}/ws?code=${room.code}&role=${role}&token=${token}`);

const player = socket('player', seat.player_token);
await new Promise((resolve) => (player.onopen = resolve));
const host = socket('host', room.host_token);

const seen = {};
let last = '';
let started = false;
const timeout = setTimeout(() => { console.error('FAIL: timed out'); process.exit(1); }, 60000);

host.onmessage = (event) => {
  const state = JSON.parse(event.data).state;
  if (!state) return;
  if (state.room.phase === 'lobby') {
    if (!started && state.room.players.some((p) => p.connected)) {
      started = true;
      host.send(JSON.stringify({ type: 'host.start' }));
    }
    return;
  }
  const game = state.game;
  const step = `${game.phase}:${game.round}:${game.question_seq}`;
  if (step === last) return;
  last = step;
  if (game.phase === 'question') seen[game.question.category] = (seen[game.question.category] ?? 0) + 1;
  if (game.phase === 'finished') {
    clearTimeout(timeout);
    const spread = Object.entries(seen).sort((a, b) => b[1] - a[1]);
    console.log(spread.map(([category, n]) => `${category}:${n}`).join('  '));
    const biggest = spread[0][1];
    console.log(`${spread.length} categories in ${QUESTIONS} questions · most from one category: ${biggest}`);
    host.send(JSON.stringify({ type: 'host.close_room' }));
    setTimeout(() => process.exit(spread.length >= 8 && biggest <= QUESTIONS / 3 ? 0 : 1), 100);
    return;
  }
  // Paced under the gateway's per-connection rate limit (15 messages a second).
  setTimeout(() => host.send(JSON.stringify({ type: 'host.next' })), 110);
};
