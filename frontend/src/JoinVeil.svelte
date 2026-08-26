<script>
  // The threshold. Tapping Join on an event should feel like walking through
  // a door, not being teleported: this veil covers the screen for the ~500ms
  // the join takes, says WHO you are entering as and that the room is E2EE —
  // the two things a green room actually answers — then fades out over the
  // already-rendered room so you land mid-step. Driven by S.joinVeil =
  // { title, leaving? }; the card that started the join owns the lifecycle.
  import Avatar from "./Avatar.svelte";
  import { S, selfMember } from "./lib/state.svelte.js";
  import { syncLayer } from "./lib/navstack.svelte.js";

  const me = $derived(selfMember());

  // A door you are already walking through has no back. The veil sits over
  // everything for the ~500ms of the join, and back used to walk past it and
  // exit the app while the join carried on regardless.
  syncLayer("veil", () => !!S.joinVeil, () => {}, { blocking: true });

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
    <!-- A hint of the destination behind the copy. The veil only knows the
         event's TITLE — the room's guild id (and thus its banner preset)
         doesn't exist client-side until the join resolves, and matching
         S.guilds by name would happily paint some other "Weekly sync"'s
         banner. So the identity here is the accent: a soft radial wash, dim
         enough to stay under the text. -->
    <div class="wash" aria-hidden="true"></div>
    <div class="inner">
      <div class="kicker evk">{S.joinVeil.title}</div>
      <Avatar name={me.name} emoji={me.emoji} color={me.color} image={me.avatar} size={64} />
      <div class="line">Joining “{S.joinVeil.title}”…</div>
      <div class="foot">
        {#if slow}Still knocking — someone inside has to let you in…{:else}as {me.name || "you"}
          · end-to-end encrypted{/if}
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
    /* Full-cover circle at rest so .leaving has a start value to iris from.
       % is against the box diagonal — 140% clears the corners with room. */
    clip-path: circle(140% at 50% 50%);
  }
  .veil.leaving {
    pointer-events: none;
    /* The door opens: the veil irises down to a point over your avatar and
       the already-rendered room is what's left. clip-path only — stays on the
       compositor. 220ms, because EventCard tears the veil down at 260ms; a
       longer iris would get cut off mid-shrink. */
    clip-path: circle(0% at 50% 50%);
    transition: clip-path 220ms var(--ease-calm);
  }
  .wash {
    position: absolute;
    inset: 0;
    pointer-events: none;
    /* Two off-center accent pools, dimmed hard. No filter blur — radial
       gradients are already soft, and the "no backdrop-filter — phones" rule
       above extends to any per-frame filter work on a full-screen layer. */
    background:
      radial-gradient(55% 70% at 50% 26%, color-mix(in srgb, var(--accent) 32%, transparent), transparent 70%),
      radial-gradient(60% 75% at 78% 88%, color-mix(in srgb, var(--accent-hover) 22%, transparent), transparent 72%);
    opacity: 0.45;
  }
  .inner {
    position: relative; /* above the wash */
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
      clip-path: none;
    }
    /* Reduced motion keeps the pre-iris exit: no clip animation, the veil
       just stops being there (the old instant opacity drop). */
    .veil.leaving {
      clip-path: none;
      opacity: 0;
      transition: none;
    }
  }
</style>
