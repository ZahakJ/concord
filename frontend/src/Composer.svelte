<script>
  // The message composer: draft, :emoji: and @mention autocomplete, emoji
  // picker, image attach (button/paste/drop via parent), typing signals, reply
  // banner, and ArrowUp-in-empty-composer to edit your last message.
  import Icon from "./Icon.svelte";
  import EmojiPicker from "./EmojiPicker.svelte";
  import { replaceShortcodes, activeShortcode, searchEmoji } from "./lib/emoji.js";
  import { S, activeChannel, sendMessage, react, flash } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";

  let draft = $state("");
  let composerEl = $state(null);
  let fileInput = $state(null);
  let suggest = $state(null); // { kind:"emoji"|"mention", start, items, sel }
  let lastTypingSent = 0;

  const ch = $derived(activeChannel());
  const typingLabel = $derived(
    S.typingList.length === 0
      ? ""
      : S.typingList.length === 1
        ? `${S.typingList[0].label} is typing…`
        : `${S.typingList.length} people are typing…`,
  );

  export function focus() {
    composerEl?.focus();
  }

  // ---- autocomplete (emoji + mentions share one popover) ----

  function activeMention(text, caret) {
    const upto = text.slice(0, caret);
    const at = upto.lastIndexOf("@");
    if (at < 0 || (at > 0 && /[\w@]/.test(upto[at - 1]))) return null;
    const query = upto.slice(at + 1);
    if (/\s[\s]/.test(query) || query.length > 24) return null;
    return { start: at, query };
  }

  function updateSuggest() {
    const caret = composerEl?.selectionStart ?? draft.length;
    const emoji = activeShortcode(draft, caret);
    if (emoji) {
      const items = searchEmoji(emoji.query, 8);
      suggest = items.length
        ? { kind: "emoji", start: emoji.start, items, sel: 0 }
        : null;
      return;
    }
    const mention = activeMention(draft, caret);
    if (mention) {
      const q = mention.query.toLowerCase();
      const items = S.members
        .filter((m) => !m.isSelf && m.name && m.name.toLowerCase().includes(q))
        .slice(0, 6);
      suggest = items.length ? { kind: "mention", start: mention.start, items, sel: 0 } : null;
      return;
    }
    suggest = null;
  }

  function accept(idx = null) {
    if (!suggest) return;
    const caret = composerEl?.selectionStart ?? draft.length;
    const item = suggest.items[idx ?? suggest.sel];
    const insert = suggest.kind === "emoji" ? item[1] : `@${item.name}`;
    draft = draft.slice(0, suggest.start) + insert + " " + draft.slice(caret);
    suggest = null;
    composerEl?.focus();
  }

  function editLastOwnMessage() {
    const own = [...S.messages].reverse().find(
      (m) => m.sender === S.identity.fingerprint && !m.deleted && m.kind === "",
    );
    if (own) S.editing = own;
  }

  function onKeydown(e) {
    if (suggest) {
      if (e.key === "ArrowDown" || (e.key === "Tab" && !e.shiftKey)) {
        e.preventDefault();
        suggest = { ...suggest, sel: (suggest.sel + 1) % suggest.items.length };
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        suggest = { ...suggest, sel: (suggest.sel - 1 + suggest.items.length) % suggest.items.length };
      } else if (e.key === "Enter") {
        e.preventDefault();
        accept();
      } else if (e.key === "Escape") {
        suggest = null;
      }
      return;
    }
    if (e.key === "ArrowUp" && !draft.trim()) {
      e.preventDefault();
      editLastOwnMessage();
    } else if (e.key === "Escape" && S.replyingTo) {
      S.replyingTo = null;
    }
  }

  function onInput() {
    const now = Date.now();
    if (now - lastTypingSent > 2000 && S.activeChannelId) {
      lastTypingSent = now;
      api.sendTyping(S.activeChannelId).catch(() => {});
    }
    updateSuggest();
  }

  async function send(e) {
    e?.preventDefault();
    const text = replaceShortcodes(draft.trim());
    if (!text || !S.activeChannelId) return;
    draft = "";
    suggest = null;
    const replyTo = S.replyingTo?.id || "";
    S.replyingTo = null;
    try {
      await sendMessage(text, replyTo);
    } catch (err) {
      flash(err);
    }
  }

  // ---- attachments ----

  const MAX_IMAGE_BYTES = 300 * 1024;
  export async function attachImage(file) {
    if (!file || !file.type.startsWith("image/") || !S.activeChannelId) {
      if (file && !file.type.startsWith("image/")) flash("Only images for now");
      return;
    }
    if (file.size > MAX_IMAGE_BYTES) {
      flash("Image too large (max 300 KB for now)");
      return;
    }
    const dataUrl = await new Promise((res, rej) => {
      const r = new FileReader();
      r.onload = () => res(r.result);
      r.onerror = rej;
      r.readAsDataURL(file);
    });
    try {
      await sendMessage(`![image](${dataUrl})`, "");
    } catch (err) {
      flash(err);
    }
  }

  function onPaste(e) {
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (item) {
      e.preventDefault();
      attachImage(item.getAsFile());
    }
  }

  function pickEmoji(e) {
    if (S.pickerTarget === "composer") {
      draft += e;
      composerEl?.focus();
    } else if (S.pickerTarget) {
      react(S.pickerTarget, e);
    }
    S.pickerTarget = null;
  }
</script>

{#if S.replyingTo}
  <div class="reply-banner">
    <span class="muted">
      Replying to <strong>{S.replyingTo.senderName || S.replyingTo.sender.slice(0, 9)}</strong>
    </span>
    <button class="mini" aria-label="Cancel reply" onclick={() => (S.replyingTo = null)}>
      <Icon name="close" size={11} />
    </button>
  </div>
{/if}
<div class="typing-line muted">{typingLabel}</div>

<div class="composer-wrap">
  {#if suggest}
    <div class="suggest-pop">
      {#each suggest.items as item, i (suggest.kind === "emoji" ? item[0] : item.fingerprint)}
        <button class="suggest-item" class:sel={i === suggest.sel} onclick={() => accept(i)}>
          {#if suggest.kind === "emoji"}
            <span class="s-emoji">{item[1]}</span> :{item[0]}:
          {:else}
            <span class="s-emoji">@</span>{item.name}
          {/if}
        </button>
      {/each}
    </div>
  {/if}
  {#if S.pickerTarget}
    <EmojiPicker onPick={pickEmoji} onClose={() => (S.pickerTarget = null)} />
  {/if}

  <form class="composer" onsubmit={send}>
    <input
      type="file"
      accept="image/*"
      bind:this={fileInput}
      style="display:none"
      onchange={(e) => {
        attachImage(e.target.files?.[0]);
        e.target.value = "";
      }}
    />
    <button
      type="button"
      class="ghost iconbtn"
      title="Attach image (or paste / drop one)"
      aria-label="Attach image"
      disabled={!ch}
      onclick={() => fileInput.click()}
    >
      <Icon name="attach" />
    </button>
    <input
      bind:this={composerEl}
      placeholder={ch ? `Message #${ch.name} — try :fire: or @name` : "Select a channel"}
      bind:value={draft}
      disabled={!ch}
      oninput={onInput}
      onkeydown={onKeydown}
      onpaste={onPaste}
    />
    <button
      type="button"
      class="ghost iconbtn"
      title="Emoji"
      aria-label="Emoji picker"
      disabled={!ch}
      onclick={() => (S.pickerTarget = S.pickerTarget === "composer" ? null : "composer")}
    >
      <Icon name="smile" />
    </button>
    <button type="submit" disabled={!draft.trim()}>Send</button>
  </form>
</div>

<style>
  .reply-banner {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 16px;
    font-size: 13px;
    border-top: 1px solid var(--border);
  }
  .typing-line {
    height: 18px;
    font-size: 12px;
    font-style: italic;
    padding: 2px 16px 0;
    border-top: 1px solid var(--border);
  }
  .composer-wrap {
    position: relative;
  }
  .suggest-pop {
    position: absolute;
    bottom: 54px;
    left: 60px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 4px;
    display: flex;
    flex-direction: column;
    min-width: 220px;
    box-shadow: var(--shadow-pop);
    z-index: 50;
  }
  .suggest-item {
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    font-size: 13px;
    font-family: ui-monospace, monospace;
  }
  .suggest-item.sel,
  .suggest-item:hover {
    background: var(--bg-3);
  }
  .s-emoji {
    font-size: 16px;
    margin-right: 6px;
  }
  .composer {
    display: flex;
    gap: 8px;
    padding: 0 16px 14px;
  }
  .iconbtn {
    display: grid;
    place-items: center;
    padding: 0 11px;
  }
  .mini {
    padding: 2px 6px;
    background: transparent;
    color: var(--text-muted);
    display: grid;
    place-items: center;
  }
  .mini:hover {
    background: var(--bg-3);
    color: var(--text);
  }
</style>
