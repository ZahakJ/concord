// Tests for the game engine and the four-in-a-row rules.
//
// The engine's whole safety claim is one sentence: a move is a PROPOSAL,
// re-validated locally against the rules and against the AUTHENTICATED sender's
// seat, and anything refused changes nothing. So most of what follows is a
// catalogue of refusals — illegal, out of turn, from a spectator, duplicated,
// malformed, after the end — each one checked twice: the game did not change,
// and the row that carried it says so.
//
// The board rules get the same treatment on their own, because a reducer that
// is wrong about a diagonal is wrong on every peer at once, identically, which
// is the one kind of bug this architecture cannot detect for itself.
import {
  parseGame,
  stripGame,
  encodeGame,
  gameNew,
  gameJoin,
  gameMove,
  gameResign,
  foldGames,
  channelGames,
  gameAt,
  isGameToken,
  GAMES,
  GAME_LIST,
  MAX_GAMES_PER_CHANNEL,
  MAX_TOKEN_CHARS,
  newGameId,
} from "./games.js";
import { create, landing, legal, apply, winningLine, full, fourInARow, COLS, ROWS } from "./fourinarow.js";

let failures = 0;
function check(name, got, want) {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) {
    failures++;
    console.error(`FAIL ${name}\n  got:  ${g}\n  want: ${w}`);
  }
}

const ANA = "AAAA 1111";
const BO = "BBBB 2222";
const CAL = "CCCC 3333";

// A channel, as the fold sees it: a list of { id, by, content }.
let seq = 0;
const msg = (by, content) => ({ id: `m${++seq}`, sender: by, content, sent: "2026-08-26T10:00:00Z" });

function play(rows) {
  return channelGames(rows);
}

// ---- four in a row, on its own ----------------------------------------------

{
  const cells = create();
  check("a new board is empty", cells.filter((c) => c !== 0).length, 0);
  check("every column starts open", [...Array(COLS).keys()].map((c) => landing(cells, c)), Array(COLS).fill(ROWS - 1));
  check("a column off the board has no landing", landing(cells, COLS), -1);
  check("a negative column has no landing", landing(cells, -1), -1);
  check("a fractional column has no landing", landing(cells, 1.5), -1);
  check("a string column has no landing", landing(cells, "1"), -1);
}

// Gravity, and a column that fills up.
{
  let cells = create();
  for (let i = 0; i < ROWS; i++) {
    check(`disc ${i + 1} lands on the stack`, landing(cells, 3), ROWS - 1 - i);
    cells = apply(cells, i % 2, 3);
  }
  check("a full column is illegal", legal(cells, 3), false);
  check("apply refuses a full column", apply(cells, 0, 3), null);
  check("the neighbours are untouched", legal(cells, 2) && legal(cells, 4), true);
}

// apply never mutates what it was handed — the fold reuses boards.
{
  const cells = create();
  const before = cells.slice();
  apply(cells, 0, 2);
  check("apply leaves its input alone", cells, before);
}

// The four ways to win, each built by hand and each checked at the moment the
// fourth disc lands rather than by scanning the finished board.
{
  // horizontal
  let c = create();
  for (const col of [0, 1, 2]) c = apply(c, 0, col);
  check("three in a row is not a win", winningLine(c, 2), null);
  c = apply(c, 0, 3);
  check("four across wins", winningLine(c, 3)?.length, 4);
}
{
  // vertical
  let c = create();
  for (let i = 0; i < 3; i++) c = apply(c, 1, 5);
  check("three stacked is not a win", winningLine(c, 5), null);
  c = apply(c, 1, 5);
  check("four stacked wins", winningLine(c, 5)?.length, 4);
}
{
  // rising diagonal: build the staircase under it first
  let c = create();
  c = apply(c, 0, 0);
  c = apply(c, 1, 1);
  c = apply(c, 0, 1);
  c = apply(c, 1, 2);
  c = apply(c, 1, 2);
  c = apply(c, 0, 2);
  c = apply(c, 1, 3);
  c = apply(c, 1, 3);
  c = apply(c, 1, 3);
  check("the staircase is not yet a win", winningLine(c, 3), null);
  c = apply(c, 0, 3);
  check("four on the rising diagonal wins", winningLine(c, 3)?.length, 4);
}
{
  // falling diagonal, the mirror of the above
  let c = create();
  c = apply(c, 0, 6);
  c = apply(c, 1, 5);
  c = apply(c, 0, 5);
  c = apply(c, 1, 4);
  c = apply(c, 1, 4);
  c = apply(c, 0, 4);
  c = apply(c, 1, 3);
  c = apply(c, 1, 3);
  c = apply(c, 1, 3);
  c = apply(c, 0, 3);
  check("four on the falling diagonal wins", winningLine(c, 3)?.length, 4);
}
// Five in a row is still a win, and the whole line is reported — a renderer
// that lit up only four of five would look like a bug in the rules.
{
  let c = create();
  for (const col of [0, 1, 2, 4]) c = apply(c, 0, col);
  c = apply(c, 0, 3);
  check("five in a row lights all five", winningLine(c, 3)?.length, 5);
}
// A line of somebody else's discs is not your win.
{
  let c = create();
  for (const col of [0, 1, 2]) c = apply(c, 0, col);
  c = apply(c, 1, 3);
  check("an opponent's disc breaks the line", winningLine(c, 3), null);
}

// A full board with no line is a draw. There is no tidy hand-written pattern
// that manages it — every obvious tiling leaves a long diagonal — so the drawn
// position is SEARCHED for: fill the board one alternating disc at a time,
// backtracking whenever a placement completes a line. The result is a real
// game somebody could have played, which is the only kind worth testing.
function anyLine(cells) {
  for (let c = 0; c < COLS; c++) {
    for (let r = 0; r < ROWS; r++) {
      const me = cells[r * COLS + c];
      if (!me) continue;
      for (const [dc, dr] of [
        [1, 0],
        [0, 1],
        [1, 1],
        [1, -1],
      ]) {
        let n = 1;
        let c2 = c + dc;
        let r2 = r + dr;
        while (c2 >= 0 && c2 < COLS && r2 >= 0 && r2 < ROWS && cells[r2 * COLS + c2] === me) {
          n++;
          if (n >= 4) return true;
          c2 += dc;
          r2 += dr;
        }
      }
    }
  }
  return false;
}

function findDraw() {
  const script = [];
  let steps = 0;
  const walk = (cells, seat) => {
    if (++steps > 400000) return null;
    if (full(cells)) return cells;
    // A rotating column order, so the search does not spend its whole budget
    // stacking column 0 and backing out of it.
    for (let k = 0; k < COLS; k++) {
      const col = (k + script.length) % COLS;
      const next = apply(cells, seat, col);
      if (!next) continue;
      if (winningLine(next, col)) continue;
      script.push([seat, col]);
      const done = walk(next, 1 - seat);
      if (done) return done;
      script.pop();
    }
    return null;
  };
  const cells = walk(create(), 0);
  return cells ? { cells, script } : null;
}

const drawn = findDraw();
if (!drawn) {
  failures++;
  console.error("FAIL could not construct a drawn board");
} else {
  check("the drawn board is full", full(drawn.cells), true);
  check("and holds no line anywhere", anyLine(drawn.cells), false);
  check("it took every square", drawn.script.length, COLS * ROWS);
  check("outcome calls it a draw", fourInARow.outcome(drawn.cells, drawn.script[drawn.script.length - 1][1]).draw, true);
}

check("the move format admits a column", fourInARow.validMove(0) && fourInARow.validMove(COLS - 1), true);
check("the move format refuses a column off the board", fourInARow.validMove(COLS), false);
check("the move format refuses a fraction", fourInARow.validMove(2.5), false);
check("the move format refuses a string", fourInARow.validMove("3"), false);
check("the move format refuses nothing at all", fourInARow.validMove(undefined), false);
check("a move reads as a human column", fourInARow.describe(3), "column 4");

// ---- the token --------------------------------------------------------------

{
  const id = "abc123";
  check("a new-game token round-trips", parseGame(gameNew("c4", id, BO)), { k: "new", i: id, g: "c4", o: BO });
  check("an open game has no opponent", parseGame(gameNew("c4", id)), { k: "new", i: id, g: "c4" });
  check("a join round-trips", parseGame(gameJoin(id)), { k: "join", i: id });
  check("a move round-trips", parseGame(gameMove(id, 3, 5)), { k: "mv", i: id, n: 3, m: 5 });
  check("a resign round-trips", parseGame(gameResign(id)), { k: "rs", i: id });
  check("a token parses out of a body with words", parseGame(`good luck ${gameMove(id, 0, 1)}`)?.m, 1);
  check("strip leaves the words", stripGame(`good luck ${gameMove(id, 0, 1)}`), "good luck");
  check("strip removes a token this client refused", stripGame("hi [game](concord://game/v1/AAAA)"), "hi");
  check("an id is generated in the right shape", /^[a-z0-9]{4,12}$/.test(newGameId()), true);
}

// Malformed tokens are not games. Each of these renders nothing.
check("no token, no game", parseGame("hello"), null);
check("garbage payload", parseGame("[game](concord://game/v1/!!!)"), null);
check("an unknown kind", parseGame(encodeGame({ k: "wat", i: "abc123" })), null);
check("a game id we do not have", parseGame(encodeGame({ k: "new", g: "chess", i: "abc123" })), null);
check("an instance id with punctuation in it", parseGame(encodeGame({ k: "join", i: "../../etc" })), null);
check("an instance id far too long", parseGame(encodeGame({ k: "join", i: "a".repeat(200) })), null);
check("a move with no number", parseGame(encodeGame({ k: "mv", i: "abc123", m: 1 })), null);
check("a move number that is not an integer", parseGame(encodeGame({ k: "mv", i: "abc123", n: 1.5, m: 1 })), null);
check("a move number past the cap", parseGame(encodeGame({ k: "mv", i: "abc123", n: 99999, m: 1 })), null);
check("a move whose payload is an object", parseGame(encodeGame({ k: "mv", i: "abc123", n: 0, m: { evil: 1 } })), null);
check("a move whose payload is an array", parseGame(encodeGame({ k: "mv", i: "abc123", n: 0, m: [1, 2] })), null);
check("a payload past the character cap", parseGame(`[game](concord://game/v1/${"A".repeat(MAX_TOKEN_CHARS + 1)})`), null);
// An opponent field long enough to be a payload rather than a fingerprint is
// dropped, which leaves the seat open rather than refusing the game.
check("an over-long opponent field is dropped, not fatal", parseGame(encodeGame({ k: "new", g: "c4", i: "abc123", o: "x".repeat(90) })), {
  k: "new",
  i: "abc123",
  g: "c4",
});
// …and one long enough to be a payload rather than a field never reaches the
// parse at all: the character cap is checked before the base64 is decoded.
check("an opponent field used as a payload is refused on length", parseGame(encodeGame({ k: "new", g: "c4", i: "abc123", o: "x".repeat(400) })), null);

// ---- the fold: a whole game -------------------------------------------------

{
  seq = 0;
  const id = "g1a2b3";
  const rows = [msg(ANA, gameNew("c4", id)), msg(BO, gameJoin(id))];
  // Ana takes the middle column four times; Bo answers alongside.
  const script = [
    [ANA, 3],
    [BO, 0],
    [ANA, 3],
    [BO, 1],
    [ANA, 3],
    [BO, 2],
    [ANA, 3],
  ];
  script.forEach(([who, col], i) => rows.push(msg(who, gameMove(id, i, col))));
  const g = play(rows).get(id);
  check("both seats are filled", g.seats, [ANA, BO]);
  check("seven moves landed", g.n, 7);
  check("Ana won", g.over, "win");
  check("…and the winner is seat 0", g.winner, 0);
  check("the winning line is four cells", g.line.length, 4);
  // Every message got a note, so no row in the feed is a blank.
  check("every row has something to say", rows.every((r) => g.notes.has(r.id)), true);
}

// A draw folds to a draw — the same searched position, played out as messages.
if (drawn) {
  seq = 0;
  const id = "draw01";
  const rows = [msg(ANA, gameNew("c4", id)), msg(BO, gameJoin(id))];
  drawn.script.forEach(([seat, col], i) => rows.push(msg(seat === 0 ? ANA : BO, gameMove(id, i, col))));
  const g = play(rows).get(id);
  check("a full board with no line is a draw", g.over, "draw");
  check("a draw has no winner", g.winner, -1);
  check("forty-two discs landed", g.n, COLS * ROWS);
  check("the last move was still accepted", g.notes.get(rows[rows.length - 1].id).kind, "move");
}

// ---- the fold: everything refused -------------------------------------------

function refused(name, rows, id, expect) {
  const g = play(rows).get(id);
  const last = rows[rows.length - 1];
  check(`${name}: the row says so`, g.notes.get(last.id)?.text, expect);
  return g;
}

{
  seq = 0;
  const id = "ref001";
  const start = [msg(ANA, gameNew("c4", id)), msg(BO, gameJoin(id))];

  // A spectator's move. THE seat gate: `by` is the authenticated sender, and a
  // token cannot claim to be somebody else.
  {
    const rows = [...start, msg(CAL, gameMove(id, 0, 3))];
    const g = refused("a spectator's move", rows, id, "not a player in this game");
    check("a spectator's move changes nothing", g.n, 0);
  }

  // Out of turn: seat 1 moving first.
  {
    const rows = [...start, msg(BO, gameMove(id, 0, 3))];
    const g = refused("out of turn", rows, id, "not their turn");
    check("out of turn changes nothing", g.n, 0);
  }

  // A duplicate: the same move number twice. This is what makes a replayed
  // message inert rather than a second disc.
  {
    const rows = [...start, msg(ANA, gameMove(id, 0, 3)), msg(BO, gameMove(id, 1, 0)), msg(ANA, gameMove(id, 0, 4))];
    const g = refused("a replayed move number", rows, id, "out of sequence");
    check("a duplicate changes nothing", g.n, 2);
  }

  // A move into a full column.
  {
    const rows = [...start];
    for (let i = 0; i < 6; i++) rows.push(msg(i % 2 === 0 ? ANA : BO, gameMove(id, i, 3)));
    rows.push(msg(ANA, gameMove(id, 6, 3)));
    const g = refused("a full column", rows, id, "illegal move");
    check("an illegal move changes nothing", g.n, 6);
  }

  // A move that is not a move in this game.
  {
    const rows = [...start, msg(ANA, gameMove(id, 0, 99))];
    const g = refused("a column off the board", rows, id, "not a move in this game");
    check("a malformed move changes nothing", g.n, 0);
  }
  {
    const rows = [...start, msg(ANA, gameMove(id, 0, -3))];
    refused("a negative column", rows, id, "not a move in this game");
  }
  {
    const rows = [...start, msg(ANA, gameMove(id, 0, true))];
    refused("a boolean move", rows, id, "not a move in this game");
  }

  // After the end.
  {
    const rows = [...start, msg(ANA, gameResign(id)), msg(BO, gameMove(id, 0, 3))];
    const g = refused("a move after a resignation", rows, id, "the game was already over");
    check("a resignation ends it", g.over, "resign");
    check("…and the other seat wins", g.winner, 1);
  }
  {
    const rows = [...start, msg(ANA, gameResign(id)), msg(BO, gameResign(id))];
    const g = refused("resigning a finished game", rows, id, "the game was already over");
    check("the first resignation still stands", g.winner, 1);
  }

  // A spectator cannot resign somebody else's game.
  {
    const rows = [...start, msg(CAL, gameResign(id))];
    const g = refused("a spectator resigning", rows, id, "not a player");
    check("…and the game is still live", g.over, "");
  }

  // Moving before anybody has taken the second seat.
  {
    seq = 100;
    const solo = "solo01";
    const rows = [msg(ANA, gameNew("c4", solo)), msg(ANA, gameMove(solo, 0, 3))];
    const g = refused("a move with one seat empty", rows, solo, "nobody has taken the second seat");
    check("a move before the game starts changes nothing", g.n, 0);
  }

  // Joining twice, joining your own game, and joining a game that named
  // somebody else.
  {
    const rows = [...start, msg(CAL, gameJoin(id))];
    refused("joining a full game", rows, id, "seat already taken");
  }
  {
    seq = 200;
    const own = "own001";
    const rows = [msg(ANA, gameNew("c4", own)), msg(ANA, gameJoin(own))];
    const g = refused("joining your own game", rows, own, "you cannot play yourself");
    check("…and the seat stays open", g.seats[1], null);
  }
  {
    seq = 300;
    const inv = "inv001";
    const rows = [msg(ANA, gameNew("c4", inv, BO)), msg(CAL, gameJoin(inv))];
    const g = refused("gatecrashing an invitation", rows, inv, "this game was for someone else");
    const rowsRedundant = [msg(ANA, gameNew("c4", inv, BO)), msg(BO, gameJoin(inv))];
    check("the invited player joining again is told they already have a seat", play(rowsRedundant).get(inv).notes.get(rowsRedundant[1].id).text, "you already have a seat");
    check("the invited seat is still theirs", g.seats[1], BO);
    // …and the invited player needs no join at all: the seat was theirs from
    // the first message.
    const rows2 = [...rows, msg(BO, gameMove(inv, 0, 3))];
    check("the invited player just plays", play(rows2).get(inv).n, 0);
    const rows3 = [msg(ANA, gameNew("c4", inv, BO)), msg(ANA, gameMove(inv, 0, 3)), msg(BO, gameMove(inv, 1, 4))];
    check("an invited game needs no join", play(rows3).get(inv).n, 2);
  }

  // A second "new" for a live game does not reset it.
  {
    seq = 400;
    const dup = "dup001";
    const rows = [msg(ANA, gameNew("c4", dup)), msg(BO, gameJoin(dup)), msg(ANA, gameMove(dup, 0, 3)), msg(CAL, gameNew("c4", dup))];
    const g = refused("a second new for a live game", rows, dup, "duplicate game");
    check("the game is not reset", g.n, 1);
    check("and the seats are unchanged", g.seats, [ANA, BO]);
  }
}

// ---- bounds -----------------------------------------------------------------

{
  seq = 1000;
  const rows = [];
  for (let i = 0; i < MAX_GAMES_PER_CHANNEL + 10; i++) rows.push(msg(ANA, gameNew("c4", `x${String(i).padStart(5, "0")}`)));
  const all = foldGames(
    rows.map((r) => ({ id: r.id, by: r.sender, tok: parseGame(r.content), at: r.sent })),
  );
  check("games per channel are capped", all.size, MAX_GAMES_PER_CHANNEL);
}

// The per-game move cap is the game's own: a board that holds 42 discs can
// never take a 43rd, so the cap and the rules agree by construction.
check("the move cap is the board", GAMES.c4.maxMoves, COLS * ROWS);

// ---- what a row draws -------------------------------------------------------

{
  seq = 2000;
  const id = "row001";
  const rows = [msg(ANA, gameNew("c4", id)), msg(BO, gameJoin(id)), msg(ANA, gameMove(id, 0, 3))];
  check("a plain message draws no game", gameAt(rows, msg(ANA, "hello")), null);
  // ONE CARD. The board is drawn on the message that opened the game and stays
  // there for the whole game; every accepted event after it folds in and draws
  // no row at all. An eight-move game used to be ten grey one-liners and a
  // board; a real one runs twenty to forty turns.
  check("the opening message draws the board", gameAt(rows, rows[0]).kind, "card");
  check("an accepted join is not a row", gameAt(rows, rows[1]).kind, "folded");
  check("an accepted move is not a row", gameAt(rows, rows[2]).kind, "folded");
  check("and the card carries the history", play(rows).get(id).log.length, 2);
  check("history says who and what", play(rows).get(id).log[1], {
    id: rows[2].id,
    by: ANA,
    seat: 0,
    text: "column 4",
    at: rows[2].sent,
  });
  // A REFUSED proposal keeps its row: it is the only place the app can say why
  // a move did nothing. It is also not history, so it never reaches the log.
  const withBad = [...rows, msg(CAL, gameMove(id, 1, 4))];
  check("a refused move is an aside", gameAt(withBad, withBad[3]).kind, "note");
  check("…and says why", gameAt(withBad, withBad[3]).note.text, "not a player in this game");
  check("…and is not in the history", play(withBad).get(id).log.length, 2);
  check("…and the board is still on the opening message", gameAt(withBad, withBad[0]).kind, "card");
  // A move in a game whose opening message is not loaded.
  check("an orphaned move is an aside", gameAt([], msg(ANA, gameMove("zzz999", 0, 1))).kind, "orphan");
}

// isGameToken decides whether a message counts towards an unread badge, and it
// deliberately does NOT consult the fold: whether a badge counts a message must
// not depend on how much history happens to be loaded. It must also agree with
// store.IsQuiet in Go, which asks the same question about the same body when
// the row is written.
{
  const tok = gameMove("tok001", 0, 3);
  check("a bare move token is bookkeeping", isGameToken(tok), true);
  check("padded with whitespace, still bookkeeping", isGameToken(`  ${tok}\n`), true);
  check("words around it and it is somebody talking", isGameToken(`nice one ${tok}`), false);
  check("a plain message is not", isGameToken("hello"), false);
  check("an empty body is not", isGameToken(""), false);
  check("a poll is not", isGameToken("[poll](concord://poll/v1/abc)"), false);
}

// Every registered game answers the whole reducer contract, so a second game
// cannot be half-added.
for (const g of GAME_LIST) {
  for (const k of ["id", "name", "blurb", "seats", "maxMoves", "create", "legal", "apply", "outcome", "describe", "validMove"]) {
    if (g[k] === undefined) {
      failures++;
      console.error(`FAIL game "${g.id}" is missing ${k}`);
    }
  }
  check(`"${g.id}" registers under its own id`, GAMES[g.id], g);
}

if (failures) {
  console.error(`\n${failures} game test(s) failed`);
  process.exit(1);
}
console.log(`games.js: all tests passed (${GAME_LIST.length} game, ${COLS}x${ROWS} board)`);
