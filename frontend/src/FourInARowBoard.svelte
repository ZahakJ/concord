<script>
  // The board for four in a row. Purely a view over the folded state: it is
  // handed cells and told whether this viewer may drop a disc, and it reports a
  // column back. It knows nothing about messages, seats or turns.
  //
  // A second game is a second component of exactly this shape, chosen by
  // GameCard from the game id. Nothing about the token, the fold or the message
  // plumbing changes.
  import { COLS, ROWS, landing } from "./lib/fourinarow.js";

  let { cells, line = null, playable = false, onDrop = null } = $props();

  let hover = $state(-1);
  const lit = $derived(new Set(line || []));

  function drop(c) {
    if (!playable || landing(cells, c) < 0) return;
    onDrop?.(c);
  }
</script>

<div class="board" class:playable role="group" aria-label="Four in a row board">
  {#each Array(COLS) as _, c (c)}
    <!-- One button per COLUMN, not per cell. Dropping is a column choice, so a
         column is the target: 42 tiny cells would be 42 tab stops for one
         decision, and on a phone none of them would clear the tap floor. -->
    <button
      type="button"
      class="col"
      class:hot={hover === c && playable && landing(cells, c) >= 0}
      disabled={!playable || landing(cells, c) < 0}
      aria-label={`Column ${c + 1}${landing(cells, c) < 0 ? ", full" : ""}`}
      onclick={() => drop(c)}
      onpointerenter={() => (hover = c)}
      onpointerleave={() => (hover = -1)}
      onfocus={() => (hover = c)}
      onblur={() => (hover = -1)}
    >
      {#each Array(ROWS) as _2, r (r)}
        {@const v = cells[r * COLS + c]}
        <span
          class="cell"
          class:p1={v === 1}
          class:p2={v === 2}
          class:win={lit.has(r * COLS + c)}
          class:ghost={v === 0 && hover === c && playable && landing(cells, c) === r}
        ></span>
      {/each}
    </button>
  {/each}
</div>

<style>
  .board {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    gap: 2px;
    padding: var(--sp-1);
    width: 100%;
    max-width: 300px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .col {
    display: grid;
    gap: 2px;
    padding: 2px;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    cursor: default;
  }
  .board.playable .col:not(:disabled) {
    cursor: pointer;
  }
  .col.hot {
    background: var(--accent-soft);
    color: var(--text);
  }
  .cell {
    aspect-ratio: 1;
    border-radius: 50%;
    background: var(--bg-3);
    transition: background var(--dur-quick) var(--ease-out);
  }
  /* Two seats, two colours. They come from the PLAYERS now — GameCard sets
     --seat-1/--seat-2 from each player's own colour, so a face and its discs
     agree and nobody changes colour between a game and its rematch — and fall
     back to this fixed pair when a seat is empty or the two are too close to
     tell apart on a grid. Either way the winning line also gets a ring, so a
     colour-blind reader still sees which four ended it. */
  .cell.p1 {
    background: var(--seat-1, var(--accent));
  }
  .cell.p2 {
    background: var(--seat-2, var(--warn));
  }
  .cell.ghost {
    background: color-mix(in srgb, var(--seat-1, var(--accent)) 30%, var(--bg-3));
  }
  .cell.win {
    box-shadow: 0 0 0 2px var(--text);
  }

  @media (prefers-reduced-motion: reduce) {
    .cell {
      transition: none;
    }
  }
</style>
