<script>
  // The threshold. Tapping Join on an event should feel like walking through
  // a door, not being teleported: this veil covers the screen for the ~500ms
  // the join takes, says WHO you are entering as and that the room is E2EE —
  // the two things a green room actually answers — then fades out over the
  // already-rendered room so you land mid-step. Driven by S.joinVeil =
  // { title, leaving? }; the card that started the join owns the lifecycle.
  import Avatar from "./Avatar.svelte";
  import { S, selfMember } from "./lib/state.svelte.js";

  const me = $derived(selfMember());

  // Past ~1.5s the honest line changes to the door language the guest page
  // already speaks — a slow P2P join is a knock, and saying so beats a mute
  // spinner.
  let slow = $state(false);
  $effect(() => {
    if (!S.joinVeil || S.joinVeil.leaving) return;
    const t = setTimeout(() => (slow = true), 1500);
    return () => {
      clearTimeout(t);
      slow = false;
    };
  });
</script>

{#if S.joinVeil}
  <div class="veil" class:leaving={S.joinVeil.leaving} role="status" aria-live="polite">
    <div class="inner">
      <div class="kicker evk">{S.joinVeil.title}</div>
      <Avatar name={me.name} emoji={me.emoji} color={me.color} image={me.avatar} size={64} />
      <div class="line">Joining “{S.joinVeil.title}”…</div>
      <div class="foot">
        {#if slow}Still knocking…{:else}as {me.name || "you"} · end-to-end encrypted{/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .veil {
    position: fixed;
    inset: 0;
    z-index: 210; /* above modals (100) and the context menu — the door closes over everything */
    display: grid;
    place-items: center;
    /* Translucent over --bg-0: the room renders BEHIND the veil, so the exit
       fade is a reveal, not a scene cut. No backdrop-filter — phones. */
    background: color-mix(in srgb, var(--bg-0) 82%, transparent);
    animation: veil-in 140ms var(--ease-calm);
  }
  .veil.leaving {
    opacity: 0;
    pointer-events: none;
    transition: opacity 220ms var(--ease-calm);
  }
  .inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-3);
    text-align: center;
    padding: var(--sp-5);
    max-width: min(420px, 90vw);
  }
  .evk {
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }
  .line {
    font-size: var(--fs-title);
    font-weight: 650;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }
  .foot {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
  @keyframes veil-in {
    from {
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .veil {
      animation: none;
    }
    .veil.leaving {
      transition: none;
    }
  }
</style>
