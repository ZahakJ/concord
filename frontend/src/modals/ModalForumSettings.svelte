<script>
  // A forum's own settings: the tag palette everyone shares, and the board's
  // look, which is yours alone.
  //
  // The split is the point. Tags are guild state (announced over the guild-meta
  // topic, gated on Manage Channels) and every member sees the same palette. The
  // layout and the header art are device-local reading preferences — a support
  // queue wants a dense list on a laptop and a gallery on a tablet, and neither
  // choice is anyone else's business. Sections say which is which, out loud,
  // because a setting that silently affects other people is a trap.
  import { tick } from "svelte";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Banner from "../Banner.svelte";
  import { BANNER_BY_ID } from "../lib/banners.js";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { tooltip } from "../lib/tooltip.js";
  import { PERM, has } from "../lib/perms.js";
  import {
    LAYOUTS,
    TAG_LIMITS,
    validateTag,
    normalizeHex,
    runeLen,
    washFor,
    readBoardPrefs,
    writeBoardPrefs,
  } from "../lib/forum.js";

  let { forum: forumProp, onClose } = $props();

  // The LIVE channel record, not the snapshot the opener handed us.
  //
  // A prop is the object as it was when the modal opened. Saving the header art
  // updates the channel on the backend and refreshes the guild list, but that
  // prop keeps its old value forever — so every tile stayed unselected no matter
  // what you clicked, and the picker looked broken while the setting was in fact
  // being saved. Read it back out of the guild each time instead; the prop is
  // only the fallback for the instant before the first refresh lands.
  const forum = $derived(
    (activeGuild()?.channels || []).find((c) => c.id === forumProp.id) || forumProp,
  );

  const guild = $derived(activeGuild());
  const canManage = $derived(
    !!guild && (guild.isOwner || has(guild.myPerms || 0, PERM.MANAGE_CHANNELS)),
  );

  // ---- device-local board look -------------------------------------------
  // Applied immediately, not on Save: the board is right there behind the
  // dialog, so a look you pick is a look you see. writeBoardPrefs announces the
  // change so the board adopts it live.
  // svelte-ignore state_referenced_locally
  let prefs = $state(readBoardPrefs(forum.id));
  const save = (patch) => (prefs = writeBoardPrefs(forum.id, patch, prefs));

  const wash = $derived(washFor(forum.id));

  // ---- header art: SHARED, on the channel record --------------------------
  // This used to live in the same device-local store as the layout, so a banner
  // was a personal preference nobody else could see — decoration, not identity.
  // Layout stays local (how YOU like the posts arranged is your business); the
  // art is the forum's, so it rides the channel and every member gets it.
  const art = $derived(forum.banner || "");
  const isUploaded = $derived(!!art && !art.startsWith("preset:"));
  const MAX_ART = 384 * 1024; // must match maxForumBannerBytes in internal/app/forum.go

  async function setArt(banner) {
    try {
      await api.setForumBanner(guild.id, forum.id, banner);
      await refreshGuilds();
    } catch (err) {
      flash(err);
    }
  }

  function applyArtFile(file) {
    if (!file || !file.type.startsWith("image/")) return;
    const reader = new FileReader();
    reader.onload = () => {
      const url = String(reader.result);
      // Checked here as well as in Go so the answer is instant and specific:
      // a rejection that arrives after an upload round trip reads as a failure
      // rather than as a size limit.
      if (url.length > MAX_ART) {
        flash(`That image is ${Math.round(url.length / 1024)} KB — the limit is ${MAX_ART / 1024} KB`, "error");
        return;
      }
      setArt(url);
    };
    reader.readAsDataURL(file);
  }

  function pickBanner() {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.onchange = () => applyArtFile(input.files?.[0]);
    input.click();
  }

  // Paste or drop anywhere on the panel — the two gestures people try before
  // they look for a button.
  function onPaste(e) {
    if (!canManage) return;
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (!item) return;
    e.preventDefault();
    applyArtFile(item.getAsFile());
  }

  // A curated dozen, not the whole catalogue. The full library is a profile
  // picker's job; a forum header wants art that a title can sit on, and offering
  // thirty of them would be the wall of choices this redesign is trying not to
  // be. Looked up rather than hardcoded, so a preset that gets renamed or
  // retired in lib/banners.js drops out of the grid instead of rendering an
  // empty tile.
  const PICKS = [
    "galaxy",
    "aurora",
    "nebula",
    "meteors",
    "sunrise",
    "ocean",
    "forest",
    "campfire",
    "sakura",
    "synthwave",
    "circuit",
    "rain",
  ];
  const artChoices = $derived(PICKS.map((id) => BANNER_BY_ID[id]).filter(Boolean));

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
      err = e?.message || String(e);
    } finally {
      busy = false;
    }
  }
</script>

<svelte:window onpaste={onPaste} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  ondragover={(e) => e.preventDefault()}
  ondrop={(e) => {
    if (!canManage) return;
    e.preventDefault();
    applyArtFile(e.dataTransfer?.files?.[0]);
  }}
>
<Modal title="Forum board" {onClose} wide>
  <!-- A live sample, not a description. Every choice below changes this strip,
       so the header you are designing is the header you are looking at. -->
  <div class="preview">
    <!-- `art` is the forum's shared banner off the live channel record — the
         prefs never carry the art any more, and a preview wired to them showed
         Auto forever. -->
    <Banner
      banner={art}
      color={art ? "" : wash.color}
      color2={art ? "" : wash.color2}
      style={{ angle: wash.angle }}
      scale={0.5}
    />
    <span class="pv-scrim" aria-hidden="true"></span>
    <div class="pv-in">
      <span class="pv-glyph"><Icon name="forum" size={14} /></span>
      <div class="pv-words">
        <strong>{forum.name}</strong>
        <span>{forum.topic || "No description yet"}</span>
      </div>
    </div>
  </div>

  <section>
    <div class="sec-head">
      <h4>Board look</h4>
      <span class="only">On this device</span>
    </div>
    <div class="layouts">
      {#each LAYOUTS as l (l.id)}
        <button
          class="lay"
          class:on={prefs.layout === l.id}
          aria-pressed={prefs.layout === l.id}
          onclick={() => save({ layout: l.id })}
        >
          <!-- Diagrams, not icons: three abstract mini-boards say what changes
               far faster than three nouns do. -->
          <span class="dia {l.id}" aria-hidden="true">
            <i></i><i></i><i></i>
          </span>
          <b>{l.label}</b>
          <em>{l.hint}</em>
        </button>
      {/each}
    </div>

    <div class="sub-head">
      Header art
      {#if canManage}<span class="shared">Everyone in {guild?.name || "this guild"}</span>{/if}
    </div>
    <div class="arts">
      <button class="art" class:on={!art} onclick={() => setArt("")}>
        <Banner color={wash.color} color2={wash.color2} style={{ angle: wash.angle }} scale={0.3} />
        <span class="art-name">Auto</span>
      </button>
      {#if canManage}
        <!-- Your own picture. Offered FIRST after Auto because it is what people
             come here for; a preset is the fallback when you have no art. -->
        <button class="art up" class:on={isUploaded} onclick={pickBanner} title="Upload an image">
          {#if isUploaded}
            <span class="thumb" style="background-image:url('{art}')"></span>
          {:else}
            <span class="up-mark"><Icon name="plus" size={16} /></span>
          {/if}
          <span class="art-name">{isUploaded ? "Your image" : "Upload…"}</span>
        </button>
      {/if}
      {#each artChoices as b (b.id)}
        <button
          class="art"
          class:on={art === `preset:${b.id}`}
          title={b.name}
          onclick={() => setArt(`preset:${b.id}`)}
        >
          <Banner banner={`preset:${b.id}`} scale={0.3} />
          <span class="art-name">{b.name}</span>
        </button>
      {/each}
    </div>
    <p class="note">
      {#if canManage}
        “Auto” colours the header from this forum's own id, so it looks the same
        for everyone with nothing stored. Anything else you pick here — a preset
        or your own image — is the forum's, and every member sees it. Drop or
        paste an image anywhere on this panel to use it.
      {:else}
        The header art belongs to the forum, and changing it needs permission to
        manage channels.
      {/if}
    </p>
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

  <div class="actions">
    {#if canManage}
      <button
        class="ghost"
        onclick={() => (S.modal = { kind: "channelTopic", channel: forum, from: "forumSettings" })}
      >
        Edit description
      </button>
    {/if}
    <span class="spacer"></span>
    <button class="ghost" onclick={onClose}>Close</button>
    {#if canManage}
      <button disabled={busy || !valid} onclick={saveTags}>Save tags</button>
    {/if}
  </div>
</Modal>
</div>

<style>
  /* The upload tile matches the preset tiles so the row reads as one set of
     choices rather than a grid with a button wedged into it. */
  .art.up {
    display: grid;
    place-items: center;
  }
  .art.up .up-mark {
    display: grid;
    place-items: center;
    width: 100%;
    aspect-ratio: 16 / 5;
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
  }
  .art.up .thumb {
    display: block;
    width: 100%;
    aspect-ratio: 16 / 5;
    border-radius: var(--radius-sm);
    background-size: cover;
    background-position: center;
  }
  /* ---- live preview ----------------------------------------------------- */
  .preview {
    position: relative;
    isolation: isolate;
    height: 84px;
    /* The dialog is a flex column that scrolls; without this the preview is the
       one item with a fixed height and gets shrunk to nothing by everything
       below it. */
    flex: none;
    border-radius: var(--radius-md);
    overflow: hidden;
    border: 1px solid var(--border);
    display: flex;
    align-items: flex-end;
  }
  .preview :global(.bnr) {
    position: absolute;
    inset: 0;
    z-index: 0;
  }
  /* Same measured floor as the board's own hero (see lib/forum.js SCRIM_FLOOR):
     the preview has to be honest about legibility, not flatter the art. */
  .pv-scrim {
    position: absolute;
    inset: 0;
    z-index: 1;
    background: linear-gradient(
      180deg,
      rgba(0, 0, 0, 0.15) 0%,
      rgba(0, 0, 0, 0.62) 42%,
      rgba(0, 0, 0, 0.8) 100%
    );
  }
  .pv-in {
    position: relative;
    z-index: 2;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 10px 12px;
    min-width: 0;
    width: 100%;
  }
  .pv-glyph {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border-radius: var(--radius-md);
    color: #fff;
    background: rgba(0, 0, 0, 0.34);
    border: 1px solid rgba(255, 255, 255, 0.18);
  }
  .pv-words {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .pv-words strong {
    font-size: var(--fs-ui);
    color: #fff;
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pv-words span {
    font-size: var(--fs-small);
    color: rgba(255, 255, 255, 0.88);
    text-shadow: 0 1px 2px rgba(0, 0, 0, 0.45);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ---- sections --------------------------------------------------------- */
  section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding-top: var(--sp-1);
    border-top: 1px solid var(--border);
  }
  .sec-head {
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
  }
  h4 {
    margin: 0;
    font-size: var(--fs-ui);
    font-weight: 700;
    letter-spacing: 0.02em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .only,
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
  .sub-head {
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text-muted);
    margin-top: 2px;
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

  /* ---- layout picker ---------------------------------------------------- */
  .layouts {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--sp-2);
  }
  .lay {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 5px;
    padding: 9px 10px;
    border-radius: var(--radius-md);
    background: var(--bg-3);
    border: 1px solid transparent;
    color: var(--text-muted);
    text-align: left;
    transition:
      transform var(--dur-standard) var(--ease-spring),
      border-color var(--dur-standard) ease;
  }
  @media (pointer: fine) {
    .lay:hover {
      transform: translateY(-1px);
    }
  }
  .lay.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--text);
  }
  .lay b {
    font-size: var(--fs-compact);
    color: var(--text);
  }
  .lay em {
    font-size: var(--fs-small);
    font-style: normal;
    line-height: 1.35;
  }
  /* The diagrams: three bars in three arrangements. */
  .dia {
    display: grid;
    gap: 3px;
    width: 100%;
    height: 30px;
    padding: 3px;
    border-radius: var(--radius-sm);
    background: var(--bg-1);
  }
  .dia i {
    display: block;
    background: var(--text-faint);
    border-radius: 2px;
    opacity: 0.55;
  }
  .dia.list {
    grid-template-rows: repeat(3, 1fr);
  }
  .dia.gallery {
    grid-template-columns: repeat(3, 1fr);
    align-items: end;
  }
  .dia.gallery i {
    height: 70%;
  }
  .dia.cover {
    grid-template-columns: 1fr;
    grid-template-rows: 1fr;
  }
  .dia.cover i {
    grid-area: 1 / 1;
    opacity: 0.85;
  }
  .dia.cover i:nth-child(2) {
    align-self: end;
    height: 5px;
    margin: 0 4px 4px;
    opacity: 0.45;
  }
  .dia.cover i:nth-child(3) {
    display: none;
  }
  .lay.on .dia i {
    background: var(--accent);
    opacity: 0.85;
  }

  /* ---- art picker ------------------------------------------------------- */
  .arts {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(84px, 1fr));
    gap: 7px;
    max-height: 188px;
    overflow-y: auto;
    padding: 2px;
  }
  .art {
    position: relative;
    isolation: isolate;
    height: 50px;
    padding: 0;
    border-radius: var(--radius-sm);
    overflow: hidden;
    border: 2px solid transparent;
    background: var(--bg-3);
    color: var(--text);
    transition: transform var(--dur-standard) var(--ease-spring);
  }
  @media (pointer: fine) {
    .art:hover {
      transform: translateY(-2px);
    }
  }
  .art.on {
    border-color: var(--accent);
  }
  .art :global(.bnr) {
    position: absolute;
    inset: 0;
    z-index: 0;
  }
  .art-name {
    position: absolute;
    z-index: 2;
    left: 0;
    right: 0;
    bottom: 0;
    padding: 8px 5px 3px;
    font-size: var(--fs-tiny);
    font-weight: 600;
    color: #fff;
    /* Same guarantee as everywhere else: the label carries its own floor rather
       than hoping the tile behind it is dark. */
    background: linear-gradient(rgba(0, 0, 0, 0), rgba(0, 0, 0, 0.72) 55%, rgba(0, 0, 0, 0.82));
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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

  .actions {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-top: var(--sp-1);
  }
  .spacer {
    flex: 1;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .layouts {
      grid-template-columns: 1fr;
    }
    .lay {
      flex-direction: row;
      align-items: center;
      gap: 10px;
    }
    .dia {
      width: 44px;
      flex: none;
    }
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
    .rows,
    .arts {
      max-height: none;
      overflow-y: visible;
    }
    /* Four rows of 84px thumbnails at 393px is a lot of tiny art. Three columns
       of taller tiles reads as a picker rather than a contact sheet. */
    .arts {
      grid-template-columns: repeat(3, 1fr);
      gap: var(--sp-2);
    }
    .art {
      height: 62px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .lay,
    .art {
      transition: none;
    }
    .lay:hover,
    .art:hover {
      transform: none;
    }
  }
</style>
