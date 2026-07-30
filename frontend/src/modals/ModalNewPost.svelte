<script>
  // New forum post. A post IS a thread channel under the forum, so this dialog
  // creates the channel (title + tags) and posts the opening message that gives
  // the post its author, its excerpt and its first content.
  //
  // It used to be a bare input plus a bare textarea. It is now the same
  // RichEditor the advanced composer uses — one writing surface, so formatting,
  // emoji, autocomplete, attachments and the live preview exist here without a
  // fourth copy of a formatting toolbar.
  //
  // Three things are specific to a post and live here:
  //   · the TITLE, which the backend caps at 64 BYTES (it is a channel name),
  //     so the budget is measured in the unit that can actually truncate it
  //   · TAGS from the forum's own palette, within the backend's limits
  //   · a draft per forum, so closing by accident doesn't cost a long post
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import RichEditor from "./RichEditor.svelte";
  import { S, refreshGuilds, selectChannel, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { TAG_LIMITS, TINT_ALPHA, normalizeHex } from "../lib/forum.js";
  import { clampToBytes, titleFit, bodyStats, TITLE_MAX_BYTES, saveDraft, loadDraft, clearDraft, draftAge } from "../lib/postdraft.js";

  let { forum, onClose } = $props();

  let title = $state("");
  let body = $state("");
  let pending = $state([]);
  let mode = $state("split");
  let tags = $state([]); // selected tag ids
  let busy = $state(false);
  let step = $state(""); // what the submit is doing right now
  let guard = $state("");
  let restored = $state("");

  const scope = $derived(forum?.id ? `post:${forum.id}` : "");
  // The palette comes off the guild snapshot (ChannelView.forumTags), which is
  // the same in-memory channel ForumBoard reads — they cannot disagree.
  // Colours are validated hex from the backend; normalizeHex is the second gate,
  // because this value lands in a CSS custom property.
  const palette = $derived(
    (forum?.forumTags || [])
      .map((t) => ({ ...t, hex: normalizeHex(t.color) }))
      .filter((t) => t.hex),
  );
  const fit = $derived(titleFit(title));
  const stats = $derived(bodyStats(body));
  // A post with an empty opening message has no provable author, so a plain
  // member could not then tag or answer their own post — and the board renders
  // it as a pending card forever. Both fields are required, and the button says
  // which one is missing rather than just sitting there greyed out.
  const missing = $derived(!title.trim() ? "title" : !body.trim() ? "body" : "");
  const canPost = $derived(!busy && !missing);
  const offline = $derived(!!S.netStatus && S.netStatus.peers === 0 && S.netStatus.hasBootstrap);

  let restoreDone = false;
  $effect(() => {
    const s = scope;
    if (restoreDone || !s) return;
    restoreDone = true;
    const d = loadDraft(s);
    if (!d) return;
    title = d.title;
    body = d.body;
    // Drop any tag the forum no longer defines — deleting a tag deliberately
    // leaves its id behind, and a chip for a tag that doesn't exist is worse
    // than no chip.
    tags = d.tags.filter((id) => palette.some((t) => t.id === id)).slice(0, TAG_LIMITS.perPost);
    restored = draftAge(d.at);
  });

  function persist() {
    if (scope) saveDraft(scope, { title, body, tags });
  }

  // clampToBytes rather than maxlength: the backend measures the title in UTF-8
  // BYTES and truncates with a raw byte slice, which can cut a rune in half. A
  // title of emoji runs out at 16 characters, and this is where that is honest.
  function onTitle(e) {
    const v = clampToBytes(e.currentTarget.value, TITLE_MAX_BYTES);
    if (v !== e.currentTarget.value) e.currentTarget.value = v;
    title = v;
    persist();
  }

  function toggleTag(id) {
    if (tags.includes(id)) tags = tags.filter((t) => t !== id);
    else if (tags.length < TAG_LIMITS.perPost) tags = [...tags, id];
    else flash(`A post can carry at most ${TAG_LIMITS.perPost} tags`, "error");
    persist();
  }

  const dirty = $derived(!!title.trim() || !!body.trim() || tags.length > 0 || pending.length > 0);
  const heavy = $derived(pending.length > 0 || stats.words >= 12 || !!title.trim());

  function requestClose() {
    if (dirty && heavy && !guard) {
      guard = "close";
      return;
    }
    persist();
    onClose();
  }
  function discardAndClose() {
    if (scope) clearDraft(scope);
    onClose();
  }
  function keepAndClose() {
    persist();
    // Before onClose, not after: touching reactive state in the same tick as
    // S.modal = null makes App.svelte re-read the (now null) modal for this
    // component's props. See the same note in ModalCompose.
    flash("Draft kept — “New post” will pick it up", "info");
    onClose();
  }

  async function create() {
    if (!canPost) return;
    busy = true;
    step = "Creating the post…";
    let ch = null;
    try {
      // The tags ride along with the create call (5th argument), so the post is
      // never briefly visible untagged on anyone's board.
      ch = await api.createThread(S.activeGuildId, forum.id, title.trim(), body.trim(), tags);
      // Attachments can only be sent INTO a channel, and the channel is what we
      // just made — so they follow the opening message rather than riding with
      // it. That ordering also matters for the board: the opening message must
      // be the text, because that is what becomes the card's excerpt and what
      // proves who wrote the post.
      if (pending.length) {
        step = `Sending ${pending.length} attachment${pending.length === 1 ? "" : "s"}…`;
        for (const a of pending) {
          if (a.isImage) await api.sendAttachment(ch.id, a.dataUrl, a.w, a.h, "", !!a.spoiler, a.name || "", a.desc || "");
          else await api.sendFile(ch.id, a.dataUrl, a.name, "");
        }
        pending = [];
      }
      if (scope) clearDraft(scope);
      await refreshGuilds();
      onClose();
      await selectChannel(ch.id);
      flash("Post created", "success");
    } catch (err) {
      // If the post itself exists, say so — retrying would create a second one.
      flash(ch ? `Post created, but an attachment failed: ${err?.message || err}` : err);
      busy = false;
      step = "";
      if (ch) {
        if (scope) clearDraft(scope);
        onClose();
        await selectChannel(ch.id);
      }
    }
  }
</script>

<Modal title="New post" size="xl" onClose={requestClose}>
  <div class="np">
    <div class="ctx">
      <span class="dest"><Icon name="forum" size={13} /> Posting in <strong>{forum.name}</strong></span>
      {#if offline}
        <span class="badge off"><Icon name="alert" size={12} /> Offline — it'll reach others when you reconnect</span>
      {/if}
      {#if restored}
        <span class="badge draft">
          <Icon name="check" size={12} /> Draft from {restored}
          <button
            type="button"
            onclick={() => {
              title = "";
              body = "";
              tags = [];
              restored = "";
              if (scope) clearDraft(scope);
            }}>Start over</button>
        </span>
      {/if}
    </div>

    <!-- The title is the post. It gets its own considered field: a large,
         quiet type ramp, and a budget you can watch rather than a box that
         stops accepting keys for no visible reason. -->
    <div class="titlefield" class:full={fit.full}>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="titleinput"
        value={title}
        oninput={onTitle}
        placeholder="What's this post about?"
        aria-label="Post title"
        aria-describedby="titlebudget"
        autofocus={!S.isMobile} />
      <div class="budget" id="titlebudget">
        <span class="bnum" class:warn={fit.tone === "warn"} class:over={fit.tone === "full"}>{fit.left}</span>
        <span class="btrack" aria-hidden="true">
          <span class="bfill" class:warn={fit.tone === "warn"} class:over={fit.tone === "full"} style="transform:scaleX({Math.min(1, fit.bytes / fit.max)})"
          ></span>
        </span>
      </div>
    </div>

    {#if palette.length}
      <!-- Tags are optional, so they're one quiet row rather than a section with
           a heading. The chip carries the tag's colour as a bounded tint with the
           label in the theme's ink — the same rule the board uses, so a tag looks
           the same everywhere and its contrast is proven, not hoped for. -->
      <div class="tagrow" role="group" aria-label="Tags">
        {#each palette as t (t.id)}
          <button
            type="button"
            class="chip"
            class:on={tags.includes(t.id)}
            style="--tc:{t.hex}; --tint:{Math.round(TINT_ALPHA * 100)}%"
            aria-pressed={tags.includes(t.id)}
            onclick={() => toggleTag(t.id)}>
            {#if t.emoji}<span class="chip-em">{t.emoji}</span>{/if}
            {t.name}
            {#if tags.includes(t.id)}<Icon name="check" size={11} />{/if}
          </button>
        {/each}
        <span class="tagcount">{tags.length}/{TAG_LIMITS.perPost}</span>
      </div>
    {/if}

    <RichEditor
      bind:body
      bind:pending
      bind:mode
      minHeight={180}
      previewTitle={title.trim()}
      attachNote="Files post into the thread right after your opening message."
      placeholder={"Start the discussion…\n\nThe toolbar and markdown both work: **bold**, > quote, - list, ## heading, ```code```, ||spoiler||. Paste or drop an image to attach it."}
      onSubmit={create}
      onInput={persist}
      submitHint="⌘/Ctrl + ↵ to post" />

    {#if guard === "close"}
      <div class="guard" role="group" aria-live="polite" aria-label="Unsaved work">
        <p>
          <Icon name="alert" size={15} />
          {#if pending.length}
            {pending.length} attachment{pending.length === 1 ? "" : "s"} staged — those can't be saved with a draft.
          {:else}
            This post isn't published yet.
          {/if}
        </p>
        <button type="button" class="ghost" onclick={() => (guard = "")}>Keep writing</button>
        <button type="button" class="ghost danger" onclick={discardAndClose}>Discard</button>
        <button type="button" onclick={keepAndClose}>Save for later</button>
      </div>
    {:else}
      <div class="actions">
        {#if missing && (title.trim() || body.trim())}
          <span class="need">
            {missing === "title" ? "A post needs a title." : "An opening message is what proves you wrote this post."}
          </span>
        {/if}
        <button type="button" class="ghost" onclick={requestClose}>Cancel</button>
        <button type="button" class="go" onclick={create} disabled={!canPost}>
          {#if busy}
            <span class="spin" aria-hidden="true"></span> {step || "Posting…"}
          {:else}
            <Icon name="send" size={14} /> Post
          {/if}
        </button>
      </div>
    {/if}
  </div>
</Modal>

<style>
  .np {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
    text-align: left;
  }
  /* See the note on RichEditor's .rx: on the phone the dialog is an auto-height
     sheet, and flex negotiation there either crushes these blocks into each
     other or balloons them. Natural heights + the sheet's own scroll. */
  @media (max-width: 760px), (pointer: coarse) {
    .np {
      flex: none;
      min-height: auto;
    }
  }
  .ctx {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 12.5px;
    color: var(--text-muted);
  }
  .dest {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .dest :global(svg) {
    color: var(--text-faint);
  }
  .dest strong {
    color: var(--text);
    font-weight: 600;
  }
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 3px 9px;
    font-size: 11.5px;
    border-radius: 999px;
    background: var(--bg-3);
  }
  .badge.off {
    color: var(--warn-text);
    background: color-mix(in srgb, var(--warn) 14%, transparent);
  }
  .badge.draft {
    color: var(--ok-text);
    background: var(--ok-soft);
  }
  .badge button {
    padding: 0 0 0 6px;
    min-height: 0;
    font-size: 11.5px;
    font-weight: 600;
    color: inherit;
    background: none;
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  /* ---- title ------------------------------------------------------------ */
  .titlefield {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 4px 14px 4px 16px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    transition:
      border-color 0.15s ease,
      box-shadow 0.15s ease;
  }
  .titlefield:focus-within {
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent);
  }
  .titlefield.full {
    border-color: color-mix(in srgb, var(--warn) 55%, transparent);
  }
  /* Descendant selectors throughout: Modal's mobile sheet sets a 44px
     min-height and a 16px font on `.dialog :global(input…)`, at a specificity a
     bare class can't reach. The title deserves its own scale. */
  .titlefield .titleinput {
    flex: 1;
    min-width: 0;
    padding: 10px 0;
    font-family: inherit;
    font-size: 20px;
    font-weight: 700;
    letter-spacing: -0.01em;
    line-height: 1.3;
    color: var(--text);
    background: transparent;
    border: none;
    box-shadow: none;
    outline: none;
  }
  .titlefield .titleinput::placeholder {
    font-weight: 600;
    color: var(--text-faint);
  }
  .budget {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .bnum {
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    color: var(--text-faint);
    min-width: 2ch;
    text-align: right;
  }
  .bnum.warn {
    color: var(--warn-text);
  }
  .bnum.over {
    color: var(--danger-text);
    font-weight: 700;
  }
  /* A bar, not a ring: it reads left-to-right like the text it measures, and it
     animates on transform alone. */
  .btrack {
    width: 56px;
    height: 4px;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  .bfill {
    display: block;
    width: 100%;
    height: 100%;
    border-radius: 999px;
    background: var(--accent);
    transform-origin: left center;
    transition:
      transform 0.16s ease,
      background 0.16s ease;
  }
  .bfill.warn {
    background: var(--warn);
  }
  .bfill.over {
    background: var(--danger);
  }
  @media (prefers-reduced-motion: reduce) {
    .bfill {
      transition: none;
    }
  }

  /* ---- tags ------------------------------------------------------------- */
  .tagrow {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }
  /* A tag's colour IDENTIFIES the chip; the theme's ink READS it. A member picks
     that colour, so painting the label in it would be a contrast lottery — the
     tint is bounded by TINT_ALPHA, which lib/forum.test.mjs composites over both
     card surfaces to prove --text still clears 4.5:1. Same rule as the board, so
     a tag looks identical in both places. */
  .tagrow .chip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 5px 11px;
    min-height: 30px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-muted);
    background: var(--bg-3);
    border: 1px solid transparent;
    border-radius: 999px;
    transition:
      background 0.14s ease,
      color 0.14s ease,
      border-color 0.14s ease,
      transform 0.16s cubic-bezier(0.34, 1.4, 0.5, 1);
  }
  .tagrow .chip:hover {
    color: var(--text);
    background: color-mix(in srgb, var(--tc) var(--tint), var(--bg-3));
    transform: translateY(-1px);
  }
  .tagrow .chip.on {
    color: var(--text);
    background: color-mix(in srgb, var(--tc) var(--tint), var(--bg-2));
    border-color: color-mix(in srgb, var(--tc) 60%, transparent);
  }
  .tagrow .chip :global(svg) {
    color: var(--text-muted);
  }
  .chip-em {
    font-size: 11px;
    line-height: 1;
  }
  .tagcount {
    font-size: 11px;
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
  }

  /* ---- actions / guard -------------------------------------------------- */
  .actions,
  .guard {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }
  .need {
    margin-right: auto;
    font-size: 12px;
    color: var(--text-muted);
  }
  .guard :global(svg) {
    color: var(--warn-text);
    flex-shrink: 0;
  }
  .guard p {
    display: flex;
    align-items: center;
    gap: 7px;
    margin: 0 auto 0 0;
    font-size: 13px;
    color: var(--text);
  }
  .guard .danger {
    color: var(--danger-text);
  }
  .go {
    display: inline-flex;
    align-items: center;
    gap: 7px;
  }
  .spin {
    width: 13px;
    height: 13px;
    border: 2px solid color-mix(in srgb, var(--accent-fg) 35%, transparent);
    border-top-color: var(--accent-fg);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  /* No prefers-reduced-motion block for the spinner: app.css already zeroes
     every animation-duration and clamps iteration-count to 1 with !important
     under that query, which a component rule cannot outrank — and the button's
     own "Sending…"/"Posting…" label is what carries the meaning anyway. */

  @media (max-width: 760px) {
    .titlefield {
      flex-wrap: wrap;
      padding: 4px 14px;
    }
    .titlefield .titleinput {
      font-size: 19px;
    }
    .budget {
      width: 100%;
      justify-content: flex-end;
      padding-bottom: 6px;
    }
    .guard {
      flex-wrap: wrap;
    }
    .guard p,
    .need {
      width: 100%;
      margin: 0;
    }
    .guard button {
      flex: 1;
    }
  }
</style>
