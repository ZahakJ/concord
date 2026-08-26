// games.js — a game in a channel is a deterministic state machine folded over
// the channel's ordered, authenticated messages.
//
// That sentence is the entire architecture, and it is available here and almost
// nowhere else. Every other chat app's in-app games need the platform's own
// infrastructure — a session service, a matchmaker, an embedded web origin —
// because the platform is the only thing both players agree about. Concord
// already has an ordered, encrypted broadcast channel with a known member list
// and a sender key on every message. A two-player game is a fold over messages
// the app already sends.
//
// So there is no game state on the wire, ever. A move message says only "in
// game <id>, move number <n>, play <this>". Every client replays the whole
// sequence through the rules engine in this file and arrives at the same board,
// which means:
//
//   • a spectator who scrolls into the middle of a game reconstructs it by
//     folding, exactly as the players do;
//   • a player who rejoins on another device needs nothing transferred;
//   • and a MOVE IS A PROPOSAL, never a state update. The sender's client
//     refuses to send an illegal move, and the receiver never trusts that: an
//     illegal move, a move out of turn, a move from somebody with no seat, a
//     duplicate and a malformed one are all recorded as rejected and change
//     NOTHING. They render as "invalid move" rather than erroring, because a
//     move that mutates nothing is not a failure of this client.
//
// CONTAINMENT: moves per game and games per channel are both bounded, the move
// payload is checked against the game's own format before it reaches the rules,
// and the seat a move speaks for is the message's AUTHENTICATED sender — never
// a field in the token. The token can lie about everything except who sent it,
// and who sent it is the only thing the rules consult.
//
// ADDING A SECOND GAME is a new reducer module and a new board component: put
// the reducer in GAMES below and teach GameCard which component draws it.
// Nothing in this file, the token format, the fold or the message plumbing
// knows which game it is carrying.

import { b64urlEncode, b64urlDecode } from "./b64url.js";
import { fourInARow } from "./fourinarow.js";

export const GAME_RE = /\[game\]\(concord:\/\/game\/v1\/([A-Za-z0-9_-]+)\)/;

// The registry. One entry per game; the id is what travels.
export const GAMES = {
  [fourInARow.id]: fourInARow,
};

export const GAME_LIST = Object.values(GAMES);

// Bounds. MAX_GAMES_PER_CHANNEL is what stops a peer opening ten thousand
// instances to make every render walk ten thousand folds; the per-game move cap
// belongs to the game, since a board that cannot hold a 43rd disc should not
// pretend it might.
export const MAX_GAMES_PER_CHANNEL = 24;
export const MAX_TOKEN_CHARS = 200;
const ID_RE = /^[a-z0-9]{4,12}$/;

// ---- the token --------------------------------------------------------------
//
// { k, i, … } — kind and instance id, then whatever that kind needs:
//   new   g   the game id;  o (optional) the fingerprint of the person invited
//   join  —   the sender claims the open seat
//   mv    n   the move number this is meant to be;  m the move itself
//   rs    —   the sender resigns
//
// Forward-compatible the way the poll token is: a kind or a game id this build
// does not know decodes to nothing and renders nothing, rather than being an
// argument.

export function newGameId() {
  return Math.random().toString(36).slice(2, 8);
}

export function encodeGame(t) {
  return `[game](concord://game/v1/${b64urlEncode(JSON.stringify(t))})`;
}

export function gameNew(game, id, opponent = "") {
  const t = { k: "new", g: game, i: id };
  if (opponent) t.o = opponent;
  return encodeGame(t);
}
export const gameJoin = (id) => encodeGame({ k: "join", i: id });
export const gameMove = (id, n, m) => encodeGame({ k: "mv", i: id, n, m });
export const gameResign = (id) => encodeGame({ k: "rs", i: id });

// parseGame(content) -> the token, or null. Everything structural is checked
// here so the fold below can be about RULES rather than about shapes.
export function parseGame(content) {
  if (!content) return null;
  const m = content.match(GAME_RE);
  if (!m || m[1].length > MAX_TOKEN_CHARS) return null;
  let t;
  try {
    t = JSON.parse(b64urlDecode(m[1]));
  } catch {
    return null;
  }
  if (!t || typeof t !== "object" || typeof t.i !== "string" || !ID_RE.test(t.i)) return null;
  switch (t.k) {
    case "new": {
      if (typeof t.g !== "string" || !GAMES[t.g]) return null;
      const out = { k: "new", i: t.i, g: t.g };
      // An opponent is a fingerprint, and a fingerprint is only ever compared
      // against the member list — never displayed from the token and never
      // trusted as a claim. A malformed one just makes the seat open.
      if (typeof t.o === "string" && t.o.length > 0 && t.o.length <= 80) out.o = t.o;
      return out;
    }
    case "join":
      return { k: "join", i: t.i };
    case "rs":
      return { k: "rs", i: t.i };
    case "mv": {
      if (!Number.isInteger(t.n) || t.n < 0 || t.n > 4096) return null;
      // The move itself is checked against the GAME's format in the fold,
      // where the game is known. Here it only has to be a small JSON value.
      if (t.m === undefined || typeof t.m === "object") return null;
      return { k: "mv", i: t.i, n: t.n, m: t.m };
    }
    default:
      return null;
  }
}

export function stripGame(content) {
  return content ? content.replace(GAME_RE, "").trim() : content;
}

// ---- the fold ---------------------------------------------------------------

function fresh(tok, by, at) {
  const rules = GAMES[tok.g];
  return {
    id: tok.i,
    game: tok.g,
    rules,
    seats: [by, tok.o && tok.o !== by ? tok.o : null],
    // An invited opponent's seat is theirs and nobody else's; an uninvited
    // game's second seat is open to the first person who claims it.
    invited: !!(tok.o && tok.o !== by),
    board: rules.create(),
    turn: 0,
    n: 0,
    over: "", // "" | "win" | "draw" | "resign"
    winner: -1,
    line: null,
    last: -1,
    startedAt: at,
    headId: "",
    // messageId -> what this row should say. A row that mutated nothing still
    // has to render as something, and "invalid move" is the honest something.
    notes: new Map(),
  };
}

// note records what a row should say and answers the question the fold cares
// about: did this event CHANGE the game? Everything refused answers false, and
// false is what keeps the board anchored to the last real move rather than
// following a stranger's rejected proposal down the feed.
const REFUSED = new Set(["bad", "dup"]);
function note(g, msgId, kind, text) {
  g.notes.set(msgId, { kind, text });
  return !REFUSED.has(kind);
}

const seatOf = (g, by) => (g.seats[0] === by ? 0 : g.seats[1] === by ? 1 : -1);

// applyEvent folds one message into one game. Everything it decides is written
// into `g`, including the reason a proposal was refused; it returns whether the
// game actually changed.
function applyEvent(g, ev) {
  const { tok, by, id } = ev;
  if (tok.k === "new") {
    // A second "new" for an id that already exists is somebody replaying, or
    // a collision. The first one wins; nothing is overwritten.
    return note(g, id, "dup", "duplicate game");
  }
  if (tok.k === "join") {
    // Ordered so the note says the most useful true thing. An invited game has
    // its second seat filled from the first message, so "seat already taken"
    // would be true of a gatecrasher and useless to them.
    if (by === g.seats[0]) return note(g, id, "bad", "you cannot play yourself");
    if (g.seats[1] === by) return note(g, id, "bad", "you already have a seat");
    if (g.invited) return note(g, id, "bad", "this game was for someone else");
    if (g.seats[1]) return note(g, id, "bad", "seat already taken");
    g.seats[1] = by;
    return note(g, id, "join", "joined");
  }
  if (tok.k === "rs") {
    const seat = seatOf(g, by);
    if (seat < 0) return note(g, id, "bad", "not a player");
    if (g.over) return note(g, id, "bad", "the game was already over");
    g.over = "resign";
    g.winner = 1 - seat;
    return note(g, id, "resign", "resigned");
  }
  // A move. Five gates, in the order that makes the cheapest refusal first.
  if (g.over) return note(g, id, "bad", "the game was already over");
  if (!g.seats[1]) return note(g, id, "bad", "nobody has taken the second seat");
  const seat = seatOf(g, by);
  // THE SEAT GATE. `by` is the message's authenticated sender, not anything the
  // token said. A spectator's move is not a move.
  if (seat < 0) return note(g, id, "bad", "not a player in this game");
  if (seat !== g.turn) return note(g, id, "bad", "not their turn");
  // The move number is what makes a replayed message inert: a duplicate says
  // the move index that has already happened.
  if (tok.n !== g.n) return note(g, id, "dup", "out of sequence");
  if (g.n >= g.rules.maxMoves) return note(g, id, "bad", "this game is full");
  if (!g.rules.validMove(tok.m)) return note(g, id, "bad", "not a move in this game");
  const board = g.rules.apply(g.board, seat, tok.m);
  if (!board) return note(g, id, "bad", "illegal move");
  // Past here the move is accepted, and this is the only place in the file
  // that writes to the board.
  g.board = board;
  g.last = tok.m;
  g.n++;
  const res = g.rules.outcome(board, tok.m);
  if (res.over) {
    g.over = res.win ? "win" : "draw";
    g.winner = res.win ? seat : -1;
    g.line = res.line;
  } else {
    g.turn = 1 - seat;
  }
  return note(g, id, "move", g.rules.describe(tok.m));
}

// foldGames(events) -> Map(instanceId -> state).
//
// `events` are { id, by, tok } in channel order — `by` being the message's
// authenticated sender fingerprint. Everything derived; nothing read off the
// wire but the proposals themselves.
export function foldGames(events) {
  const out = new Map();
  for (const ev of events) {
    if (!ev.tok) continue;
    if (ev.tok.k === "new") {
      if (out.has(ev.tok.i)) {
        // Fold the duplicate into the existing game so the row still renders.
        applyEvent(out.get(ev.tok.i), ev);
        continue;
      }
      // The games-per-channel cap. Past it, a "new" is simply not a game — the
      // move messages that follow find no instance and render as an aside.
      if (out.size >= MAX_GAMES_PER_CHANNEL) continue;
      const g = fresh(ev.tok, ev.by, ev.at);
      g.headId = ev.id;
      out.set(g.id, g);
      note(g, ev.id, "new", "started");
      continue;
    }
    const g = out.get(ev.tok.i);
    if (!g) continue; // a move in a game we have not seen the start of
    // The board is drawn on the latest message that actually MOVED the game, so
    // the live position sits where the action is — and a rejected proposal
    // cannot drag the card down the feed behind it.
    if (applyEvent(g, ev)) g.headId = ev.id;
  }
  return out;
}

// ---- the channel-level view -------------------------------------------------

// Folding is cheap but it is not free, and every visible row asks the same
// question about the same array. S.messages is REPLACED on every change, so its
// identity is a correct and very cheap cache key.
let cacheKey = null;
let cacheVal = new Map();

export function channelGames(messages) {
  if (messages === cacheKey) return cacheVal;
  const events = [];
  for (const m of messages || []) {
    if (m.deleted) continue;
    const tok = parseGame(m.content);
    if (tok) events.push({ id: m.id, by: m.sender, tok, at: m.sent });
  }
  cacheKey = messages;
  cacheVal = foldGames(events);
  return cacheVal;
}

// gameAt(messages, message) -> what this ROW should draw: the live card when it
// is the newest message of its game, a one-line aside otherwise, or null when
// the message carries no game token at all.
export function gameAt(messages, m) {
  const tok = parseGame(m?.content);
  if (!tok) return null;
  const g = channelGames(messages).get(tok.i);
  if (!g) return { kind: "orphan", note: { kind: "bad", text: "a game that started earlier" } };
  if (g.headId === m.id) return { kind: "card", game: g, note: g.notes.get(m.id) || null };
  return { kind: "note", game: g, note: g.notes.get(m.id) || null };
}
