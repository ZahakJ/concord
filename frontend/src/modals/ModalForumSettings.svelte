<script>
  // A forum's own settings: its description and the tag palette everyone
  // shares (guild state, announced over the guild-meta topic, gated on Manage
  // Channels).
  //
  // This dialog used to sell the board's look as well — a gradient header-art
  // grid of a dozen presets and List / Gallery / Cover card layouts. The board
  // has no hero and is one list of rows now, so offering them was offering
  // decoration that no longer draws. What a forum already stored (its banner,
  // a device's layout pref) is left as it is and simply not rendered.
  import { tick } from "svelte";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { humanError } from "../lib/errors.js";
  import { api } from "../lib/api.js";
  import { tooltip } from "../lib/tooltip.js";
  import { PERM, has } from "../lib/perms.js";
  import { TAG_LIMITS, validateTag, normalizeHex, runeLen } from "../lib/forum.js";

  let { forum: forumProp, onClose } = $props();

  // The LIVE channel record, not the snapshot the opener handed us: a prop is
  // the object as it was when the modal opened, and an edit made from here (the
  // description) refreshes the guild list, not the prop.
  const forum = $derived(
    (activeGuild()?.channels || []).find((c) => c.id === forumProp.id) || forumProp,
  );

  const guild = $derived(activeGuild());
  const canManage = $derived(
    !!guild && (guild.isOwner || has(guild.myPerms || 0, PERM.MANAGE_CHANNELS)),
  );

  // ---- tag palette --------------------------------------------------------
  // A draft copy. Nothing is sent until Save, because SetForumTags replaces the
  // WHOLE palette — an autosave per keystroke would broadcast a channel
  // announcement per character.
  //
  // Each row keeps its ID. Dropping the id on an edit mints a NEW tag and
  // orphans every post carrying the old one, which is the single most expensive
  // mistake this dialog could make.
  // Seeded once, on purpose: this is a DRAFT. If a peer edits the palette while
  // the dialog is open, adopting their version mid-edit would silently discard
  // what is being typed — the reconciliation happens at Save, which sends the
  // whole list anyway.
  // svelte-ignore state_referenced_locally
  let draft = $state(
    (forum.forumTags || []).map((t) => ({ id: t.id, name: t.name, color: t.color, emoji: t.emoji || "" })),
  );
  let busy = $state(false);
  let err = $state("");

  const NEW_COLORS = ["#14a394", "#4a7cf0", "#a06bff", "#e0555b", "#d9a13c", "#3ba55d"];

  let rowsEl = $state(null);

  async function addTag() {
    if (draft.length >= TAG_LIMITS.perForum) return;
    // No id: the backend mints one and returns it. Colour cycles so a fresh
    // palette doesn't come out as six identical teal chips.
    draft = [...draft, { name: "", color: NEW_COLORS[draft.length % NEW_COLORS.length], emoji: "" }];
    err = "";
    // Past the fourth tag the new row landed below the fold of an inner
    // scroller nested inside a sheet that also scrolls, so "Add tag" looked
    // like it did nothing — people tapped it repeatedly, ended up with four
    // empty rows, and Save went dead with no visible cause. Bring the row into
    // view and put the caret in it, so the next thing you do is name it.
    await tick();
    const last = rowsEl?.lastElementChild;
    last?.scrollIntoView({ block: "nearest" });
    last?.querySelector("input.nm")?.focus();
  }
  function dropTag(i) {
    draft = draft.filter((_, n) => n !== i);
    err = "";
  }
  function patch(i, key, value) {
    draft = draft.map((t, n) => (n === i ? { ...t, [key]: value } : t));
    err = "";
  }
  function move(i, dir) {
    const j = i + dir;
    if (j < 0 || j >= draft.length) return;
    const next = [...draft];
    [next[i], next[j]] = [next[j], next[i]];
    draft = next;
  }

  // Live validity, so Save is only enabled when it can succeed. The first bad
  // row's own message is shown under it — a single error line at the bottom of a
  // list of eight rows tells you nothing about which one.
  const rowErr = $derived(draft.map((t) => validateTag(t)));
  const dupes = $derived.by(() => {
    const seen = new Map();
    const out = new Array(draft.length).fill(false);
    draft.forEach((t, i) => {
      const k = t.name.trim().toLowerCase();
      if (!k) return;
      if (seen.has(k)) out[i] = true;
      else seen.set(k, i);
    });
    return out;
  });
  const valid = $derived(rowErr.every((e) => !e) && dupes.every((d) => !d));

  async function saveTags() {
    if (!valid) return;
    busy = true;
    err = "";
    try {
      // Send the full desired list — this REPLACES the palette, it is not a
      // delta — with ids preserved and colours normalized to the strict
      // lowercase #rrggbb the backend accepts.
      const payload = draft.map((t) => ({
        ...(t.id ? { id: t.id } : {}),
        name: t.name.trim(),
        color: normalizeHex(t.color),
        emoji: t.emoji || "",
      }));
      const saved = await api.setForumTags(guild.id, forum.id, payload);
      draft = (saved || []).map((t) => ({
        id: t.id,
        name: t.name,
        color: t.color,
        emoji: t.emoji || "",
      }));
      await refreshGuilds();
      flash(payload.length ? "Tags saved" : "Tags cleared", "success");
    } catch (e) {
      err = humanError(e);
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="Forum board" {onClose} wide>
  <!-- The description is edited where every channel's is; here it is a row
       that says what it is now and opens that editor. -->
  <section>
    <div class="sec-head"><h4>Description</h4></div>
    <div class="desc-row">
      <p class="desc" class:none={!forum.topic}>{forum.topic || "No description yet"}</p>
      {#if canManage}
        <button
          class="link"
          onclick={() => (S.modal = { kind: "channelTopic", channel: forum, from: "forumSettings" })}
        >
          Edit
        </button>
      {/if}
    </div>
  </section>

  <section>
    <div class="sec-head">
      <h4>Tags</h4>
      <span class="shared">Everyone in {guild?.name || "this guild"}</span>
    </div>

    {#if !canManage}
      <p class="note">
        Only members who can manage channels edit this palette. You can still put
        the forum's existing tags on your own posts from a post's ⋯ menu.
      </p>
      {#if draft.length}
        <div class="ro">
          {#each draft as t (t.id)}
            <span class="chip" style="--tc:{t.color}">{t.emoji ? `${t.emoji} ` : ""}{t.name}</span>
          {/each}
        </div>
      {:else}
        <p class="note">This forum has no tags yet.</p>
      {/if}
    {:else}
      {#if draft.length}
        <div class="rows" bind:this={rowsEl}>
          {#each draft as t, i (t.id || `new-${i}`)}
            <div class="row">
              <input
                class="col"
                type="color"
                value={normalizeHex(t.color) || "#14a394"}
                aria-label="Tag colour"
                oninput={(e) => patch(i, "color", e.currentTarget.value)}
              />
              <input
                class="em"
                value={t.emoji}
                placeholder="🙂"
                aria-label="Tag emoji"
                maxlength={TAG_LIMITS.emojiChars}
                oninput={(e) => patch(i, "emoji", e.currentTarget.value)}
              />
              <input
                class="nm"
                value={t.name}
                placeholder="Tag name"
                aria-label="Tag name"
                oninput={(e) => patch(i, "name", e.currentTarget.value)}
              />
              <!-- Only near the limit. "3/24" beside a name field, on every
                   row, all the time, is noise you have to decode — and beside a
                   pair of stepper arrows it read as an icon index rather than a
                   character count. It appears when it starts to matter. -->
              {#if runeLen(t.name) >= TAG_LIMITS.nameRunes * 0.8}
                <span class="count" class:over={runeLen(t.name) > TAG_LIMITS.nameRunes}>
                  {runeLen(t.name)}/{TAG_LIMITS.nameRunes}
                </span>
              {/if}
              <!-- Labelled, because the two chevrons beside a counter read as
                   one three-part control that does something to the number. -->
              <span class="ord" role="group" aria-label="Reorder this tag">
                <button
                  class="mini"
                  aria-label="Move {t.name || "tag"} earlier"
                  use:tooltip={"Move earlier"}
                  disabled={i === 0}
                  onclick={() => move(i, -1)}><Icon name="chevron" size={12} /></button
                >
                <button
                  class="mini down"
                  aria-label="Move {t.name || "tag"} later"
                  use:tooltip={"Move later"}
                  disabled={i === draft.length - 1}
                  onclick={() => move(i, 1)}><Icon name="chevron" size={12} /></button
                >
              </span>
              <button class="mini del" aria-label="Delete tag" onclick={() => dropTag(i)}>
                <Icon name="trash" size={13} />
              </button>
              {#if rowErr[i] || dupes[i]}
                <p class="row-err">{rowErr[i] || "Another tag already has that name."}</p>
              {/if}
            </div>
          {/each}
        </div>
      {:else}
        <p class="note">
          No tags yet. Tags are how a board becomes searchable — “Bug”, “Idea”,
          “Answered” — and they filter the board with one click.
        </p>
      {/if}

      <div class="tag-acts">
        <button class="ghost" onclick={addTag} disabled={draft.length >= TAG_LIMITS.perForum}>
          <Icon name="plus" size={13} /> Add tag
        </button>
        <span class="muted small">{draft.length}/{TAG_LIMITS.perForum}</span>
      </div>

      {#if err}<p class="bad">{err}</p>{/if}

      <p class="note">
        Deleting a tag leaves it off every post that carried it — the chip just
        stops showing. A post can wear up to {TAG_LIMITS.perPost}.
      </p>
    {/if}
  </section>

  {#if canManage}
    <div class="actions">
      <button class="ghost" onclick={onClose}>Cancel</button>
      <button class="primary" disabled={busy || !valid} onclick={saveTags}>Save</button>
    </div>
  {/if}
</Modal>

<style>

  /* ---- sections --------------------------------------------------------- */
  section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding-top: var(--sp-1);
    border-top: 1px solid var(--border);
  }
  .desc-row {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
  }
  .desc {
    flex: 1;
    min-width: 0;
    margin: 0;
    color: var(--text);
    line-height: 1.5;
  }
  .desc.none {
    color: var(--text-muted);
  }
  .sec-head {
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
  }
  h4 {
    margin: 0;
    font-size: var(--fs-ui);
    font-weight: 600;
    letter-spacing: 0.02em;
    color: var(--text-muted);
  }
  .shared {
    font-size: var(--fs-small);
    padding: 2px 7px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .shared {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .note {
    margin: 0;
    font-size: var(--fs-compact);
    line-height: 1.5;
    color: var(--text-muted);
  }
  .bad {
    margin: 0;
    font-size: var(--fs-compact);
    color: var(--danger-text);
  }
  .muted {
    color: var(--text-muted);
  }
  .small {
    font-size: var(--fs-small);
  }


  /* ---- tag rows --------------------------------------------------------- */
  .rows {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 236px;
    overflow-y: auto;
    padding-right: 2px;
  }
  .row {
    display: grid;
    grid-template-columns: 32px 46px 1fr auto auto auto;
    align-items: center;
    gap: 6px;
  }
  .row input {
    background: var(--bg-3);
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--fs-ui);
    padding: 7px 9px;
    min-width: 0;
  }
  .row input:focus {
    outline: none;
    border-color: var(--accent);
  }
  /* Four classes deep for the same reason RichEditor's swatch is: Modal's mobile
     sheet puts `min-height: 44px` on `.dialog :global(input:not(…))`, whose
     specificity (0,3,1) beats a plain `.col` — and a 34×44 colour chip is a
     lozenge sitting a head taller than the two fields beside it. */
  .rows .row input.col {
    /* A native colour input, on purpose: it is the one control that can only
       produce the strict #rrggbb the backend accepts. */
    padding: 2px !important;
    height: 34px;
    min-height: 34px;
    cursor: pointer;
  }
  .em {
    text-align: center;
  }
  .count {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
  }
  .count.over {
    color: var(--danger-text);
  }
  .ord {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .mini {
    display: grid;
    place-items: center;
    width: 24px;
    height: 15px;
    padding: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-faint);
  }
  .mini:hover:not(:disabled) {
    background: var(--bg-3);
    color: var(--text);
  }
  .mini:disabled {
    opacity: 0.3;
  }
  .mini :global(svg) {
    transform: rotate(-90deg);
  }
  .mini.down :global(svg) {
    transform: rotate(90deg);
  }
  .mini.del {
    width: 28px;
    height: 28px;
    color: var(--text-muted);
  }
  .mini.del :global(svg) {
    transform: none;
  }
  .mini.del:hover {
    background: var(--danger-soft);
    color: var(--danger-text);
  }
  .row-err {
    grid-column: 1 / -1;
    margin: -2px 0 2px;
    font-size: var(--fs-small);
    color: var(--danger-text);
  }
  .tag-acts {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .ro {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 3px 9px;
    border-radius: 999px;
    font-size: var(--fs-small);
    font-weight: 600;
    /* Colour identifies, theme ink reads — the same rule as the board's chips,
       measured in forum.test.mjs. */
    background: color-mix(in srgb, var(--tc) 20%, var(--bg-elevated));
    color: var(--text);
  }


  @media (max-width: 768px) {
    /* 56px for the emoji, not 48: the sheet forces 16px type into this field and
       it accepts up to TAG_LIMITS.emojiChars characters — at 48px minus 18px of
       padding a single emoji filled it and a second one clipped. */
    .row {
      grid-template-columns: 34px 56px 1fr auto;
      gap: 10px;
    }
    /* The reorder arrows are the first thing to go on a phone: the palette's
       order is cosmetic, and 15px targets are not. */
    .ord {
      display: none;
    }
    .count {
      display: none;
    }
    /* A destructive control rendered 28 wide by 44 tall — the sheet's floor set
       the height and nothing set the width — six pixels from the field the same
       finger is typing in, and dropTag() has no confirm. Square it up and put
       real space between it and the input. */
    .mini.del {
      width: 44px;
      height: 44px;
    }
    /* Two nested scrollers inside a sheet that also scrolls: flicking past the
       end of either chained straight into the sheet's own drag-to-dismiss, and
       three-and-a-half visible rows hid the rest behind a scroll with no hint
       that it existed. On a phone the sheet is the only scroller. */
    .rows {
      max-height: none;
      overflow-y: visible;
    }
  }
</style>
