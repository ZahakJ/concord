<script>
  // The advanced composer: a full writing workspace for the times the one-line
  // box isn't enough. Everything it produces is an ordinary message — markdown,
  // an optional rich-embed token, optional attachments — so it sends like any
  // other message and needs no protocol support.
  //
  // The editor itself lives in RichEditor.svelte, shared with the forum-post
  // composer. What is HERE is only what makes this a *message*:
  //   · where it's going, and under what rules (channel, disappearing timer)
  //   · the rich-embed builder
  //   · send vs. save-an-edit
  //   · a close that cannot silently eat a long draft
  import { tick } from "svelte";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import RichEditor from "./RichEditor.svelte";
  import EmbedView from "../EmbedView.svelte";
  import { S, activeChannel, activeGuild, customEmojiMap, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { COLOR_NAMES } from "../lib/markdown.js";
  import { encodeEmbed, parseEmbed, stripEmbedToken } from "../lib/richembed.js";
  import { stampEphemeral, stripEphemeral, channelTTL, ttlLabel, EPH_RE } from "../lib/ephemeral.svelte.js";
  import { bodyStats, saveDraft, loadDraft, clearDraft, draftAge } from "../lib/postdraft.js";

  // editId set ⇒ editing an existing message rather than composing: `initial` is
  // that message's raw content, decoded back into the editor and the embed
  // builder, and saved with api.editMessage.
  let { onClose, onSent, initial = "", editId = "" } = $props();

  const seededEmbed = parseEmbed(initial);
  // Keep the original disappearing-message token verbatim so an edit preserves
  // the message's own expiry instead of resetting it to the channel default.
  const ephToken = initial.match(EPH_RE)?.[0] || "";
  const seedBody = stripEphemeral(stripEmbedToken(initial));

  const EMPTY_EMBED = () => ({ color: "#14a394", title: "", desc: "", fields: [] });

  let body = $state(seedBody);
  let pending = $state([]);
  let mode = $state("split");
  // Focus mode is the editor's, but this dialog has to answer to it: see the
  // embed panel below.
  let zen = $state(false);
  let busy = $state(false);
  // "" | "close" — the guarded-close bar. See requestClose.
  let guard = $state("");
  let restored = $state(""); // "3 minutes ago", when a draft came back

  let embedOn = $state(!!seededEmbed);
  let embed = $state(seededEmbed ? { ...EMPTY_EMBED(), ...seededEmbed, color: seededEmbed.color || "#14a394" } : EMPTY_EMBED());

  const ch = $derived(activeChannel());
  const guild = $derived(activeGuild());
  const cemoji = $derived(customEmojiMap());
  const ttl = $derived(S.activeChannelId ? channelTTL(S.activeChannelId) : 0);
  // Offline is a real state and it is NOT an error: a message written with no
  // peers connected is stored and gossiped when the link comes back. Say so
  // rather than blocking, and rather than pretending it went out instantly.
  const offline = $derived(!!S.netStatus && S.netStatus.peers === 0 && S.netStatus.hasBootstrap);

  const embedFilled = $derived(
    embedOn && !!(embed.title.trim() || embed.desc.trim() || embed.fields.some((f) => f.name.trim() || f.value.trim())),
  );
  const previewEmbed = $derived(embedFilled ? embed : null);
  const stats = $derived(bodyStats(body));
  const canPost = $derived(!busy && !!S.activeChannelId && (!!body.trim() || !!previewEmbed || pending.length > 0));

  // ---- drafts --------------------------------------------------------------
  // Rescue only, and only for a NEW message: an edit already has its content
  // safe in the message itself, so persisting an edit draft would mean offering
  // to restore stale text over a message that has since changed.
  const scope = $derived(editId ? "" : S.activeChannelId ? `channel:${S.activeChannelId}` : "");

  // Restore only when the seed is empty — the seed is the inline composer's own
  // draft, and that is always the more recent of the two. Once only: if the
  // active channel changed under an open dialog, re-running this would overwrite
  // whatever is being typed right now.
  let restoreDone = false;
  $effect(() => {
    const s = scope;
    if (restoreDone || !s || seedBody || seededEmbed) return;
    restoreDone = true;
    const d = loadDraft(s);
    if (!d) return;
    body = d.body;
    if (d.embed) {
      embed = { ...EMPTY_EMBED(), ...d.embed };
      embedOn = true;
    }
    restored = draftAge(d.at);
  });

  function persist() {
    if (scope) saveDraft(scope, { body, embed: embedOn ? embed : null });
  }

  // ---- embed builder -------------------------------------------------------
  // The panel opens BELOW the workspace, and in a dialog tall enough to scroll
  // "it opened" has to mean "you can see it". Instant, not smooth: this is a
  // reveal, not a journey, and a smooth scroll here is motion that explains
  // nothing.
  let ebEl = $state(null);
  $effect(() => {
    if (embedOn && ebEl) ebEl.scrollIntoView({ block: "nearest" });
  });
  const PALETTE = Object.entries(COLOR_NAMES);
  const MAX_FIELDS = 8; // mirrors lib/richembed.js, which truncates at 8

  function addField() {
    if (embed.fields.length < MAX_FIELDS) {
      embed.fields = [...embed.fields, { name: "", value: "" }];
      persist();
    }
  }
  function removeField(i) {
    embed.fields = embed.fields.filter((_, j) => j !== i);
    persist();
  }
  function moveField(from, to) {
    if (to < 0 || to >= embed.fields.length || from === to) return;
    const next = [...embed.fields];
    next.splice(to, 0, next.splice(from, 1)[0]);
    embed.fields = next;
    persist();
  }

  // Reorder by dragging the handle — and by ArrowUp/ArrowDown while the handle
  // has focus, because a drag-only affordance is unreachable from a keyboard.
  let dragFrom = $state(-1);
  function onHandleKey(i, e) {
    const dir = e.key === "ArrowUp" ? -1 : e.key === "ArrowDown" ? 1 : 0;
    if (!dir) return;
    e.preventDefault();
    moveField(i, i + dir);
    // Follow the row that moved, so a second press keeps going.
    const next = i + dir;
    tick().then(() => document.querySelector(`[data-fh="${next}"]`)?.focus());
  }

  // ---- close ---------------------------------------------------------------
  // Rule: Escape must never silently throw away real writing. Two mechanisms,
  // because they answer different questions —
  //   · every keystroke is persisted (new messages), so even a crash is survivable
  //   · a close with SUBSTANTIAL unsaved work asks first
  // "Substantial" is deliberately not "any character": a dialog that argues
  // about three typed characters trains people to click through it. Staged
  // attachments always count, because those are the one thing localStorage
  // cannot hold (a base64 image would blow the quota for every other draft).
  // Compare embeds by the token they'd encode to — the same string the message
  // would carry — rather than by "is one present", so tweaking a field counts.
  const embedToken = $derived(previewEmbed ? encodeEmbed(previewEmbed) : "");
  const seedToken = seededEmbed ? encodeEmbed(seededEmbed) : "";
  const dirty = $derived(body !== seedBody || embedToken !== seedToken || pending.length > 0);
  // Losing a typed EDIT silently is worse than one extra click, so an edit
  // confirms on any change. A new message uses a threshold: a dialog that argues
  // about three typed characters just teaches people to click through it.
  const heavy = $derived(
    editId ? dirty : pending.length > 0 || stats.words >= 12 || (!!embedToken && embedToken !== seedToken),
  );

  function requestClose() {
    // The editor swallows Escape for its own layers (popover, emoji picker,
    // focus mode) via a capture-phase handler, so by the time this runs the
    // author really does mean "close this".
    if (dirty && heavy && !guard) {
      guard = "close";
      return;
    }
    if (!editId) persist();
    onClose();
  }
  function discardAndClose() {
    if (scope) clearDraft(scope);
    onClose();
  }
  function keepAndClose() {
    persist();
    // flash BEFORE onClose, not after. App.svelte reads S.modal.editId to build
    // this component's props; nulling S.modal and then touching reactive state
    // in the same tick re-evaluates that prop expression against the null and
    // throws (verified in a real browser — it was the one page error left).
    if (scope) flash("Draft kept — reopen the composer to pick it up", "info");
    onClose();
  }

  // ---- send ---------------------------------------------------------------
  async function post() {
    if (!canPost) return;
    busy = true;
    const chId = S.activeChannelId;
    try {
      let content = body.trim();
      if (previewEmbed) {
        const token = encodeEmbed(previewEmbed);
        content = content ? `${content}\n${token}` : token;
      }
      if (editId) {
        await api.editMessage(chId, editId, ephToken + content);
      } else {
        // Attachments first, then the text — so a picture sits above its caption
        // in the feed, exactly like the inline composer.
        for (const a of pending) {
          if (a.isImage) await api.sendAttachment(chId, a.dataUrl, a.w, a.h, "", !!a.spoiler, a.name || "", a.desc || "");
          else await api.sendFile(chId, a.dataUrl, a.name, "");
        }
        pending = [];
        // Same stamping as the one-line composer: in a disappearing-messages
        // channel this message must expire like any other.
        if (content) await api.sendMessage(chId, stampEphemeral(chId, content), "");
      }
      if (scope) clearDraft(scope);
      onSent?.();
      onClose();
    } catch (err) {
      // Whatever failed, the text is still in the box and the draft is still on
      // disk — a retry can't lose it, and attachments already sent are gone from
      // the tray so it can't double-post them either.
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title={editId ? "Edit message" : "Advanced composer"} size="xl" onClose={requestClose}>
  <div class="ac">
    <!-- Where this is going, and under what rules. In a dialog you've navigated
         away from the channel header, and a disappearing-messages timer that
         silently applies is exactly the kind of thing a composer owes you. -->
    <div class="ctx">
      <span class="dest">
        {#if editId}
          <Icon name="edit" size={13} />
          Editing your message in <strong>#{ch?.name || "this channel"}</strong>
        {:else}
          <strong>#{ch?.name || "no channel"}</strong>
          {#if guild?.name}<span class="in">in {guild.name}</span>{/if}
        {/if}
      </span>
      {#if ttl > 0}
        <span class="badge eph"><Icon name="clock" size={12} /> Disappears after {ttlLabel(ttl)}</span>
      {/if}
      {#if offline}
        <span class="badge off"><Icon name="alert" size={12} /> Offline — it'll go out when you reconnect</span>
      {/if}
      {#if restored}
        <span class="badge draft">
          <Icon name="check" size={12} /> Draft from {restored}
          <button
            type="button"
            onclick={() => {
              body = "";
              embed = EMPTY_EMBED();
              embedOn = false;
              restored = "";
              if (scope) clearDraft(scope);
            }}>Start over</button>
        </span>
      {/if}
    </div>

    <!-- minHeight: the workspace yields 50px to a panel the author just opened,
         rather than the dialog growing a scrollbar the moment they open it.
         attachments={!editId}: api.editMessage replaces content only — there is
         no way to add an attachment to an existing message, so offering the tray
         while editing would look like it worked and then quietly drop the file. -->
    <RichEditor
      bind:body
      bind:pending
      bind:mode
      bind:zen
      autofocus
      minHeight={embedOn && !zen ? 150 : 200}
      attachments={!editId}
      placeholder="Write your message…"
      hint="Select text and use the toolbar, or type markdown directly: **bold**, *italic*, ||spoiler||, `code`, > quote, - list, ## heading. :shortcodes: and @mentions autocomplete."
      hintKey="compose-md"
      previewExtraFilled={!!previewEmbed}
      onSubmit={post}
      onInput={persist}
      submitHint={editId ? "⌘/Ctrl + ↵ to save" : "⌘/Ctrl + ↵ to send"}>
      {#snippet toolbarExtra()}
        <span class="sep" aria-hidden="true"></span>
        <button
          type="button"
          class="embedbtn"
          class:on={embedOn}
          aria-pressed={embedOn}
          title={embedOn ? "Remove the rich embed" : "Build a rich embed card"}
          onclick={() => {
            embedOn = !embedOn;
            persist();
          }}>
          <Icon name={embedOn ? "close" : "diamond"} size={13} />
          <span class="lbl">Embed</span>
        </button>
      {/snippet}
      {#snippet previewExtra()}
        {#if previewEmbed}<EmbedView embed={previewEmbed} customEmoji={cemoji} />{/if}
      {/snippet}
    </RichEditor>

    {#if embedOn && !zen}
      <!-- The embed builder is the one panel that appears rather than being
           always present: most messages don't carry a card, and an empty
           six-field form is the definition of a wall of controls. It also stands
           down in focus mode; the card it builds is still in the message, it is
           just not on screen while you write. -->
      <section class="eb" bind:this={ebEl}>
        <header>
          <Icon name="diamond" size={13} />
          <h4>Rich embed</h4>
          <span class="ebhint">A card under your message. It syncs like any message — older peers see it too.</span>
        </header>
        <div class="row">
          <div class="accent" role="group" aria-label="Accent colour">
            {#each PALETTE as [name, hex] (name)}
              <button
                type="button"
                class="acc"
                class:on={embed.color === hex}
                style="--acc:{hex}"
                title={name}
                aria-label={`Accent ${name}`}
                onclick={() => {
                  embed.color = hex;
                  persist();
                }}></button>
            {/each}
            <label class="acccustom" title="Custom accent colour">
              <input type="color" bind:value={embed.color} oninput={persist} aria-label="Custom accent colour" />
            </label>
          </div>
        </div>
        <input dir="auto" class="etitle" maxlength="200" placeholder="Embed title" bind:value={embed.title} oninput={persist} />
        <textarea class="edesc" rows="2" maxlength="2000" placeholder="Description — markdown works here too" bind:value={embed.desc} oninput={persist}
        ></textarea>
        {#if embed.fields.length}
          <ul class="fields">
            <!-- Keyed by index: field text is free-form, so a content-derived key
                 can collide and Svelte rejects that at render time — a crash an
                 author could cause by typing the same thing twice. -->
            {#each embed.fields as f, i (i)}
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <li class="frow" class:dragging={dragFrom === i} ondragover={(e) => e.preventDefault()} ondrop={() => (dragFrom = -1)}>
                <button
                  type="button"
                  class="handle"
                  data-fh={i}
                  draggable="true"
                  title="Drag to reorder — or use ↑ / ↓"
                  aria-label={`Reorder field ${i + 1} of ${embed.fields.length}`}
                  ondragstart={() => (dragFrom = i)}
                  ondragend={() => (dragFrom = -1)}
                  ondragenter={() => {
                    if (dragFrom >= 0 && dragFrom !== i) {
                      moveField(dragFrom, i);
                      dragFrom = i;
                    }
                  }}
                  onkeydown={(e) => onHandleKey(i, e)}><Icon name="menu" size={12} /></button>
                <input dir="auto" class="fname" maxlength="100" placeholder="Field name" bind:value={f.name} oninput={persist} />
                <input dir="auto" class="fval" maxlength="400" placeholder="Field value" bind:value={f.value} oninput={persist} />
                {#if S.isMobile}
                  <!-- Android's WebView synthesises no HTML5 drag events from
                       touch and a phone has no arrow keys, so the handle above
                       responds to nothing there — with eight fields the only way
                       to fix an ordering mistake was to delete and retype every
                       row below it. Same moveField() the keyboard path uses. -->
                  <button
                    type="button"
                    class="fmove"
                    aria-label={`Move field ${i + 1} up`}
                    disabled={i === 0}
                    onclick={() => moveField(i, i - 1)}><Icon name="chevron" size={14} /></button>
                  <button
                    type="button"
                    class="fmove down"
                    aria-label={`Move field ${i + 1} down`}
                    disabled={i === embed.fields.length - 1}
                    onclick={() => moveField(i, i + 1)}><Icon name="chevron" size={14} /></button>
                {/if}
                <button type="button" class="fx" aria-label={`Remove field ${i + 1}`} title="Remove field" onclick={() => removeField(i)}>
                  <Icon name="close" size={12} />
                </button>
              </li>
            {/each}
          </ul>
        {/if}
        {#if embed.fields.length < MAX_FIELDS}
          <button type="button" class="addfield" onclick={addField}>
            <Icon name="plus" size={12} /> Add field
            <span class="count">{embed.fields.length}/{MAX_FIELDS}</span>
          </button>
        {/if}
      </section>
    {/if}

    {#if guard === "close"}
      <!-- The designed close. Never a silent discard, and never a dead end: both
           outcomes are one click, and the non-destructive one is the default. -->
      <div class="guard" role="group" aria-live="polite" aria-label="Unsaved work">
        <p>
          <Icon name="alert" size={15} />
          {#if editId}
            Discard your changes to this message?
          {:else if pending.length}
            You have {pending.length} attachment{pending.length === 1 ? "" : "s"} staged — those can't be saved with a draft.
          {:else}
            {stats.words} words unsent.
          {/if}
        </p>
        <button type="button" class="ghost" onclick={() => (guard = "")}>Keep writing</button>
        {#if editId}
          <button type="button" class="danger" onclick={onClose}>Discard changes</button>
        {:else}
          <button type="button" class="ghost danger" onclick={discardAndClose}>Discard</button>
          <button type="button" onclick={keepAndClose}>Save for later</button>
        {/if}
      </div>
    {:else}
      <div class="actions">
        <button type="button" class="ghost" onclick={requestClose}>Cancel</button>
        <button type="button" class="send" onclick={post} disabled={!canPost}>
          {#if busy}
            <span class="spin" aria-hidden="true"></span> {editId ? "Saving…" : "Sending…"}
          {:else}
            <Icon name={editId ? "check" : "send"} size={14} />
            {editId ? "Save changes" : "Send message"}
          {/if}
        </button>
      </div>
    {/if}
  </div>
</Modal>

<style>
  .ac {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    text-align: left;
  }
  /* See the note on RichEditor's .rx: on the phone the dialog is an auto-height
     sheet, and flex negotiation there either crushes these blocks into each
     other or balloons them. Natural heights + the sheet's own scroll. */
  @media (pointer: coarse), (max-width: 768px) {
    .ac {
      flex: none;
      min-height: auto;
    }
  }

  /* ---- context bar ------------------------------------------------------ */
  .ctx {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--sp-2);
    font-size: var(--fs-compact);
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
  .in {
    color: var(--text-faint);
  }
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 3px 9px;
    font-size: var(--fs-small);
    border-radius: 999px;
    background: var(--bg-3);
  }
  .badge.eph {
    color: var(--accent-hover);
    background: var(--accent-soft);
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
    font-size: var(--fs-small);
    font-weight: 600;
    color: inherit;
    background: none;
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  /* The embed toggle rides in the editor's own toolbar, so it reads as one of
     the insert tools rather than a separate feature bolted underneath. */
  .embedbtn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 30px;
    padding: 0 9px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-muted);
    background: transparent;
    border-radius: var(--radius-sm);
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  .embedbtn:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .embedbtn.on {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .sep {
    width: 1px;
    align-self: stretch;
    margin: 2px 4px;
    background: var(--border);
  }

  /* ---- embed builder ---------------------------------------------------- */
  .eb {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    /* Sized so the panel and the editor's 200px floor BOTH fit the xl dialog at
       a laptop height — otherwise opening the builder pushes the workspace past
       the dialog and the whole thing starts scrolling under you mid-sentence. */
    max-height: clamp(150px, 24 * var(--vh), 240px);
    /* min-height:0 with flex-shrink:0 is not a contradiction, it is the fix for
       a runaway: WITHOUT it this panel contributes its full unclamped content
       height to the column's min-content, which makes the column taller than the
       dialog, and .rx (flex-grow:1) then eats the surplus — so adding a field
       made the EDITOR grow and pushed the panel off-screen. min-height:0 stops
       the contribution; flex-shrink:0 still keeps the panel at its own height. */
    min-height: 0;
    overflow-y: auto;
    /* Both flex-shrink:0 declarations are load-bearing. A flex column with a
       max-height SHRINKS its children to fit before it will scroll — which
       squeezed the description box to a 20px slot and stacked the field rows on
       top of it. Pinned children make the panel scroll like a panel should, and
       pinning the panel itself stops the workspace stealing its height. */
    flex-shrink: 0;
    padding: var(--sp-3);
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    /* Appears in place; nothing above it moves except downward, which is the
       honest direction for a panel that opened. */
    animation: eb-in 0.18s var(--ease-out);
  }
  .eb > * {
    flex-shrink: 0;
  }
  @keyframes eb-in {
    from {
      opacity: 0;
      transform: translateY(-6px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .eb {
      animation: none;
    }
  }
  .eb header {
    display: flex;
    align-items: center;
    gap: 7px;
  }
  .eb header :global(svg) {
    color: var(--accent-hover);
  }
  .eb h4 {
    margin: 0;
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text);
  }
  .ebhint {
    font-size: var(--fs-small);
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .accent {
    display: flex;
    align-items: center;
    gap: 5px;
    flex-wrap: wrap;
  }
  /* Descendant selector: the mobile sheet's 44px button floor would stretch a
     18px dot into a lozenge. */
  .accent .acc {
    width: 18px;
    height: 18px;
    min-height: 18px;
    padding: 0;
    border-radius: 50%;
    background: var(--acc);
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.28);
    transition: transform var(--dur-quick) ease;
  }
  .accent .acc:hover {
    transform: scale(1.18);
  }
  .accent .acc.on {
    box-shadow:
      inset 0 0 0 1px rgba(0, 0, 0, 0.28),
      0 0 0 2px var(--bg-1),
      0 0 0 3.5px var(--text);
  }
  .accent .acccustom input[type="color"] {
    width: 22px;
    height: 22px;
    min-height: 22px;
    padding: 0;
    margin-left: var(--sp-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: none;
    cursor: pointer;
  }
  .etitle {
    font-weight: 600;
  }
  .edesc {
    width: 100%;
    resize: vertical;
    font-family: inherit;
  }
  .fields {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .frow {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .frow.dragging {
    opacity: 0.5;
  }
  .frow .handle {
    flex-shrink: 0;
    width: 22px;
    height: 26px;
    min-height: 26px;
    display: grid;
    place-items: center;
    padding: 0;
    color: var(--text-faint);
    background: transparent;
    border-radius: var(--radius-sm);
    cursor: grab;
  }
  .frow .handle:hover,
  .frow .handle:focus-visible {
    color: var(--text);
    background: var(--bg-3);
  }
  .fname {
    width: 32%;
    min-width: 0;
  }
  .fval {
    flex: 1;
    min-width: 0;
  }
  /* Desktop never renders these — the handle drags and ArrowUp/Down reorders. */
  .fmove {
    flex-shrink: 0;
    width: var(--tap-min);
    height: var(--tap-min);
    display: grid;
    place-items: center;
    padding: 0;
    border-radius: 50%;
    color: var(--text-muted);
    background: transparent;
  }
  .fmove :global(svg) {
    transform: rotate(90deg); /* the chevron points right at rest */
  }
  .fmove.down :global(svg) {
    transform: rotate(-90deg);
  }
  .fmove:disabled {
    opacity: 0.3;
  }
  /* On a phone the four controls plus two inputs cannot share 320px, so the
     row becomes name / value / a control strip beneath them. */
  @media (pointer: coarse) {
    .frow {
      flex-wrap: wrap;
      row-gap: 6px;
    }
    .frow .handle {
      display: none; /* drag is unreachable here; the buttons replace it */
    }
    .fname,
    .fval {
      order: 1;
      width: auto;
      flex: 1 1 100%;
    }
    .fval {
      order: 2;
    }
    .fmove {
      order: 3;
    }
    .fmove:not(.down) {
      margin-left: auto; /* the strip sits right, under the value it edits */
    }
    .frow .fx {
      order: 4;
      width: var(--tap-min);
      height: var(--tap-min);
      min-height: var(--tap-min);
    }
  }
  .frow .fx {
    flex-shrink: 0;
    width: 26px;
    height: 26px;
    min-height: 26px;
    display: grid;
    place-items: center;
    padding: 0;
    border-radius: 50%;
    color: var(--text-muted);
    background: transparent;
  }
  .frow .fx:hover {
    color: var(--danger-text);
    background: var(--danger-soft);
  }
  .addfield {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    font-size: 12px;
    font-weight: 600;
    color: var(--accent-hover);
    background: transparent;
    border: 1px dashed color-mix(in srgb, var(--accent) 45%, var(--border));
    border-radius: var(--radius-sm);
  }
  .addfield:hover {
    background: var(--accent-soft);
  }
  .count {
    color: var(--text-faint);
    font-weight: 500;
    font-variant-numeric: tabular-nums;
  }

  /* ---- actions / guard -------------------------------------------------- */
  .actions,
  .guard {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--sp-2);
    padding-top: var(--sp-3);
    border-top: 1px solid var(--border);
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
    font-size: var(--fs-ui);
    color: var(--text);
  }
  .guard .danger {
    color: var(--danger-text);
  }
  .guard button.danger:not(.ghost) {
    color: var(--danger-fg);
    background: var(--danger);
  }
  .send {
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

  @media (pointer: coarse), (max-width: 768px) {
    .eb {
      max-height: none;
    }
    .fname {
      width: 100%;
    }
    .frow {
      flex-wrap: wrap;
    }
    /* The guard is a decision, not a row of chrome: stack it so both buttons are
       full-width thumb targets instead of three cramped ones. */
    .guard {
      flex-wrap: wrap;
    }
    .guard p {
      width: 100%;
      margin: 0;
    }
    .guard button {
      flex: 1;
    }
  }
</style>
