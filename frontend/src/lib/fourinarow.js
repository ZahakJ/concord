// fourinarow.js — the rules of four in a row, as a pure function of a board.
//
// This module knows nothing about messages, senders, turns or seats. It is
// handed a board and a move and says whether the move is legal and what the
// board becomes; whose turn it is and whether they are allowed to speak for
// that seat belong to the engine in games.js, because those questions are the
// same for every game and these are not.
//
// That split is the whole point of the arrangement. A second game is this file
// again with different rules and a different board component — nothing in the
// engine, the token format, the fold or the card plumbing has to know which
// game it is carrying.
//
// Nothing here mutates its input. The engine folds the whole move list from
// scratch on every render, so a rule that quietly wrote into the board it was
// given would corrupt the fold the moment the same state was reused.

export const COLS = 7;
export const ROWS = 6;

// Cells run left to right, top row first. 0 empty, 1 the first seat, 2 the
// second. A flat array rather than nested rows: it compares cleanly in a test,
// it serializes without thought, and every win check below is a stride.
export function create() {
  return new Array(COLS * ROWS).fill(0);
}

const at = (cells, c, r) => cells[r * COLS + c];

// The row a disc dropped into column c would land in, or -1 when the column is
// full. Gravity is the only thing that makes a move legal here, which is why
// this is the whole legality check.
export function landing(cells, c) {
  if (!Number.isInteger(c) || c < 0 || c >= COLS) return -1;
  for (let r = ROWS - 1; r >= 0; r--) {
    if (at(cells, c, r) === 0) return r;
  }
  return -1;
}

export function legal(cells, move) {
  return landing(cells, move) >= 0;
}

// apply returns a NEW board, or null when the move is not legal. null is the
// engine's signal to record a rejected proposal and change nothing — the same
// fail-closed shape every other token in the app has.
export function apply(cells, seat, move) {
  const r = landing(cells, move);
  if (r < 0) return null;
  if (seat !== 0 && seat !== 1) return null;
  const next = cells.slice();
  next[r * COLS + move] = seat + 1;
  return next;
}

const DIRS = [
  [1, 0], // ─
  [0, 1], // │
  [1, 1], // ╲
  [1, -1], // ╱
];

// winningLine returns the four (or more) cell indices that end the game, or
// null. It scans from the cell just played rather than the whole board: only a
// line through the new disc can have just been completed, and walking outward
// from it is a dozen reads instead of eighty-four.
export function winningLine(cells, move) {
  // The disc just played sits on top of its column.
  let r = -1;
  for (let i = 0; i < ROWS; i++) {
    if (at(cells, move, i) !== 0) {
      r = i;
      break;
    }
  }
  if (r < 0) return null;
  const me = at(cells, move, r);
  for (const [dc, dr] of DIRS) {
    const line = [r * COLS + move];
    for (const sign of [1, -1]) {
      let c2 = move + dc * sign;
      let r2 = r + dr * sign;
      while (c2 >= 0 && c2 < COLS && r2 >= 0 && r2 < ROWS && at(cells, c2, r2) === me) {
        line.push(r2 * COLS + c2);
        c2 += dc * sign;
        r2 += dr * sign;
      }
    }
    if (line.length >= 4) return line.sort((a, b) => a - b);
  }
  return null;
}

export function full(cells) {
  for (let c = 0; c < COLS; c++) {
    if (landing(cells, c) >= 0) return false;
  }
  return true;
}

// The reducer the registry holds. `outcome` is called after a successful move
// and answers the only three things the engine needs to know about any game:
// is it over, who won, and which cells to light up.
export const fourInARow = {
  id: "c4",
  name: "Four in a row",
  blurb: "Drop a disc down a column. Four in a line — any direction — wins.",
  seats: 2,
  // 42 cells, so 42 moves is a full board and there can never be a 43rd.
  maxMoves: COLS * ROWS,
  create,
  legal,
  apply,
  outcome(cells, move) {
    const line = winningLine(cells, move);
    if (line) return { over: true, win: true, line };
    if (full(cells)) return { over: true, draw: true, line: null };
    return { over: false, line: null };
  },
  // How a move reads in the feed when its card is scrolled away. Columns are
  // named from one, because nobody counts a board from zero out loud.
  describe(move) {
    return `column ${move + 1}`;
  },
  // The wire form of a move is one small integer. validMove is the FORMAT
  // check — is this a thing this game's moves are made of — as distinct from
  // legal(), which asks whether it can be played right now.
  validMove(m) {
    return Number.isInteger(m) && m >= 0 && m < COLS;
  },
};
