<script>
  // Fullscreen story playback. Not a Modal: it's an overlay like the studios,
  // so it registers with the overlay-closer stack (BannerStudio.svelte does
  // the same) and the phone's back button dismisses it before anything else.
  //
  // The banner IS the story — Banner.svelte paints the preset full-bleed and
  // brings its own reduced-motion handling with it (drift and fx layers both
  // stand down), so this component adds no animation of its own beyond the
  // progress bars, whose movement is information, not decoration.
  import Icon from "./Icon.svelte";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import Avatar from "./Avatar.svelte";
  import Banner from "./Banner.svelte";
  import { S, memberByFpr, startDM, flash } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { sealAgo } from "./lib/timestamp.js";

  let { stories = [], start = 0, onClose } = $props();

  const DURATION = 5000; // ms per story

  // Initial-value capture is the point: the viewer mounts fresh per open and
  // owns its position from there — later prop churn must not yank playback.
  // svelte-ignore state_referenced_locally
  let index = $state(Math.max(0, Math.min(start, stories.length - 1)));
  let progress = $state(0); // 0..1 through the current story
  let paused = $state(false);

  const story = $derived(stories[index]);
  const mem = $derived(story ? memberByFpr(story.author) : null);
  const isSelf = $derived(story?.author === S.identity.fingerprint);

  $effect(() => pushLayer("story", () => onClose?.()));

  // Seen is recorded when you move PAST a story (auto-advance or tap-forward)
  // — rewinding and bailing out mid-story leave it unread, and the call is
  // local-only (no view receipts; see MarkStorySeen in the bridge).
  let changed = false;
  function markSeen(s) {
    if (!s || s.seen) return;
    s.seen = true;
    changed = true;
    api.markStorySeen(s.id).catch(() => {});
  }

  // One local echo on the way out, however the viewer closes (X, Esc, back
  // button, last story ending): the tray re-fetches and the rings grey out.
  // markStorySeen never crosses the network, so no backend event covers this.
  $effect(() => {
    return () => {
      if (changed) window.dispatchEvent(new CustomEvent("concord:stories-changed"));
    };
  });

  // The clock: one rAF loop, reading `paused` per frame rather than
  // re-arming the effect on every pause flip. An armed delete-confirm also
  // holds the clock — the story must not advance out from under the decision.
  $effect(() => {
    let raf;
    let last = performance.now();
    const tick = (now) => {
      const dt = now - last;
      last = now;
      if (!paused && !confirmDelete && dt < 1000) {
        // A background tab freezes rAF; a resumed one hands us the whole gap
        // in a single dt — skip it rather than blowing through stories.
        progress += dt / DURATION;
        if (progress >= 1) advance();
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  });

  function advance() {
    markSeen(stories[index]);
    if (index >= stories.length - 1) {
      onClose?.();
      return;
    }
    index++;
    progress = 0;
  }

  // Deleting your own story. The viewer sits at z-index 400, ABOVE the modal
  // tier, so a confirm raised through S.modal would open underneath it —
  // instead the trash button itself is the confirm: first tap arms it red
  // ("Delete?"), second tap retracts. Any navigation disarms.
  let confirmDelete = $state(false);
  let deleting = false;
  $effect(() => {
    story?.id; // moving to another story is a "never mind"
    confirmDelete = false;
  });
  async function deleteCurrent() {
    if (!confirmDelete) {
      confirmDelete = true;
      return;
    }
    const s = story;
    if (!s || deleting) return;
    deleting = true;
    try {
      await api.deleteStory(s.id);
      // The tray re-fetches on this echo (deletion DOES cross the network,
      // but the backend event repaints other peers — this one repaints us).
      changed = false; // seen-echo on close is redundant now
      window.dispatchEvent(new CustomEvent("concord:stories-changed"));
      // Slide past the gap: the splice moves the next story into this index.
      stories.splice(index, 1);
      if (stories.length === 0 || index >= stories.length) {
        onClose?.();
        return;
      }
      progress = 0;
      confirmDelete = false;
    } catch (err) {
      flash(err);
      confirmDelete = false;
    } finally {
      deleting = false;
    }
  }

  function rewind() {
    // Mid-story, a back-tap restarts it (the video-player convention); only
    // from the very start does it step to the previous story.
    if (progress > 0.25 || index === 0) {
      progress = 0;
      return;
    }
    index--;
    progress = 0;
  }

  // ---- input: keys, taps, hold-to-pause ----
  function onKeydown(e) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onClose?.();
    } else if (e.key === "ArrowRight") {
      e.preventDefault();
      advance();
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      rewind();
    }
  }

  // A press that lasts is a pause (touch has no hover to hide behind); a
  // quick release is a tap — left third rewinds, the rest advances. The pause
  // arms on a short delay so a crisp tap never visibly stutters the bar.
  let downAt = 0;
  let pauseTimer = null;
  function onDown(e) {
    downAt = performance.now();
    clearTimeout(pauseTimer);
    pauseTimer = setTimeout(() => (paused = true), 180);
    e.currentTarget.setPointerCapture?.(e.pointerId);
  }
  function onUp(e) {
    clearTimeout(pauseTimer);
    const held = performance.now() - downAt > 250;
    paused = false;
    if (held) return; // that was a hold-to-pause, not a navigation tap
    const x = e.clientX / window.innerWidth;
    if (x < 0.33) rewind();
    else advance();
  }
  function onCancel() {
    clearTimeout(pauseTimer);
    paused = false;
  }

  // "Reply" drops into the author's DM with the composer prefilled — same
  // textarea trick as the share-sheet intake (App.svelte insertShare): the
  // composer owns its draft privately, so we reach it the way a paste does,
  // through the input event that feeds bind:value, autosize AND draft save.
  // S.replyingTo would be wrong here: a story is not a message to thread on.
  function reply() {
    const s = story;
    if (!s) return;
    const text = s.caption ? `re your moment: ${s.caption.slice(0, 40)}` : "re your moment";
    onClose?.();
    startDM(s.author)
      .then(() => insertDraft(text, 0))
      .catch(flash);
  }
  function insertDraft(text, tries) {
    const el = document.querySelector("textarea.draft");
    if (!el) {
      // The composer mounts a beat after the DM opens; try briefly.
      if (tries < 12) setTimeout(() => insertDraft(text, tries + 1), 250);
      return;
    }
    el.value = el.value ? `${el.value.replace(/\s+$/, "")}\n${text}` : text;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.focus();
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if story}
  <div class="viewer" role="dialog" aria-label="Moment by {story.authorName || story.author.slice(0, 9)}">
    <!-- The banner full-bleed, with the dark text-floor scrim Banner ships for
         art that carries writing — the caption must survive any preset. -->
    <Banner banner={story.preset} scrim="light" class="story-full" />

    <!-- Tap/hold layer: under the chrome, over the art. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="touch"
      onpointerdown={onDown}
      onpointerup={onUp}
      onpointercancel={onCancel}
    ></div>

    <div class="top">
      <div class="bars" aria-hidden="true">
        {#each stories as s, i (s.id)}
          <span class="bar">
            <span
              class="fill"
              style="width:{i < index ? 100 : i === index ? Math.min(100, progress * 100) : 0}%"
            ></span>
          </span>
        {/each}
      </div>
      <div class="head">
        <Avatar
          name={story.authorName || story.author}
          emoji={mem?.emoji || ""}
          color={mem?.color || story.color1}
          image={mem?.avatar || ""}
          size={30}
        />
        <span class="who">
          <span class="name">{story.authorName || story.author.slice(0, 9)}{isSelf ? " (you)" : ""}</span>
          <span class="when">{sealAgo(story.postedAt * 1000)}</span>
        </span>
        {#if isSelf}
          <button
            class="x del"
            class:armed={confirmDelete}
            aria-label={confirmDelete ? "Confirm delete" : "Delete moment"}
            onclick={deleteCurrent}
          >
            {#if confirmDelete}Delete?{:else}<Icon name="trash" size={15} />{/if}
          </button>
        {/if}
        <button class="x" aria-label="Close" onclick={() => onClose?.()}>
          <Icon name="close" size={16} />
        </button>
      </div>
    </div>

    <div class="caption-wrap">
      <p class="caption">{story.caption}</p>
      <!-- The author's accent pair as the underline — their colors sign the
           words the way they ring their tile in the tray. Backend-gated #hex. -->
      <span
        class="accent"
        aria-hidden="true"
        style="background:linear-gradient(90deg, {story.color1 || 'var(--accent)'}, {story.color2 ||
          story.color1 ||
          'var(--accent)'})"
      ></span>
    </div>

    {#if !isSelf}
      <button class="reply" onclick={reply}>
        <Icon name="spark" size={13} /> Reply
      </button>
    {/if}
  </div>
{/if}

<style>
  .viewer {
    position: fixed;
    inset: 0;
    z-index: 400; /* the studios' tier — above modals, below nothing that matters */
    background: #000;
    display: grid;
    place-items: center;
  }
  .viewer :global(.story-full) {
    position: absolute;
    inset: 0;
  }
  .touch {
    position: absolute;
    inset: 0;
    /* No default gestures: a hold must pause, not select or context-menu. */
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
    cursor: pointer;
  }
  .top {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    padding: calc(8px + var(--safe-top, 0px)) 12px 0;
    /* A quiet ceiling so the bars and name read over bright presets. */
    background: linear-gradient(rgba(0, 0, 0, 0.45), transparent);
    pointer-events: none;
  }
  .bars {
    display: flex;
    gap: var(--sp-1);
    margin-bottom: var(--sp-2);
  }
  .bar {
    flex: 1;
    height: 2.5px;
    border-radius: 2px;
    background: rgba(255, 255, 255, 0.3);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    background: #fff;
    /* Width is driven per-frame by the rAF clock — no CSS transition to fight
       it, and nothing here loops, so reduced-motion needs no special case. */
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .name {
    color: #fff;
    font-weight: 600;
    font-size: var(--fs-ui);
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .when {
    color: rgba(255, 255, 255, 0.75);
    font-size: var(--fs-small);
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  }
  .x {
    pointer-events: auto;
    background: rgba(0, 0, 0, 0.35);
    color: #fff;
    border-radius: 50%;
    width: 34px;
    height: 34px;
    display: grid;
    place-items: center;
    padding: 0;
    flex: none;
  }
  .x:hover,
  .x:active {
    background: rgba(0, 0, 0, 0.55);
  }
  /* Armed, the trash button becomes its own confirm — a red pill that says
     what the next tap does. Raw rgba like the rest of this chrome: everything
     here paints over art on black, not over the theme. */
  .del.armed {
    width: auto;
    border-radius: 999px;
    padding: 0 12px;
    background: rgba(196, 49, 42, 0.9);
    font-size: var(--fs-small);
    font-weight: 600;
  }
  .del.armed:hover,
  .del.armed:active {
    background: rgba(219, 55, 47, 0.95);
  }
  .caption-wrap {
    position: relative;
    z-index: 1;
    max-width: min(640px, 86vw);
    text-align: center;
    pointer-events: none;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-3);
  }
  .caption {
    margin: 0;
    color: #fff;
    font-size: clamp(20px, 4.5vw, 34px);
    font-weight: 700;
    line-height: 1.3;
    text-shadow: 0 2px 12px rgba(0, 0, 0, 0.55);
    word-break: break-word;
    white-space: pre-wrap;
  }
  .accent {
    width: 56px;
    height: 4px;
    border-radius: 2px;
  }
  .reply {
    position: absolute;
    bottom: calc(18px + var(--safe-bottom, 0px));
    left: 50%;
    transform: translateX(-50%);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: rgba(0, 0, 0, 0.45);
    color: #fff;
    border: 1px solid rgba(255, 255, 255, 0.35);
    border-radius: 999px;
    padding: 9px 18px;
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .reply:hover,
  .reply:active {
    background: rgba(0, 0, 0, 0.65);
  }
</style>
