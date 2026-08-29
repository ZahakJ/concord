<script>
  // Start a game in this channel: pick the game, then either open the seat to
  // whoever takes it first or name an opponent.
  //
  // The whole dialog produces one ordinary message. Nothing is registered
  // anywhere, no session exists, and the game is live the moment the message
  // lands — because a game here IS the sequence of messages, not a thing a
  // service is holding on your behalf.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import EmptyState from "../EmptyState.svelte";
  import { S, flash, nameFor, sendMessage, activeChannel } from "../lib/state.svelte.js";
  import { plural } from "../lib/plural.js";
  import { GAME_LIST, gameNew, newGameId } from "../lib/games.js";

  let { onClose } = $props();

  let pick = $state(GAME_LIST[0]?.id || "");
  let opponent = $state(""); // "" = open to anybody
  let busy = $state(false);

  const me = $derived(S.identity.fingerprint);
  // Everyone in the guild except you. A DM has two members, which makes the
  // list exactly one person and the choice obvious.
  const others = $derived((S.members || []).filter((m) => m.fingerprint !== me && !m.fingerprint.startsWith("guest:")));

  async function start() {
    if (!pick || busy) return;
    if (!S.activeChannelId) return flash("Open a channel first");
    busy = true;
    try {
      await sendMessage(gameNew(pick, newGameId(), opponent), "");
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="Start a game" {onClose}>
  {#if GAME_LIST.length === 0}
    <EmptyState icon="die" headline="No games are built in" sub="A game is a rules module and a board; none are registered in this build." />
  {:else}
    <div class="field">
      <span class="lbl">Game</span>
      <div class="picks" role="radiogroup" aria-label="Game">
        {#each GAME_LIST as g (g.id)}
          <button
            type="button"
            class="pick"
            class:on={pick === g.id}
            role="radio"
            aria-checked={pick === g.id}
            onclick={() => (pick = g.id)}
          >
            <span class="pname">{g.name}</span>
            <span class="pblurb">{g.blurb}</span>
            <span class="pmeta">{plural(g.seats, "player")} · spectators welcome</span>
          </button>
        {/each}
      </div>
    </div>

    <div class="field">
      <span class="lbl">Against</span>
      <div class="opps" role="radiogroup" aria-label="Opponent">
        <button
          type="button"
          class="opp"
          class:on={opponent === ""}
          role="radio"
          aria-checked={opponent === ""}
          onclick={() => (opponent = "")}
        >
          <span class="oav" aria-hidden="true"><Icon name="members" size={15} /></span>
          <span class="oname">Anybody here</span>
          <span class="osub">The first person to take the seat plays</span>
        </button>
        {#each others as m (m.fingerprint)}
          <button
            type="button"
            class="opp"
            class:on={opponent === m.fingerprint}
            role="radio"
            aria-checked={opponent === m.fingerprint}
            onclick={() => (opponent = m.fingerprint)}
          >
            <span class="oav">
              <Avatar name={nameFor(m.fingerprint)} emoji={m.emoji} color={m.color} image={m.avatar} size={22} />
            </span>
            <span class="oname">{nameFor(m.fingerprint)}</span>
            <span class="osub">Their seat, nobody else's</span>
          </button>
        {/each}
      </div>
    </div>

    <p class="cap">
      Posts one message to {activeChannel()?.name ? `#${activeChannel().name}` : "this channel"}. Every move after it is
      another message, and every client works the board out for itself — nothing is stored anywhere.
    </p>

    <div class="actions">
      <button class="ghost" onclick={onClose}>Cancel</button>
      <button onclick={start} disabled={busy || !pick}>Start</button>
    </div>
  {/if}
</Modal>

<style>
  .field {
    display: grid;
    gap: var(--sp-1);
    margin-bottom: var(--sp-4);
  }
  .lbl {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .picks,
  .opps {
    display: grid;
    gap: var(--sp-2);
  }
  .pick {
    display: grid;
    gap: 2px;
    padding: var(--sp-3);
    text-align: left;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-2);
    color: var(--text);
    cursor: pointer;
  }
  .pick.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .pname {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .pblurb {
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .pmeta {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }

  .opps {
    max-height: calc(40 * var(--dvh));
    overflow-y: auto;
  }
  .opp {
    display: grid;
    grid-template-columns: auto 1fr;
    grid-template-rows: auto auto;
    align-items: center;
    gap: 0 var(--sp-2);
    min-height: var(--tap-min);
    padding: var(--sp-2) var(--sp-3);
    text-align: left;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-2);
    color: var(--text);
    cursor: pointer;
  }
  .opp.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .oav {
    grid-row: 1 / span 2;
    display: grid;
    place-items: center;
    width: 22px;
    color: var(--text-muted);
  }
  .oname {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .osub {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }

  .cap {
    margin: 0;
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: var(--sp-4);
  }
</style>
