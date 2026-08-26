<script>
  // The game card in a channel: the live board, whose turn it is, and the two
  // affordances a game needs (take the empty seat, or resign).
  //
  // It draws the state the FOLD produced — never anything the token said. The
  // seats are authenticated sender fingerprints, the board is the replayed move
  // list, and pressing a column sends a PROPOSAL that this client will re-judge
  // when it comes back round through the channel like everybody else's.
  //
  // Adding a second game means one more entry in BOARDS. Everything else on
  // this card is game-agnostic.
  import FourInARowBoard from "./FourInARowBoard.svelte";
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, flash, memberByFpr, nameFor, sendMessage } from "./lib/state.svelte.js";
  import { haptic } from "./lib/touch.js";
  import { tooltip } from "./lib/tooltip.js";
  import { gameJoin, gameMove, gameResign } from "./lib/games.js";

  const BOARDS = { c4: FourInARowBoard };

  let { game } = $props();

  let busy = $state(false);
  const me = $derived(S.identity.fingerprint);
  const Board = $derived(BOARDS[game.game] || null);
  const mySeat = $derived(game.seats[0] === me ? 0 : game.seats[1] === me ? 1 : -1);
  const canJoin = $derived(!game.seats[1] && !game.invited && mySeat < 0 && !game.over);
  const myTurn = $derived(!game.over && mySeat >= 0 && game.turn === mySeat && !!game.seats[1]);

  // A seat that nobody has claimed, or one claimed by somebody who is not in
  // the member list any more, has no name to show — say what is true instead of
  // inventing a person.
  function seatLabel(i) {
    const fpr = game.seats[i];
    if (!fpr) return game.invited ? "Invited" : "Open seat";
    return nameFor(fpr);
  }

  // Same shape the poll's voter faces use: a member row resolved locally from
  // the fingerprint, never anything the token carried.
  const who = (fpr) => {
    const mem = memberByFpr(fpr);
    return { name: nameFor(fpr), emoji: mem?.emoji || "", color: mem?.color || "", image: mem?.avatar || "" };
  };

  const status = $derived.by(() => {
    if (game.over === "resign") return `${seatLabel(game.winner)} wins — ${seatLabel(1 - game.winner)} resigned`;
    if (game.over === "win") return `${seatLabel(game.winner)} wins`;
    if (game.over === "draw") return "A draw — the board is full";
    if (!game.seats[1]) return game.invited ? `Waiting for ${seatLabel(1)}` : "Waiting for somebody to take the second seat";
    return myTurn ? "Your turn" : `${seatLabel(game.turn)} to play`;
  });

  async function send(token) {
    if (busy) return;
    busy = true;
    try {
      await sendMessage(token, "");
    } catch (err) {
      flash(err);
    }
    busy = false;
  }

  function drop(col) {
    // The sender's own client refuses to send a move it knows is illegal. That
    // is a courtesy to the feed, not a security measure — every receiver
    // re-judges it, including this one when the message comes back.
    if (!myTurn || busy) return;
    if (!game.rules.legal(game.board, col)) return;
    haptic("light");
    send(gameMove(game.id, game.n, col));
  }

  function join() {
    haptic("light");
    send(gameJoin(game.id));
  }

  function resign() {
    if (mySeat < 0 || game.over) return;
    send(gameResign(game.id));
  }
</script>

<div class="game" class:over={!!game.over}>
  <div class="head">
    <Icon name="die" size={14} />
    <span class="title">{game.rules.name}</span>
    <span class="dot" aria-hidden="true">·</span>
    <span class="status" class:live={myTurn}>{status}</span>
  </div>

  <div class="seats">
    {#each [0, 1] as i (i)}
      <span class="seat" class:s1={i === 0} class:s2={i === 1} class:turn={!game.over && game.turn === i && !!game.seats[1]}>
        <span class="pip" aria-hidden="true"></span>
        {#if game.seats[i]}
          {@const w = who(game.seats[i])}
          <Avatar name={w.name} emoji={w.emoji} color={w.color} image={w.image} size={18} />
        {/if}
        <span class="who">{seatLabel(i)}</span>
      </span>
    {/each}
  </div>

  {#if Board}
    <Board
      cells={game.board}
      line={game.line}
      playable={myTurn && !busy}
      onDrop={drop}
    />
  {/if}

  <div class="acts">
    {#if canJoin}
      <button type="button" class="act primary" onclick={join} disabled={busy}>Take the seat</button>
    {/if}
    {#if mySeat >= 0 && !game.over}
      <button type="button" class="act" onclick={resign} disabled={busy} use:tooltip={"Hand this one to your opponent"}>Resign</button>
    {/if}
    {#if mySeat < 0 && !canJoin}
      <span class="watching">Watching</span>
    {/if}
    <span class="count">{game.n} played</span>
  </div>
</div>

<style>
  /* NOT justify-items: start. The board sizes its cells from the grid column
     it sits in, and a start-aligned item is min-content wide — which for a
     grid of aspect-ratio boxes with no intrinsic width is nothing at all. The
     card carries the max-width instead. */
  .game {
    display: grid;
    gap: var(--sp-2);
    margin-top: var(--sp-1);
    padding: var(--sp-3);
    max-width: 360px;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-1);
  }
  .game.over {
    border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    flex-wrap: wrap;
    color: var(--text-muted);
    font-size: var(--fs-small);
  }
  .title {
    font-weight: 600;
    color: var(--text);
  }
  .status.live {
    color: var(--accent-hover);
    font-weight: 600;
  }
  .seats {
    display: flex;
    gap: var(--sp-3);
    flex-wrap: wrap;
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .seat {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .seat.turn .who {
    color: var(--text);
    font-weight: 600;
  }
  .pip {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--bg-3);
  }
  .seat.s1 .pip {
    background: var(--accent);
  }
  .seat.s2 .pip {
    background: var(--warn);
  }
  .acts {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  .act {
    min-height: var(--tap-min);
    padding: var(--sp-1) var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text);
    font-size: var(--fs-compact);
    cursor: pointer;
  }
  .act.primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: 600;
  }
  .act:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .count {
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }
</style>
