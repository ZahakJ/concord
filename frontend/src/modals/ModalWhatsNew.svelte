<script>
  // The one-time post-update splash: a launch, not a changelog. Concord used
  // to ship silently — v0.54's biggest features arrived unannounced and were
  // discovered by accident or not at all. App.svelte opens this ONCE per
  // version bump (it compares the build's version with the last one this
  // device ran, and a fresh install stays quiet).
  //
  // The words are the release notes themselves: src/releases/vX.Y.Z.md, the
  // same file publish-release.sh gives the GitHub release (docs/RELEASING.md),
  // bundled into this lazy chunk at build time — no network. The deck opens on
  // a frame with the version and the release in one sentence, walks one slide
  // per feature (a drawing on the stage, the notes' own bullets beside it),
  // and ends on the fine print. Security, Known limits and the rest are never
  // a slide and never trimmed: Full notes shows every section word for word in
  // the chrome's own ink.
  //
  // A stage, not a Modal.svelte dialog: it owns the screen on a phone as on a
  // desktop. It keeps the dialog contract by hand: one layer on the navigation
  // stack (Escape and Back close it), focus taken in, trapped, and given back.
  import { onDestroy, onMount, tick } from "svelte";
  import { layer } from "../lib/navstack.svelte.js";
  import { Z } from "../lib/layers.js";
  import { markOpen, markClosed } from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import ReleaseHero from "./ReleaseHero.svelte";
  import { parseNotes, chooseNotes, versionOfPath, inlineRuns, plainText } from "../lib/releasenotes.js";

  let { version = "", seen = "", onClose } = $props();

  const FILES = import.meta.glob("../releases/v*.md", { query: "?raw", import: "default", eager: true });
  const BY_VERSION = {};
  for (const [p, md] of Object.entries(FILES)) {
    const v = versionOfPath(p);
    if (v) BY_VERSION[v] = md;
  }
  // Read once: the props are the moment the splash was raised.
  // svelte-ignore state_referenced_locally
  const chosen = chooseNotes(Object.keys(BY_VERSION), version, seen);
  const notes = chosen ? parseNotes(BY_VERSION[chosen], chosen) : null;
  // The hero names the minor release ("0.62"); a patch with notes of its own
  // keeps its full number. The kicker always says exactly which notes these are.
  const short = chosen.endsWith(".0") ? chosen.slice(0, -2) : chosen;

  const slides = notes
    ? [
        { kind: "open", title: `Concord ${short}` },
        ...notes.features.map((f, i) => ({ kind: "feature", title: plainText(f.title), f, n: i + 1 })),
        ...(notes.fine.length ? [{ kind: "fine", title: "The fine print" }] : []),
      ]
    : [];
  const limits = notes?.fine.find((s) => s.fine === "limits") || null;
  const otherFine = notes?.fine.filter((s) => s !== limits) || [];
  const secId = (i) => `wn-sec-${i}`;

  let index = $state(0);
  let dir = $state(1);
  let view = $state("deck"); // "deck" | "notes"
  const last = $derived(index === slides.length - 1);
  const slide = $derived(slides[index]);

  let dialog = $state(null);
  let nextBtn = $state(null);
  let notesScroller = $state(null);
  let opener = null;

  const me = notes ? layer("modal", () => onClose()) : null;
  if (notes) markOpen();

  onMount(() => {
    if (!notes) {
      onClose();
      return;
    }
    const a = document.activeElement;
    opener = a instanceof HTMLElement && a !== document.body ? a : null;
    tick().then(() => nextBtn?.focus({ preventScroll: true }));
  });
  onDestroy(() => {
    if (!notes) return;
    queueMicrotask(markClosed);
    const back = opener;
    opener = null;
    if (!back) return;
    tick().then(() => {
      const now = document.activeElement;
      if (now && now !== document.body && now !== document.documentElement) return;
      if (document.contains(back)) back.focus?.({ preventScroll: true });
    });
  });

  function go(to) {
    const i = Math.max(0, Math.min(slides.length - 1, to));
    if (i === index) return;
    dir = i > index ? 1 : -1;
    index = i;
  }
  function next() {
    if (last) onClose();
    else go(index + 1);
  }
  async function openNotes(at = -1) {
    view = "notes";
    await tick();
    const el = at >= 0 ? notesScroller?.querySelector("#" + secId(at)) : null;
    if (notesScroller) notesScroller.scrollTop = el ? el.offsetTop - 8 : 0;
    notesScroller?.focus({ preventScroll: true });
  }
  async function closeNotes() {
    view = "deck";
    await tick();
    nextBtn?.focus({ preventScroll: true });
  }

  const FOCUSABLE = 'a[href],button:not([disabled]),[tabindex]:not([tabindex="-1"])';
  function onKeydown(e) {
    if (!notes || !me?.isTop) return;
    if (e.key === "Tab" && dialog) {
      const f = [...dialog.querySelectorAll(FOCUSABLE)].filter((el) => el.getClientRects().length);
      if (!f.length) return;
      const first = f[0];
      const lastEl = f[f.length - 1];
      const at = document.activeElement;
      if (!f.includes(at)) {
        e.preventDefault();
        (e.shiftKey ? lastEl : first).focus();
      } else if (e.shiftKey && at === first) {
        e.preventDefault();
        lastEl.focus();
      } else if (!e.shiftKey && at === lastEl) {
        e.preventDefault();
        first.focus();
      }
      return;
    }
    if (view !== "deck" || e.altKey || e.ctrlKey || e.metaKey) return;
    // "Next" is the reading direction's forward: leftward in Arabic.
    const rtl = getComputedStyle(dialog || document.documentElement).direction === "rtl";
    const fwd = rtl ? "ArrowLeft" : "ArrowRight";
    const bwd = rtl ? "ArrowRight" : "ArrowLeft";
    if (e.key === fwd) go(index + 1);
    else if (e.key === bwd) go(index - 1);
    else if (e.key === "Home") go(0);
    else if (e.key === "End") go(slides.length - 1);
    else return;
    e.preventDefault();
  }

  // A swipe turns the page, toward the reading direction's start = forward.
  let touch = null;
  function onTouchStart(e) {
    const t = e.touches[0];
    touch = t ? { x: t.clientX, y: t.clientY } : null;
  }
  function onTouchEnd(e) {
    const from = touch;
    touch = null;
    const t = e.changedTouches[0];
    if (!from || !t || view !== "deck") return;
    const dx = t.clientX - from.x;
    const dy = t.clientY - from.y;
    if (Math.abs(dx) < 48 || Math.abs(dx) < Math.abs(dy) * 1.3) return;
    const rtl = getComputedStyle(dialog || document.documentElement).direction === "rtl";
    const forward = (rtl ? 1 : -1) * Math.sign(dx) > 0;
    go(index + (forward ? 1 : -1));
  }

  const announce = $derived(slide ? `${index + 1} of ${slides.length}: ${slide.title}` : "");
  const progress = $derived(slides.length > 1 ? (index + 1) / slides.length : 1);
</script>

<svelte:window onkeydown={onKeydown} />

{#snippet runs(text)}{#each inlineRuns(text) as r, i (i)}{#if r.t === "strong"}<strong>{r.v}</strong>{:else if r.t === "em"}<em>{r.v}</em>{:else if r.t === "code"}<code>{r.v}</code>{:else}{r.v}{/if}{/each}{/snippet}

{#snippet items(list)}
  {#each list as it, i (i)}
    {#if it.kind === "li"}
      <li>{@render runs(it.text)}</li>
    {:else}
      <p>{@render runs(it.text)}</p>
    {/if}
  {/each}
{/snippet}

{#if notes}
  <div
    class="wn-overlay"
    style:z-index={Z.layer + (me?.index ?? 0) * 2}
    onclick={onClose}
    role="presentation"
    data-modal-overlay
  >
    <div
      bind:this={dialog}
      class="wn"
      class:notes={view === "notes"}
      role="dialog"
      aria-modal="true"
      aria-label="What's new in Concord {chosen}"
      tabindex="-1"
      onclick={(e) => e.stopPropagation()}
      onkeydown={() => {}}
    >
      <!-- The light: one layer, painted once, drifting on the compositor. The
           veil over it keeps every line of body text on a calm ground. -->
      <div class="aurora" aria-hidden="true"></div>
      <div class="veil" aria-hidden="true"></div>
      <div class="progress" aria-hidden="true" style:transform="scaleX({view === 'notes' ? 1 : progress})"></div>

      <header class="top">
        <span class="kicker">What's new<span class="sep" aria-hidden="true">·</span><span class="ver">{chosen}</span></span>
        <span class="topacts">
          {#if view === "deck"}
            <button class="textbtn" onclick={() => openNotes()}>Full notes</button>
          {/if}
          <button class="iconbtn x" onclick={onClose} aria-label="Close" title="Close · Esc">
            <Icon name="close" size={18} />
          </button>
        </span>
      </header>

      {#if view === "deck"}
        <div class="body" ontouchstart={onTouchStart} ontouchend={onTouchEnd} role="presentation">
          {#key index}
            <section
              class="slide {slide.kind}"
              data-dir={dir}
              role="group"
              aria-roledescription="slide"
              aria-label="{index + 1} of {slides.length}: {slide.title}"
            >
              {#if slide.kind === "open"}
                <div class="emblem rv" style="--i: 0"><ReleaseHero kind="emblem" /></div>
                <h2 class="hero rv" style="--i: 1">
                  Concord <span class="glow">{short}</span>
                </h2>
                {#if notes.tagline}<p class="summary rv" style="--i: 2">{@render runs(notes.tagline)}</p>{/if}
                <ol class="toc rv" style="--i: 3" aria-label="In this release">
                  {#each notes.features as f, i (i)}
                    <li>
                      <button class="chip" onclick={() => go(i + 1)}>
                        <span class="num" aria-hidden="true">{String(i + 1).padStart(2, "0")}</span>
                        <span class="cname">{plainText(f.headline)}</span>
                      </button>
                    </li>
                  {/each}
                </ol>
              {:else if slide.kind === "feature"}
                <!-- One idea per slide: the drawing, a headline, one
                     sentence. The bullets are in the full notes, a tap away. -->
                <div class="art" aria-hidden="true">
                  <div class="pool"></div>
                  <div class="float"><ReleaseHero kind={slide.f.hero} /></div>
                </div>
                <div class="copy">
                  <h2 class="headline rv" style="--i: 1">{@render runs(slide.f.headline)}</h2>
                  {#if slide.f.tagline}
                    <p class="tagline rv" style="--i: 2">{@render runs(slide.f.tagline)}</p>
                  {/if}
                  <button class="more rv" style="--i: 3" onclick={() => openNotes(notes.sections.indexOf(slide.f))}>
                    The details<Icon name="chevron" size={16} />
                  </button>
                </div>
              {:else}
                <div class="finehead">
                  <p class="fkicker rv" style="--i: 0">Also in {chosen}</p>
                  <h2 class="headline rv" style="--i: 1">The fine print</h2>
                  <p class="finelede rv" style="--i: 2">
                    What this release fixes quietly, what it does not do yet, and what to know before going back. Every word is in the full notes.
                  </p>
                </div>
                <div class="finebody">
                  {#if limits}
                    <!-- Known limits keep the chrome's ink, weight and size:
                         never softened into stage type. -->
                    <section class="limits rv" style="--i: 3" aria-labelledby="wn-limits-h">
                      <h3 id="wn-limits-h">{@render runs(limits.title)}</h3>
                      <ul>{@render items(limits.items)}</ul>
                    </section>
                  {/if}
                  <ul class="rows rv" style="--i: 4">
                    {#each otherFine as s (s.title)}
                      <li>
                        <button class="row" onclick={() => openNotes(notes.sections.indexOf(s))}>
                          <span class="rtitle">{plainText(s.title)}</span>
                          <span class="rcount">{s.items.length} {s.items.length === 1 ? "item" : "items"}</span>
                          <Icon name="chevron" size={16} />
                        </button>
                      </li>
                    {/each}
                  </ul>
                </div>
              {/if}
            </section>
          {/key}
        </div>
      {:else}
        <!-- The whole of the notes, as published. Chrome ink on an opaque
             surface: this is a document to read, not a stage. -->
        <div class="fullnotes" bind:this={notesScroller} tabindex="-1" aria-label="Release notes for {chosen}" role="region">
          <h2 class="fntitle">Concord {chosen}</h2>
          {#each notes.lead as p, i (i)}
            <p class="fnlead">{@render runs(p)}</p>
          {/each}
          {#each notes.sections as s, i (i)}
            <section id={secId(i)} class="fnsec">
              <h3>{@render runs(s.title)}</h3>
              {#if s.items.some((x) => x.kind === "li")}
                <ul>{@render items(s.items)}</ul>
              {:else}
                {@render items(s.items)}
              {/if}
            </section>
          {/each}
        </div>
      {/if}

      <footer class="foot">
        {#if view === "deck"}
          <button class="navbtn back" class:hidden={index === 0} onclick={() => go(index - 1)} disabled={index === 0} aria-label="Previous" title="Previous · ←">
            <Icon name="chevron" size={18} />
          </button>
          <div class="dots" role="group" aria-label="Slides">
            {#each slides as s, i (i)}
              <button
                class="dot"
                class:on={i === index}
                class:done={i < index}
                aria-label="{i + 1} of {slides.length}: {s.title}"
                aria-current={i === index ? "step" : undefined}
                onclick={() => go(i)}
              ><span></span></button>
            {/each}
          </div>
        {:else}
          <button class="navbtn textual" onclick={closeNotes}>
            <span class="flip"><Icon name="chevron" size={16} /></span>Highlights
          </button>
          <span class="spacer"></span>
        {/if}
        <button class="primary go" class:done={last || view === "notes"} bind:this={nextBtn} onclick={view === "notes" ? onClose : next}>
          {#if view === "notes" || last}Done{:else if index === 0}See what's new{:else}Next{/if}
          {#if view === "deck" && !last}<Icon name="chevron" size={16} />{/if}
        </button>
      </footer>
      <p class="sr-only" aria-live="polite">{view === "deck" ? announce : "Full notes"}</p>
    </div>
  </div>
{/if}

<style>
  /* ---- the frame ---------------------------------------------------------- */
  .wn-overlay {
    position: fixed;
    inset: 0;
    display: grid;
    place-items: center;
    background: var(--scrim);
    animation: wn-fade var(--dur-stage) ease-out both;
  }
  .wn {
    position: relative;
    isolation: isolate;
    width: min(1040px, calc(100vw - var(--sp-7)));
    /* A fixed size, not a maximum: a deck that resized on every slide moved
       Next out from under the pointer. The copy scrolls when it is longer. */
    height: min(660px, calc(100dvh - var(--sp-7)));
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto;
    background: var(--stage-ground);
    color: var(--stage-ink);
    border-radius: var(--radius-xl);
    border: 1px solid color-mix(in srgb, var(--stage-ink) 10%, transparent);
    box-shadow: var(--shadow-stage);
    overflow: hidden;
    outline: none;
    animation: wn-arrive var(--dur-stage) var(--ease-spring) both;
  }
  @keyframes wn-fade {
    from {
      opacity: 0;
    }
  }
  @keyframes wn-arrive {
    from {
      opacity: 0;
      transform: translateY(var(--stage-rise)) scale(var(--stage-scale));
    }
  }

  /* The aurora drifts — one composited layer, transformed, never re-painted.
     Bigger than the frame so the drift never shows an edge. */
  .aurora {
    position: absolute;
    inset: -18%;
    z-index: -2;
    background-image: var(--aurora);
    will-change: transform;
    animation: wn-drift 26s ease-in-out infinite alternate;
    pointer-events: none;
  }
  @keyframes wn-drift {
    from {
      transform: translate3d(-4%, -2%, 0) scale(1);
    }
    to {
      transform: translate3d(4%, 3%, 0) scale(1.08);
    }
  }
  /* The calm in the middle where the words are. */
  .veil {
    position: absolute;
    inset: 0;
    z-index: -1;
    background: radial-gradient(ellipse 70% 62% at 50% 52%, var(--stage-veil) 35%, transparent 100%);
    pointer-events: none;
  }
  .wn.notes .veil {
    background: var(--stage-veil);
  }
  .progress {
    position: absolute;
    inset: 0 0 auto;
    height: 3px;
    background: var(--aurora-ink);
    transform-origin: left center;
    transition: transform var(--dur-stage) var(--ease-spring);
    pointer-events: none;
  }
  :global([dir="rtl"]) .progress {
    transform-origin: right center;
  }

  /* ---- top and foot --------------------------------------------------------- */
  .top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-4) var(--sp-4) 0 var(--sp-6);
  }
  .kicker {
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--accent-hover);
    font-variant-numeric: tabular-nums;
  }
  .sep {
    margin-inline: var(--sp-2);
    color: var(--stage-ink-muted);
  }
  .ver {
    color: var(--stage-ink-muted);
    font-weight: 500;
  }
  .topacts {
    margin-inline-start: auto;
    display: flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .textbtn {
    padding: 6px 12px;
    border-radius: 999px;
    font-size: var(--fs-ui);
    font-weight: 500;
    color: var(--stage-ink-muted);
  }
  .textbtn:hover {
    color: var(--stage-ink);
    background: color-mix(in srgb, var(--stage-ink) 8%, transparent);
  }
  .iconbtn {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    padding: 0;
    border-radius: 999px;
    color: var(--stage-ink-muted);
  }
  .iconbtn:hover {
    color: var(--stage-ink);
    background: color-mix(in srgb, var(--stage-ink) 8%, transparent);
  }

  /* Three columns, the outer two equal, so the dots sit on the sheet's
     centre line whatever the two buttons are called. */
  .foot {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-5) var(--sp-5);
  }
  .foot > .navbtn {
    justify-self: start;
  }
  .foot > .go {
    justify-self: end;
  }
  .navbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--sp-1);
    min-width: 44px;
    height: 44px;
    padding: 0 var(--sp-3);
    border-radius: 999px;
    color: var(--stage-ink);
    border: 1px solid color-mix(in srgb, var(--stage-ink-muted) 60%, transparent);
  }
  .navbtn.back :global(svg),
  .flip :global(svg) {
    transform: scaleX(-1);
  }
  .flip {
    display: inline-grid;
  }
  /* Right-to-left reading: forward points left. Every chevron in the deck
     turns, the ones drawn turned (Back, Highlights) turn back. Keyed on the
     dir attribute, as the progress bar is: the build lowers :dir() to a
     list of right-to-left languages, which a dir attribute does not match. */
  :global([dir="rtl"]) .go :global(svg),
  :global([dir="rtl"]) .more :global(svg),
  :global([dir="rtl"]) .row :global(svg) {
    transform: scaleX(-1);
  }
  :global([dir="rtl"]) .navbtn.back :global(svg),
  :global([dir="rtl"]) .flip :global(svg) {
    transform: none;
  }
  .navbtn:hover:not(:disabled) {
    background: color-mix(in srgb, var(--stage-ink) 8%, transparent);
  }
  .navbtn.hidden {
    visibility: hidden;
  }
  .navbtn.textual {
    padding: 0 var(--sp-4) 0 var(--sp-3);
    font-weight: 500;
  }
  .spacer {
    flex: 1;
  }
  .dots {
    display: flex;
    justify-content: center;
    gap: var(--sp-1);
  }
  .dot {
    display: grid;
    place-items: center;
    width: 26px;
    height: 32px;
    padding: 0;
  }
  .dot span {
    width: 8px;
    height: 8px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--stage-ink-muted) 55%, transparent);
    transition:
      transform var(--dur-stage) var(--ease-spring),
      opacity var(--dur-stage) ease;
  }
  .dot.done span {
    background: var(--aurora-1);
    opacity: 0.6;
  }
  .dot.on span {
    background: var(--aurora-ink);
    transform: scaleX(2.6);
  }
  .go {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    height: 44px;
    padding: 0 var(--sp-5) 0 var(--sp-6);
    border-radius: 999px;
    font-size: var(--fs-body);
    white-space: nowrap;
    flex: none;
    box-shadow: 0 6px 24px color-mix(in srgb, var(--aurora-1) 32%, transparent);
  }
  .go.done {
    padding: 0 var(--sp-6);
  }
  .go:hover {
    filter: brightness(1.06);
  }

  /* ---- slides -------------------------------------------------------------- */
  .body {
    position: relative;
    min-height: 0;
    display: grid;
    touch-action: pan-y;
  }
  .slide {
    min-height: 0;
    animation: wn-next var(--dur-stage) var(--ease-spring) both;
  }
  .slide[data-dir="-1"] {
    animation-name: wn-prev;
  }
  @keyframes wn-next {
    from {
      opacity: 0;
      transform: translateX(calc(var(--stage-rise) * 1.6));
    }
  }
  @keyframes wn-prev {
    from {
      opacity: 0;
      transform: translateX(calc(var(--stage-rise) * -1.6));
    }
  }
  /* The staggered reveal: each part rises into place in reading order. The
     resting state is the final frame (app.css zeroes durations and delays
     under reduced motion, so nothing waits invisible). */
  .rv {
    animation: wn-rise var(--dur-reveal) var(--ease-spring) both;
    animation-delay: calc(80ms + var(--i, 0) * var(--stagger) * 1.5);
  }
  @keyframes wn-rise {
    from {
      opacity: 0;
      transform: translateY(var(--stage-rise));
    }
  }

  /* The opening frame: the version, in the light, and the release in one
     sentence. */
  .slide.open {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 0 var(--sp-7) var(--sp-3);
    overflow-y: auto;
  }
  .emblem {
    width: 168px;
    height: 168px;
    margin-bottom: var(--sp-4);
  }
  .emblem.rv {
    animation-name: wn-emblem;
    animation-duration: calc(var(--dur-reveal) * 1.6);
  }
  @keyframes wn-emblem {
    from {
      opacity: 0;
      transform: scale(0.7) rotate(-20deg);
    }
  }
  .hero {
    margin: 0;
    font-family: var(--display-font);
    font-size: var(--fs-hero);
    font-weight: 700;
    line-height: 1.05;
    letter-spacing: -0.02em;
    color: var(--stage-ink);
  }
  .glow {
    background: var(--aurora-ink);
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }
  .summary {
    margin: var(--sp-4) 0 0;
    max-width: 60ch;
    font-size: var(--fs-title);
    line-height: 1.55;
    color: var(--stage-ink-muted);
    text-wrap: pretty;
  }
  .toc {
    list-style: none;
    margin: var(--sp-6) 0 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--sp-2);
    max-width: 820px;
  }
  .chip {
    display: inline-flex;
    align-items: baseline;
    gap: var(--sp-2);
    padding: 7px 14px 7px 12px;
    border-radius: 999px;
    font-size: var(--fs-ui);
    color: var(--stage-ink);
    background: color-mix(in srgb, var(--stage-ground) 55%, transparent);
    border: 1px solid color-mix(in srgb, var(--stage-ink-muted) 45%, transparent);
    transition:
      background var(--dur-quick) ease,
      transform var(--dur-quick) var(--ease-out);
  }
  .chip:hover {
    background: color-mix(in srgb, var(--aurora-1) 16%, var(--stage-ground));
    transform: translateY(-1px);
  }
  .num {
    font-family: var(--display-font);
    font-size: var(--fs-small);
    font-weight: 700;
    color: var(--accent-hover);
    font-variant-numeric: tabular-nums;
  }

  /* A feature: the drawing is the hero. It is drawn larger than its column
     and runs off the frame's edge; the words sit on the calm side. */
  .slide.feature {
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1.25fr) minmax(0, 1fr);
    align-items: center;
    gap: var(--sp-5);
    padding: 0 var(--sp-7) 0 0;
  }
  .art {
    position: relative;
    align-self: stretch;
    min-height: 0;
    display: grid;
    place-items: center;
    /* The bleed: wider and taller than the column, clipped by the frame. */
    margin: calc(var(--sp-7) * -1) 0 calc(var(--sp-6) * -1) calc(var(--sp-7) * -1);
    animation: wn-card calc(var(--dur-reveal) * 1.2) var(--ease-spring) both;
  }
  @keyframes wn-card {
    from {
      opacity: 0;
      transform: scale(var(--stage-scale));
    }
  }
  .pool {
    position: absolute;
    inset: 4% 0 0 8%;
    border-radius: 50%;
    background:
      radial-gradient(closest-side, color-mix(in srgb, var(--aurora-2) 34%, transparent), transparent),
      radial-gradient(closest-side at 30% 30%, color-mix(in srgb, var(--aurora-1) 26%, transparent), transparent);
    animation: wn-breathe 7s ease-in-out infinite alternate;
    pointer-events: none;
  }
  /* Ambient motion, transform and opacity only. The global stand-down zeroes
     the duration under reduced motion, so the drawing simply rests. */
  .float {
    position: relative;
    width: 118%;
    max-width: 640px;
    display: grid;
    place-items: center;
    animation: wn-float 9s ease-in-out infinite alternate;
  }
  .float :global(svg) {
    width: 100%;
    height: auto;
    max-height: 100%;
  }
  @keyframes wn-float {
    from {
      transform: translate3d(0, -1.5%, 0) rotate(-1deg);
    }
    to {
      transform: translate3d(0, 1.5%, 0) rotate(1deg);
    }
  }
  @keyframes wn-breathe {
    from {
      opacity: 0.7;
      transform: scale(0.94);
    }
    to {
      opacity: 1;
      transform: scale(1.04);
    }
  }
  .copy {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--sp-4);
  }
  .fkicker {
    margin: 0 0 var(--sp-2);
    font-family: var(--display-font);
    font-size: var(--fs-small);
    font-weight: 700;
    color: var(--accent-hover);
    font-variant-numeric: tabular-nums;
  }
  .headline {
    margin: 0 0 var(--sp-4);
    font-family: var(--display-font);
    font-size: var(--fs-headline);
    font-weight: 700;
    line-height: 1.12;
    letter-spacing: -0.02em;
    color: var(--stage-ink);
    text-wrap: balance;
  }
  .copy .headline {
    margin: 0;
    font-size: var(--fs-hero);
    line-height: 1.04;
    letter-spacing: -0.03em;
  }
  .tagline {
    margin: 0;
    max-width: 30ch;
    font-size: var(--fs-body);
    line-height: 1.55;
    color: var(--stage-ink-muted);
    text-wrap: pretty;
  }
  .tagline :global(strong) {
    color: var(--stage-ink);
    font-weight: 600;
  }
  .more {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    margin-top: var(--sp-2);
    padding: var(--sp-2) var(--sp-3);
    margin-inline-start: calc(var(--sp-3) * -1);
    border: none;
    border-radius: 999px;
    background: none;
    color: var(--accent-hover);
    font: inherit;
    font-size: var(--fs-ui);
    font-weight: 600;
    cursor: pointer;
  }
  .more:hover {
    background: color-mix(in srgb, var(--stage-ink) 8%, transparent);
  }
  .fullnotes :global(code),
  .limits :global(code) {
    font-family: var(--mono-font, ui-monospace, monospace);
    font-size: 0.92em;
  }

  /* The fine print: Known limits shown whole, the rest one tap away. */
  .slide.fine {
    display: grid;
    grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
    gap: var(--sp-6);
    padding: var(--sp-6) var(--sp-6) 0;
    min-height: 0;
  }
  .finehead {
    align-self: center;
  }
  .finelede {
    margin: 0;
    font-size: var(--fs-body);
    line-height: 1.55;
    color: var(--stage-ink-muted);
    max-width: 42ch;
  }
  .finebody {
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding-bottom: var(--sp-3);
  }
  .limits {
    padding: var(--sp-4) var(--sp-5);
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    color: var(--text);
    border: 1px solid var(--border);
  }
  .limits h3 {
    margin: 0 0 var(--sp-2);
    font-size: var(--fs-ui);
    font-weight: 600;
    color: var(--text);
  }
  .limits ul {
    margin: 0;
    padding-inline-start: 1.1em;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .limits li,
  .limits p {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.55;
    color: var(--text);
  }
  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .rows li + li {
    border-top: 1px solid var(--border);
  }
  .row {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-4);
    border-radius: 0;
    color: var(--text);
    text-align: start;
    min-height: 48px;
  }
  .row:hover {
    background: var(--bg-3);
  }
  .rtitle {
    flex: 1;
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .rcount {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .row :global(svg) {
    color: var(--text-muted);
  }

  /* ---- full notes: a document, in the chrome's ink ------------------------ */
  .fullnotes {
    min-height: 0;
    overflow-y: auto;
    margin: var(--sp-3) var(--sp-5) 0;
    padding: var(--sp-5) var(--sp-6) var(--sp-6);
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    color: var(--text);
    border: 1px solid var(--border);
    outline: none;
    animation: wn-rise var(--dur-stage) var(--ease-spring) both;
  }
  .fullnotes:focus-visible {
    outline: var(--focus-ring);
  }
  .fntitle {
    margin: 0 0 var(--sp-3);
    font-family: var(--display-font);
    font-size: var(--fs-display);
    font-weight: 700;
    color: var(--text);
  }
  .fnlead {
    margin: 0 0 var(--sp-3);
    font-size: var(--fs-body);
    line-height: 1.6;
    color: var(--text);
    max-width: 72ch;
  }
  .fnsec {
    margin-top: var(--sp-5);
    max-width: 72ch;
  }
  .fnsec h3 {
    margin: 0 0 var(--sp-2);
    font-size: var(--fs-title);
    font-weight: 600;
    color: var(--text);
  }
  .fnsec ul {
    margin: 0;
    padding-inline-start: 1.2em;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .fnsec li,
  .fnsec p {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.6;
    color: var(--text);
  }

  /* ---- phone: the stage owns the screen ------------------------------------ */
  @media (max-width: 768px) {
    .wn-overlay {
      place-items: stretch;
    }
    .wn {
      width: 100vw;
      height: 100dvh;
      border-radius: 0;
      border: none;
      padding-top: var(--safe-top);
      padding-bottom: var(--safe-bottom);
    }
    .top {
      padding: var(--sp-2) var(--sp-2) 0 var(--sp-5);
    }
    /* The dots on a line of their own, centred on the screen, with Back
       and Next under them: in one row at 360px the button, six dots and
       Back were wider than the screen, and the whole sheet slid sideways
       when Next took focus. */
    .foot {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      grid-template-areas:
        "dots dots"
        "back go";
      align-items: center;
      padding: var(--sp-2) var(--sp-4) var(--sp-4);
      gap: var(--sp-2) var(--sp-3);
    }
    .dots {
      grid-area: dots;
    }
    .navbtn.back,
    .navbtn.textual {
      grid-area: back;
    }
    .spacer {
      display: none;
    }
    .go {
      grid-area: go;
      justify-self: end;
    }
    .dot {
      width: 20px;
    }
    .go {
      padding: 0 var(--sp-4) 0 var(--sp-5);
    }
    .slide.open {
      justify-content: safe center;
      padding: var(--sp-4) var(--sp-5) var(--sp-3);
    }
    .emblem {
      width: 148px;
      height: 148px;
      flex: none;
    }
    .dots {
      gap: 0;
    }
    .summary {
      font-size: var(--fs-body);
    }
    .toc {
      margin-top: var(--sp-5);
    }
    .chip {
      font-size: var(--fs-compact);
    }
    .slide.feature {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: minmax(0, 1fr) auto;
      align-items: end;
      gap: var(--sp-2);
      padding: 0 var(--sp-5) var(--sp-3);
    }
    .art {
      margin: calc(var(--sp-6) * -1) calc(var(--sp-5) * -1) 0;
    }
    .float {
      width: 112%;
    }
    .copy {
      gap: var(--sp-3);
    }
    .slide.fine {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto minmax(0, 1fr);
      gap: var(--sp-4);
      padding: var(--sp-4) var(--sp-4) 0;
    }
    .fullnotes {
      margin: var(--sp-2) var(--sp-3) 0;
      padding: var(--sp-4);
    }
  }
</style>
