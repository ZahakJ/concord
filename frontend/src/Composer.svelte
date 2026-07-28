<script>
  // The message composer: draft, :emoji: / @mention / slash-command
  // autocomplete, emoji picker, image attach (button/paste/drop via parent),
  // typing signals, reply banner, and ArrowUp-in-empty-composer to edit your
  // last message.
  import Icon from "./Icon.svelte";
  import EmojiPicker from "./EmojiPicker.svelte";
  import { untrack } from "svelte";
  import { replaceShortcodes, activeShortcode, searchEmoji } from "./lib/emoji.js";
  import { S, activeChannel, activeGuild, sendMessage, react, flash, nameColorFor } from "./lib/state.svelte.js";

  import { PERM, has } from "./lib/perms.js";
  import { api } from "./lib/api.js";
  import { scheduleMessage } from "./lib/scheduled.svelte.js";
  import { stampEphemeral, channelTTL, ttlLabel } from "./lib/ephemeral.svelte.js";

  let draft = $state("");
  let uploading = $state(0); // files being read into `pending` (brief)
  // Staged attachments: pasting/dropping/picking a file adds a PREVIEW to the
  // composer (Discord-style), sent together with the text on submit.
  // Each: { id, dataUrl, w, h, name, isImage }
  let pending = $state([]);
  let composerEl = $state(null);
  let fileInput = $state(null);
  let suggest = $state(null); // { kind:"emoji"|"mention", start, items, sel }
  let lastTypingSent = 0;

  // A composer placeholder that reads like the conversation you're in — never
  // the internal "#dm" channel name.
  const composerPlaceholder = $derived.by(() => {
    if (!ch) return "Select a channel";
    const g = activeGuild();
    if (g?.dmNotes) return "Write a note to yourself…";
    if (g?.kind === "dm") return `Message ${g.name || "your friend"}`;
    return `Message #${ch.name}`;
  });

  const ch = $derived(activeChannel());
  // Touch layout: hide the desktop formatting toolbar (it can't hover-reveal on
  // touch and just eats a row), send with an explicit button instead of Enter
  // (Enter is a newline on a phone keyboard), and roomier tap targets.
  // `mobile` drives LAYOUT (S.isMobile also matches a narrow desktop window);
  // Enter behavior keys off actual pointer coarseness so a physical keyboard
  // in a narrow window keeps Enter-to-send.
  const mobile = $derived(S.isMobile);
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;
  const canSend = $derived((!!draft.trim() || pending.length > 0) && !!ch);
  // Disappearing-messages timer for this channel (0 = off). channelTTL reads the
  // reactive per-channel store, so this updates the moment you change it.
  const ephTTL = $derived(ch ? channelTTL(S.activeChannelId) : 0);
  // Mobile markdown lives behind a toggle rather than an always-on toolbar row.
  let showFmt = $state(false);
  const showFmtBar = $derived(!mobile || showFmt);

  export function focus() {
    composerEl?.focus();
  }

  // ---- per-channel drafts (survive reloads + channel switches) ----
  const draftKey = (id) => `concord.draft.${id}`;
  function saveDraft(id, text) {
    if (!id) return;
    try {
      if (text) localStorage.setItem(draftKey(id), text);
      else localStorage.removeItem(draftKey(id));
    } catch {
      /* storage blocked */
    }
  }
  function loadDraft(id) {
    try {
      return (id && localStorage.getItem(draftKey(id))) || "";
    } catch {
      return "";
    }
  }

  let prevChannel = S.activeChannelId;
  draft = loadDraft(prevChannel);
  // On channel switch: stash the outgoing draft, restore the incoming one.
  $effect(() => {
    const cur = S.activeChannelId;
    untrack(() => {
      if (cur === prevChannel) return;
      saveDraft(prevChannel, draft);
      draft = loadDraft(cur);
      prevChannel = cur;
      queueAutosize();
    });
  });

  // Size to a restored draft once the textarea mounts.
  $effect(() => {
    if (composerEl) queueAutosize();
  });

  // ---- auto-growing textarea ----
  function autosize() {
    if (!composerEl) return;
    composerEl.style.height = "auto";
    composerEl.style.height = Math.min(composerEl.scrollHeight, 200) + "px";
  }
  const queueAutosize = () => requestAnimationFrame(autosize);

  // ---- slash commands (client-side text expansion) ----
  // One registry drives both the "/" autocomplete menu and applySlash(), so
  // the menu can never drift out of sync with what actually expands. `args`
  // controls whether accepting a command leaves the caret after "/cmd ".
  const kaomoji = (face) => (rest) => (rest ? rest + " " : "") + face;
  const SLASH_COMMANDS = [
    { name: "shrug", usage: "/shrug [message]", desc: "Appends ¯\\_(ツ)_/¯", args: true, expand: kaomoji("¯\\_(ツ)_/¯") },
    { name: "tableflip", usage: "/tableflip [message]", desc: "Appends (╯°□°)╯︵ ┻━┻", args: true, expand: kaomoji("(╯°□°)╯︵ ┻━┻") },
    { name: "unflip", usage: "/unflip [message]", desc: "Appends ┬─┬ ノ( ゜-゜ノ)", args: true, expand: kaomoji("┬─┬ ノ( ゜-゜ノ)") },
    // House joke. Mirror-symmetric on purpose: the right half is the standard
    // flip, the left half is that same flip reflected, and "fa me" sits exactly
    // between them — two people flipping tables away from the phrase.
    { name: "fa", usage: "/fa [message]", desc: "┻━┻ ︵╰(°□°╰) fa me (╯°□°)╯︵ ┻━┻", args: true, expand: kaomoji("┻━┻ ︵╰(°□°╰) fa me (╯°□°)╯︵ ┻━┻") },
    { name: "me", usage: "/me <action>", desc: "Italicized action text", args: true, expand: (rest, text) => (rest ? `*${rest}*` : text) },
    { name: "spoiler", usage: "/spoiler <text>", desc: "Hides text until clicked", args: true, expand: (rest, text) => (rest ? `||${rest}||` : text) },
    // An ACTION, not a text expansion: it runs instead of sending (see runAction).
    { name: "clear", usage: "/clear <n>", desc: "Delete the last n messages (moderators)", args: true, mod: true, expand: (_, text) => text },
  ];

  const canModerate = $derived(has(activeGuild()?.myPerms || 0, PERM.MANAGE_MESSAGES));
  const slashCommands = $derived(SLASH_COMMANDS.filter((c) => !c.mod || canModerate));

  // Action commands do something instead of sending text. Returns true if the
  // draft was consumed.
  async function runAction(text) {
    const m = text.match(/^\/clear(?:\s+(\d+))?\s*$/i);
    if (!m) return false;
    if (!canModerate) {
      flash("You need the Manage messages permission to clear messages");
      return true;
    }
    const n = parseInt(m[1] || "", 10);
    if (!n) {
      flash("How many? e.g. /clear 10");
      return true;
    }
    try {
      const cleared = await api.purgeMessages(S.activeChannelId, n);
      flash(`Cleared ${cleared} message${cleared === 1 ? "" : "s"}`, "success");
    } catch (err) {
      flash(err);
    }
    return true;
  }

  function applySlash(text) {
    const m = text.match(/^\/(\w+)(?:\s+([\s\S]*))?$/);
    const cmd = m && SLASH_COMMANDS.find((c) => c.name === m[1]);
    return cmd ? cmd.expand((m[2] || "").trim(), text) : text;
  }

  // ---- autocomplete (emoji + mentions + slash commands share one popover) ----

  // Only while the caret is still inside the first "/word" token — once a
  // space is typed the command is committed and the menu stays out of the way.
  function activeSlash(text, caret) {
    const m = text.match(/^\/(\w*)/);
    if (!m || caret < 1 || caret > m[0].length) return null;
    return { query: text.slice(1, caret).toLowerCase() };
  }

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
    const slash = activeSlash(draft, caret);
    if (slash) {
      const items = slashCommands.filter((c) => c.name.startsWith(slash.query));
      suggest = items.length ? { kind: "slash", start: 0, items, sel: 0 } : null;
      return;
    }
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
    const insert =
      suggest.kind === "emoji"
        ? item[1] + " "
        : suggest.kind === "mention"
          ? `@${item.name} `
          : `/${item.name}` + (item.args ? " " : "");
    const pos = suggest.start + insert.length;
    draft = draft.slice(0, suggest.start) + insert + draft.slice(caret);
    suggest = null;
    composerEl?.focus();
    queueAutosize();
    // Land the caret right after what we inserted (e.g. after "/spoiler ") so
    // the user can keep typing the args — bind:value alone parks it at the end.
    requestAnimationFrame(() => composerEl?.setSelectionRange(pos, pos));
  }

  // ---- markdown formatting (toolbar buttons + Ctrl/Cmd shortcuts) ----
  // Wraps the current selection with markers, or inserts a selected
  // placeholder when nothing is selected. Mirrors accept(): mutate `draft`,
  // refocus, then place the selection on the next frame because bind:value
  // alone would park the caret at the end.

  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent || "");
  const MOD_LABEL = isMac ? "⌘" : "Ctrl+";
  const WRAPS = {
    bold: { pre: "**", post: "**", ph: "bold" },
    italic: { pre: "*", post: "*", ph: "italic" },
    strike: { pre: "~~", post: "~~", ph: "strikethrough" },
    spoiler: { pre: "||", post: "||", ph: "spoiler" },
    code: { pre: "`", post: "`", ph: "code" },
  };
  // Groups render with a thin separator between them. `keys` is only the
  // tooltip hint — the actual bindings live in onKeydown.
  const FMT_GROUPS = [
    [
      { kind: "bold", label: "Bold", keys: "B" },
      { kind: "italic", label: "Italic", keys: "I" },
      { kind: "strike", label: "Strikethrough" },
      { kind: "spoiler", label: "Spoiler", keys: "Shift+X" },
    ],
    [
      { kind: "code", label: "Inline code", keys: "E" },
      { kind: "codeblock", label: "Code block" },
    ],
    [
      { kind: "quote", label: "Quote", keys: "Shift+." },
      { kind: "link", label: "Link", keys: "K" },
    ],
  ];
  const fmtTitle = (b) => (b.keys ? `${b.label} (${MOD_LABEL}${b.keys})` : b.label);

  function applyFormat(kind) {
    if (!composerEl || !ch) return;
    const start = composerEl.selectionStart ?? draft.length;
    const end = composerEl.selectionEnd ?? start;
    const sel = draft.slice(start, end);
    let selStart, selEnd;

    if (kind === "quote") {
      // Toggle "> " on every line the selection touches.
      const lineStart = draft.lastIndexOf("\n", start - 1) + 1;
      let lineEnd = draft.indexOf("\n", end);
      if (lineEnd < 0) lineEnd = draft.length;
      const lines = draft.slice(lineStart, lineEnd).split("\n");
      const quoted = lines.every((l) => l.startsWith("> "));
      const block = lines.map((l) => (quoted ? l.slice(2) : "> " + l)).join("\n");
      draft = draft.slice(0, lineStart) + block + draft.slice(lineEnd);
      if (start === end) {
        const p = Math.max(lineStart, start + (quoted ? -2 : 2));
        selStart = selEnd = Math.min(p, lineStart + block.length);
      } else {
        selStart = lineStart;
        selEnd = lineStart + block.length;
      }
    } else if (kind === "link") {
      // A selection becomes the label with "url" selected to type over; with
      // nothing selected, insert a full placeholder and select the label.
      const label = sel || "text";
      draft = draft.slice(0, start) + `[${label}](url)` + draft.slice(end);
      if (sel) {
        selStart = start + label.length + 3; // just past "[label]("
        selEnd = selStart + 3;
      } else {
        selStart = start + 1;
        selEnd = selStart + label.length;
      }
    } else if (kind === "codeblock") {
      // Fences want their own lines — only add the newlines that are missing.
      const text = sel || "code";
      const pre = (start > 0 && draft[start - 1] !== "\n" ? "\n" : "") + "```\n";
      const post = "\n```" + (end < draft.length && draft[end] !== "\n" ? "\n" : "");
      draft = draft.slice(0, start) + pre + text + post + draft.slice(end);
      selStart = start + pre.length;
      selEnd = selStart + text.length;
    } else {
      const { pre, post, ph } = WRAPS[kind];
      // Already wrapped (markers just outside, or included in the selection)?
      // Then this press toggles the formatting back off. The italic guard
      // keeps a lone "*" check from eating half of a surrounding "**".
      const outside =
        sel &&
        draft.slice(start - pre.length, start) === pre &&
        draft.slice(end, end + post.length) === post &&
        !(kind === "italic" && draft.slice(start - 2, start) === "**" && draft.slice(end, end + 2) === "**");
      const inside = sel.length >= pre.length + post.length && sel.startsWith(pre) && sel.endsWith(post);
      if (outside) {
        draft = draft.slice(0, start - pre.length) + sel + draft.slice(end + post.length);
        selStart = start - pre.length;
        selEnd = selStart + sel.length;
      } else if (inside) {
        const inner = sel.slice(pre.length, sel.length - post.length);
        draft = draft.slice(0, start) + inner + draft.slice(end);
        selStart = start;
        selEnd = start + inner.length;
      } else {
        const text = sel || ph;
        draft = draft.slice(0, start) + pre + text + post + draft.slice(end);
        selStart = start + pre.length;
        selEnd = selStart + text.length;
      }
    }

    saveDraft(S.activeChannelId, draft);
    suggest = null;
    composerEl.focus();
    queueAutosize();
    requestAnimationFrame(() => composerEl?.setSelectionRange(selStart, selEnd));
  }

  function editLastOwnMessage() {
    const own = [...S.messages].reverse().find(
      (m) => m.sender === S.identity.fingerprint && !m.deleted && m.kind === "",
    );
    if (own) S.editing = own;
  }

  function onKeydown(e) {
    // Formatting shortcuts first — they all carry Ctrl/Cmd, so they can't
    // collide with autocomplete nav, Enter-send, or ArrowUp-edit below.
    if ((e.ctrlKey || e.metaKey) && !e.altKey) {
      const k = e.key.toLowerCase();
      const kind = !e.shiftKey
        ? k === "b"
          ? "bold"
          : k === "i"
            ? "italic"
            : k === "e"
              ? "code"
              : k === "k"
                ? "link"
                : null
        : k === "x"
          ? "spoiler"
          : e.code === "Period" || k === "." || k === ">"
            ? "quote"
            : null;
      if (kind) {
        e.preventDefault();
        applyFormat(kind);
        return;
      }
    }
    if (suggest) {
      // Tab cycles for emoji/mentions (long-standing behavior) but accepts for
      // slash commands, where there's a single obvious completion.
      const tabAccepts = suggest.kind === "slash";
      if (e.key === "ArrowDown" || (e.key === "Tab" && !e.shiftKey && !tabAccepts)) {
        e.preventDefault();
        suggest = { ...suggest, sel: (suggest.sel + 1) % suggest.items.length };
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        suggest = { ...suggest, sel: (suggest.sel - 1 + suggest.items.length) % suggest.items.length };
      } else if (e.key === "Enter" || (e.key === "Tab" && !e.shiftKey && tabAccepts)) {
        e.preventDefault();
        accept();
      } else if (e.key === "Escape") {
        suggest = null;
      }
      return;
    }
    // Physical keyboard: Enter sends, Shift+Enter is a newline. Touch: Enter is
    // always a newline (you send with the button) — matches every phone
    // messenger and avoids firing off half-typed messages on the on-screen
    // keyboard. Keyed on pointer coarseness, not layout: a narrow desktop
    // window keeps Enter-to-send. Ignore Enter mid-IME-composition so CJK
    // candidate selection doesn't send.
    if (!coarse && e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
    } else if (e.key === "ArrowUp" && !draft) {
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
    saveDraft(S.activeChannelId, draft);
    autosize();
    updateSuggest();
  }

  // Send-button launch: the paper plane flies off and glides back in. State
  // toggles the CSS animation; cleared on a timer so rapid sends replay it.
  let launching = $state(false);
  let launchTimer;
  function playLaunch() {
    clearTimeout(launchTimer);
    launching = false;
    requestAnimationFrame(() => {
      launching = true;
      launchTimer = setTimeout(() => (launching = false), 450);
    });
  }

  async function send(e) {
    e?.preventDefault();
    const raw = draft.trim();
    if (raw.startsWith("/") && S.activeChannelId && (await runAction(raw))) {
      draft = "";
      saveDraft(S.activeChannelId, "");
      suggest = null;
      queueAutosize();
      return;
    }
    const text = replaceShortcodes(applySlash(raw).trim());
    const atts = pending;
    if ((!text && atts.length === 0) || !S.activeChannelId) return;
    if (mobile) playLaunch();
    const chId = S.activeChannelId;
    const prevDraft = draft;
    const prevReply = S.replyingTo;
    draft = "";
    pending = [];
    saveDraft(chId, "");
    suggest = null;
    queueAutosize();
    // The reply attaches to the FIRST message we send; the rest are plain.
    let rt = S.replyingTo?.id || "";
    const nextReply = () => {
      const r = rt;
      rt = "";
      return r;
    };
    S.replyingTo = null;
    let sent = 0; // attachments successfully sent so far
    try {
      // Attachments first, then the caption — so a pasted image sits above its
      // text in the feed, the way Discord shows an image with a caption below.
      for (const a of atts) {
        if (a.isImage) await api.sendAttachment(chId, a.dataUrl, a.w, a.h, nextReply());
        else await api.sendFile(chId, a.dataUrl, a.name, nextReply());
        sent++;
      }
      if (text) await sendMessage(stampEphemeral(chId, text), nextReply());
    } catch (err) {
      // Restore only what did NOT go out, so a retry can't double-post. The text
      // is the last step, so on any failure it's unsent — put the draft back. The
      // reply rides the first send; only restore it if nothing was sent yet
      // (otherwise it was already consumed and would re-attach to a stray retry).
      draft = prevDraft;
      pending = atts.slice(sent);
      saveDraft(chId, prevDraft);
      if (sent === 0) S.replyingTo = prevReply;
      queueAutosize();
      flash(err);
    }
  }

  // ---- attachments ----
  // Images go out as encrypted blobs (see lib/attachments.js): up to 5 MB,
  // sent as-is when already png/jpeg/gif/webp (keeps GIF animation), anything
  // else (HEIC/AVIF/SVG/...) is canvas-normalized to JPEG — which is also what
  // fixes "my image arrived as a wall of text" for exotic formats.

  const MAX_IMAGE_BYTES = 5 * 1024 * 1024;
  const NATIVE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];

  function readAsDataURL(blob) {
    return new Promise((res, rej) => {
      const r = new FileReader();
      r.onload = () => res(r.result);
      r.onerror = rej;
      r.readAsDataURL(blob);
    });
  }

  async function decodeBitmap(file) {
    try {
      return await createImageBitmap(file);
    } catch {
      // Fallback for formats createImageBitmap rejects but <img> can decode.
      const url = URL.createObjectURL(file);
      try {
        return await new Promise((res, rej) => {
          const img = new Image();
          img.onload = () => res(img);
          img.onerror = rej;
          img.src = url;
        });
      } finally {
        URL.revokeObjectURL(url);
      }
    }
  }

  async function normalizeToJpeg(file) {
    const bmp = await decodeBitmap(file);
    const w = bmp.width || bmp.naturalWidth;
    const h = bmp.height || bmp.naturalHeight;
    const canvas = document.createElement("canvas");
    canvas.width = w;
    canvas.height = h;
    canvas.getContext("2d").drawImage(bmp, 0, 0);
    for (const q of [0.85, 0.7]) {
      const dataUrl = canvas.toDataURL("image/jpeg", q);
      if (dataUrl.length * 0.75 <= MAX_IMAGE_BYTES) return { dataUrl, w, h };
    }
    throw new Error("still too large after compression");
  }

  async function imageDims(dataUrl) {
    return new Promise((res) => {
      const img = new Image();
      img.onload = () => res({ w: img.naturalWidth, h: img.naturalHeight });
      img.onerror = () => res({ w: 0, h: 0 });
      img.src = dataUrl;
    });
  }

  const MAX_FILE_BYTES = 25 * 1024 * 1024;

  function removePending(id) {
    pending = pending.filter((p) => p.id !== id);
  }
  const uid = () =>
    (crypto?.randomUUID?.() ?? String(Date.now()) + Math.random());

  // stageImage: read + normalize an image and hold it as a pending attachment.
  async function stageImage(file) {
    if (!file || !S.activeChannelId) return;
    uploading++;
    try {
      let dataUrl, w, h;
      if (NATIVE_TYPES.includes(file.type) && file.size <= MAX_IMAGE_BYTES) {
        dataUrl = await readAsDataURL(file);
        ({ w, h } = await imageDims(dataUrl));
      } else {
        ({ dataUrl, w, h } = await normalizeToJpeg(file));
      }
      pending = [...pending, { id: uid(), dataUrl, w, h, isImage: true }];
    } catch (err) {
      const msg = String(err?.message || err);
      flash(
        msg.includes("too large")
          ? "Image too large (max 5 MB, even after compression)"
          : "Couldn't read that image format",
        "error",
      );
    } finally {
      uploading--;
    }
  }

  // attachFile: the general entry point (paste / drop / file button). Images and
  // other files alike are STAGED; nothing sends until submit.
  export async function attachFile(file) {
    if (!file || !S.activeChannelId) return;
    if (file.type.startsWith("image/")) {
      await stageImage(file);
      return;
    }
    if (file.size > MAX_FILE_BYTES) {
      flash("File too large (max 25 MB)", "error");
      return;
    }
    uploading++;
    try {
      const dataUrl = await readAsDataURL(file);
      const isVideo = file.type.startsWith("video/");
      pending = [...pending, { id: uid(), dataUrl, name: file.name || "file", isImage: false, isVideo }];
    } catch (err) {
      flash(err);
    } finally {
      uploading--;
    }
  }
  // Kept for callers that expect the old name; now stages too.
  export const attachImage = stageImage;

  // ---- voice messages ----
  // Record a clip and ship it through the same encrypted-file path as any
  // attachment (SendFile), tagged audio/* so the feed renders it as a player.
  const canRecord =
    typeof MediaRecorder !== "undefined" && !!navigator.mediaDevices?.getUserMedia;
  let recording = $state(false);
  let recSecs = $state(0);
  let mediaRec = null;
  let recChunks = [];
  let recTimer = null;
  let recStream = null;

  async function startRecording() {
    if (!ch || recording || !canRecord) return;
    try {
      recStream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      flash("Microphone permission denied", "error");
      return;
    }
    recChunks = [];
    const pref = ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus"].find(
      (t) => MediaRecorder.isTypeSupported?.(t),
    );
    mediaRec = new MediaRecorder(recStream, pref ? { mimeType: pref } : undefined);
    mediaRec.ondataavailable = (e) => e.data.size && recChunks.push(e.data);
    mediaRec.start();
    recording = true;
    recSecs = 0;
    recTimer = setInterval(() => {
      if (++recSecs >= 300) stopRecording(true); // 5-minute cap
    }, 1000);
  }

  function teardownRec() {
    clearInterval(recTimer);
    recTimer = null;
    recStream?.getTracks().forEach((t) => t.stop());
    recStream = null;
    recording = false;
  }

  function stopRecording(send) {
    const rec = mediaRec;
    mediaRec = null;
    if (!rec) {
      teardownRec();
      return;
    }
    rec.onstop = async () => {
      teardownRec();
      if (!send || !recChunks.length || !S.activeChannelId) return;
      // Clean type (no ;codecs=…) so SendFile's data-URL regex matches.
      const ogg = (rec.mimeType || "").includes("ogg");
      const blob = new Blob(recChunks, { type: ogg ? "audio/ogg" : "audio/webm" });
      if (blob.size > MAX_FILE_BYTES) {
        flash("Voice message too large", "error");
        return;
      }
      const chId = S.activeChannelId;
      uploading++;
      try {
        const dataUrl = await readAsDataURL(blob);
        await api.sendFile(chId, dataUrl, `Voice message.${ogg ? "ogg" : "webm"}`);
      } catch (err) {
        flash(err);
      } finally {
        uploading--;
      }
    };
    rec.stop();
  }

  const recClock = $derived(
    `${Math.floor(recSecs / 60)}:${(recSecs % 60).toString().padStart(2, "0")}`,
  );

  // ---- scheduled send ----
  // With a draft: pick a time and queue it (see lib/scheduled). Empty: open the
  // manager so the clock is also the way to see/cancel what's queued.
  function scheduleSend() {
    if (!S.activeChannelId) return;
    const text = replaceShortcodes(draft.trim());
    if (!text) {
      S.modal = { kind: "scheduled" };
      return;
    }
    const chId = S.activeChannelId;
    const replyTo = S.replyingTo?.id || "";
    S.modal = {
      kind: "when",
      title: "Send later",
      cta: "Schedule",
      onPick: (at) => {
        scheduleMessage(chId, text, replyTo, at);
        draft = "";
        saveDraft(chId, "");
        S.replyingTo = null;
        queueAutosize();
        flash("Message scheduled", "success");
      },
    };
  }

  function onPaste(e) {
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (item) {
      e.preventDefault();
      stageImage(item.getAsFile());
    }
  }

  // Typing anywhere focuses the composer — the pressed character lands in the
  // box naturally (focus moves during keydown, before the char is inserted).
  // Skips modifier chords, modals/menus, and anything already editable, so
  // shortcuts and other inputs keep working untouched.
  function typeToFocus(e) {
    if (!ch || S.modal || S.contextMenu || S.editing) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key.length !== 1) return; // printable characters only
    const t = e.target;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
    composerEl?.focus();
  }

  // Focus follows intent: switching channels or starting a reply puts the
  // caret in the box (desktop only — popping the keyboard on the phone for a
  // mere channel switch would be rude).
  $effect(() => {
    S.activeChannelId;
    if (!mobile && ch) composerEl?.focus();
  });
  $effect(() => {
    if (S.replyingTo && !mobile) composerEl?.focus();
  });

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

<svelte:window onkeydown={typeToFocus} />

{#if S.replyingTo}
  <div class="reply-banner">
    <span class="rb-label">
      <span class="rb-icon"><Icon name="reply" size={12} /></span>
      Replying to <strong>{S.replyingTo.senderName || S.replyingTo.sender.slice(0, 9)}</strong>
    </span>
    <button class="mini" aria-label="Cancel reply" onclick={() => (S.replyingTo = null)}>
      <Icon name="close" size={11} />
    </button>
  </div>
{/if}
<div class="typing-line muted">
  {#if uploading > 0}
    <span class="up-dot"></span> Adding {uploading > 1 ? `${uploading} attachments` : "attachment"}…
  {:else if S.typingList.length > 0}
    <span class="t-dots" aria-hidden="true"><span></span><span></span><span></span></span>
    {#if S.typingList.length === 1}
      <span
        class="typer"
        style={nameColorFor(S.typingList[0].from) ? `color:${nameColorFor(S.typingList[0].from)}` : ""}
        >{S.typingList[0].label}</span
      > is typing…
    {:else if S.typingList.length === 2}
      <span
        class="typer"
        style={nameColorFor(S.typingList[0].from) ? `color:${nameColorFor(S.typingList[0].from)}` : ""}
        >{S.typingList[0].label}</span
      > and <span
        class="typer"
        style={nameColorFor(S.typingList[1].from) ? `color:${nameColorFor(S.typingList[1].from)}` : ""}
        >{S.typingList[1].label}</span
      > are typing…
    {:else}
      {S.typingList.length} people are typing…
    {/if}
  {/if}
</div>

<div class="composer-wrap">
  {#if suggest}
    <div class="suggest-pop">
      <div class="suggest-head">
        {suggest.kind === "slash" ? "Commands" : suggest.kind === "emoji" ? "Emoji" : "Members"}
      </div>
      {#each suggest.items as item, i (suggest.kind === "emoji" ? item[0] : suggest.kind === "slash" ? item.name : item.fingerprint)}
        <button class="suggest-item" class:sel={i === suggest.sel} onclick={() => accept(i)}>
          {#if suggest.kind === "emoji"}
            <span class="s-emoji">{item[1]}</span> :{item[0]}:
          {:else if suggest.kind === "slash"}
            <span class="s-slash" aria-hidden="true"><Icon name="code" size={13} /></span>
            <span class="s-cmd">{item.usage}</span>
            <span class="s-desc">{item.desc}</span>
          {:else}
            <span class="s-emoji">@</span>{item.name}
          {/if}
          <kbd class="s-enter" aria-hidden="true">↵</kbd>
        </button>
      {/each}
    </div>
  {/if}
  {#if S.pickerTarget}
    <EmojiPicker onPick={pickEmoji} onClose={() => (S.pickerTarget = null)} />
  {/if}

  <form class="composer" class:mobile onsubmit={send}>
    <input
      type="file"
      bind:this={fileInput}
      style="display:none"
      onchange={(e) => {
        attachFile(e.target.files?.[0]);
        e.target.value = "";
      }}
    />
    {#if ephTTL > 0}
      <button type="button" class="eph-banner" onclick={() => (S.modal = { kind: "disappear", channelId: S.activeChannelId })}>
        <Icon name="clock" size={13} />
        <span>Messages disappear after <strong>{ttlLabel(ephTTL)}</strong></span>
        <span class="eph-change">change</span>
      </button>
    {/if}
    <div class="input-shell" class:active={!!ch}>
    {#if pending.length || uploading > 0}
      <div class="attach-tray">
        {#each pending as p (p.id)}
          <div class="att-chip" class:file={!p.isImage}>
            {#if p.isImage}
              <img src={p.dataUrl} alt="" />
            {:else}
              <span class="att-file"><Icon name={p.isVideo ? "play" : "attach"} size={16} /><span class="att-name">{p.name}</span></span>
            {/if}
            <button
              type="button"
              class="att-x"
              aria-label="Remove attachment"
              title="Remove"
              onclick={() => removePending(p.id)}
            >✕</button>
          </div>
        {/each}
        {#if uploading > 0}<div class="att-chip loading"><span class="att-spin"></span></div>{/if}
      </div>
    {/if}
    {#if showFmtBar}
    <div class="fmt-bar" role="toolbar" aria-label="Text formatting">
      {#each FMT_GROUPS as group, gi (gi)}
        {#if gi > 0}<span class="fmt-sep" aria-hidden="true"></span>{/if}
        {#each group as b (b.kind)}
          <button
            type="button"
            class="fmtbtn"
            title={fmtTitle(b)}
            aria-label={b.label}
            disabled={!ch}
            onmousedown={(e) => e.preventDefault()}
            onclick={() => applyFormat(b.kind)}
          >
            <Icon name={b.kind} size={15} />
          </button>
        {/each}
      {/each}
    </div>
    {/if}
    <div class="input-box" class:focused={ch} class:recording>
      {#if recording}
        <!-- Recording a voice message: the whole row becomes the transport. -->
        <span class="rec-dot" aria-hidden="true"></span>
        <span class="rec-label">Recording… <span class="rec-clock">{recClock}</span></span>
        <button
          type="button"
          class="iconbtn rec-cancel"
          title="Discard"
          aria-label="Discard recording"
          onclick={() => stopRecording(false)}
        >
          <Icon name="trash" size={18} />
        </button>
        <button
          type="button"
          class="sendbtn"
          title="Send voice message"
          aria-label="Send voice message"
          onclick={() => stopRecording(true)}
        >
          <Icon name="send" size={17} />
        </button>
      {:else}
        <button
          type="button"
          class="iconbtn"
          title="Attach a file or image (or paste / drop one)"
          aria-label="Attach a file"
          disabled={!ch || uploading > 0}
          onclick={() => fileInput.click()}
        >
          <Icon name="attach" size={20} />
        </button>
        <textarea
          bind:this={composerEl}
          class="draft"
          rows="1"
          placeholder={composerPlaceholder}
          bind:value={draft}
          disabled={!ch}
          oninput={onInput}
          onkeydown={onKeydown}
          onpaste={onPaste}
          onblur={() => setTimeout(() => (suggest = null), 150)}
        ></textarea>
        {#if mobile}
          <!-- Formatting lives behind a toggle on phones — no room for a
               permanent toolbar row, and hover-reveal doesn't exist on touch. -->
          <button
            type="button"
            class="iconbtn fmt-toggle"
            class:on={showFmt}
            title="Formatting"
            aria-label="Toggle formatting toolbar"
            disabled={!ch}
            onclick={() => (showFmt = !showFmt)}
          >Aa</button>
        {/if}
        {#if canRecord && !draft.trim() && pending.length === 0}
          <!-- Mic replaces nothing; it appears when there's no text/attachment to
               send, the way messengers surface record-vs-send. -->
          <button
            type="button"
            class="iconbtn"
            title="Record a voice message"
            aria-label="Record a voice message"
            disabled={!ch}
            onclick={startRecording}
          >
            <Icon name="mic" size={20} />
          </button>
        {/if}
        <button
          type="button"
          class="iconbtn"
          title="Create a poll"
          aria-label="Create a poll"
          disabled={!ch}
          onclick={() => (S.modal = { kind: "poll" })}
        >
          <Icon name="poll" size={20} />
        </button>
        <button
          type="button"
          class="iconbtn"
          title="Advanced composer (colors, rich embeds, preview)"
          aria-label="Advanced composer"
          disabled={!ch}
          onclick={() =>
            (S.modal = {
              kind: "compose",
              initial: draft,
              // The modal seeds from the inline draft; once IT sends, the seed
              // must go too — otherwise the same text sits in the one-line box
              // waiting for a stray Enter to post it twice.
              onSent: () => {
                draft = "";
                saveDraft(S.activeChannelId, "");
                queueAutosize();
              },
            })}
        >
          <Icon name="heading" size={19} />
        </button>
        <button
          type="button"
          class="iconbtn"
          title={draft.trim() ? "Schedule this message" : "Scheduled messages & reminders"}
          aria-label="Schedule message"
          disabled={!ch}
          onclick={scheduleSend}
        >
          <Icon name="clock" size={20} />
        </button>
        <button
          type="button"
          class="iconbtn"
          title="Emoji"
          aria-label="Emoji picker"
          disabled={!ch}
          onclick={() => (S.pickerTarget = S.pickerTarget === "composer" ? null : "composer")}
        >
          <Icon name="smile" size={20} />
        </button>
        {#if coarse}
          <!-- Touch only: on a phone Enter is a newline, so this is the only way
               to send. On desktop (even a narrow window) Enter sends and this
               button is just noise — keyed on pointer coarseness, not layout, so
               it never shows there. -->
          <button type="submit" class="sendbtn" class:launch={launching} aria-label="Send" disabled={!canSend}>
            <Icon name="send" size={17} />
          </button>
        {/if}
      {/if}
    </div>
    </div>
  </form>
</div>

<style>
  .eph-banner {
    display: flex;
    align-items: center;
    gap: 7px;
    width: 100%;
    padding: 6px 12px;
    margin-bottom: 4px;
    font-size: 12px;
    color: var(--text-muted);
    background: color-mix(in srgb, var(--accent) 8%, var(--bg-input));
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
    border-radius: var(--radius-md);
    text-align: left;
  }
  .eph-banner :global(svg) {
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  .eph-banner strong {
    color: var(--text);
    font-weight: 600;
  }
  .eph-change {
    margin-left: auto;
    color: var(--accent-hover);
    font-weight: 600;
  }
  .eph-banner:hover .eph-change {
    text-decoration: underline;
  }
  .reply-banner {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 6px 16px;
    font-size: 13px;
    border-top: 1px solid var(--border);
    /* Faint accent wash ties the banner to the reply you're composing. */
    background: color-mix(in srgb, var(--accent) 7%, transparent);
    animation: rb-in 0.16s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  @keyframes rb-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  .rb-label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rb-label strong {
    color: var(--text);
    font-weight: 600;
  }
  .rb-icon {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--accent-hover);
  }
  .typing-line {
    height: 20px;
    font-size: 12px;
    font-style: italic;
    padding: 3px 16px 1px;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .typing-line .typer {
    font-weight: 600;
    font-style: normal;
  }
  /* Three staggered bouncing dots ahead of "X is typing…". */
  .t-dots {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-right: 5px;
    vertical-align: middle;
  }
  .t-dots span {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
    animation: t-bounce 1.2s ease-in-out infinite;
  }
  .t-dots span:nth-child(2) {
    animation-delay: 0.15s;
  }
  .t-dots span:nth-child(3) {
    animation-delay: 0.3s;
  }
  @keyframes t-bounce {
    0%,
    55%,
    100% {
      transform: none;
      opacity: 0.35;
    }
    28% {
      transform: translateY(-3px);
      opacity: 1;
    }
  }
  /* The global reduced-motion rule only shortens durations — an infinite loop
     would still churn, so stop the dots entirely and hold a steady frame. */
  @media (prefers-reduced-motion: reduce) {
    .t-dots span,
    .up-dot {
      animation: none;
      opacity: 0.7;
    }
  }
  .up-dot {
    display: inline-block;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    animation: up-pulse 0.9s ease-in-out infinite;
    vertical-align: middle;
  }
  @keyframes up-pulse {
    50% {
      opacity: 0.3;
    }
  }
  .composer-wrap {
    position: relative;
  }
  .suggest-pop {
    position: absolute;
    /* Anchor to the wrap's top edge, not a fixed offset — the mobile input is
       taller than desktop's, and a hardcoded bottom overlapped it. */
    bottom: calc(100% + 6px);
    left: 60px;
    /* Glassy floating panel over the feed, matching the command palette. */
    background: color-mix(in srgb, var(--bg-1) 90%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 4px;
    display: flex;
    flex-direction: column;
    min-width: 240px;
    box-shadow: var(--shadow-pop);
    z-index: 50;
    transform-origin: bottom left;
    animation: sg-pop 0.14s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  @keyframes sg-pop {
    from {
      opacity: 0;
      transform: translateY(4px) scale(0.98);
    }
  }
  .suggest-item {
    display: flex;
    align-items: center;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    font-size: 13px;
    font-family: ui-monospace, monospace;
    transition:
      background 0.1s ease,
      transform 0.12s ease;
  }
  .suggest-item.sel,
  .suggest-item:hover {
    background: var(--bg-3);
    transform: translateX(2px);
  }
  .suggest-item.sel {
    background: color-mix(in srgb, var(--accent) 12%, var(--bg-3));
    box-shadow: inset 2px 0 0 var(--accent);
  }
  /* ↵ affordance on the selected row only (matches the command palette). */
  .s-enter {
    margin-left: auto;
    padding-left: 12px;
    font-family: inherit;
    font-size: 11px;
    color: var(--accent-hover);
    opacity: 0;
    transition: opacity 0.12s ease;
  }
  .suggest-item.sel .s-enter {
    opacity: 0.9;
  }
  .s-emoji {
    font-size: 16px;
    margin-right: 6px;
  }
  .s-slash {
    display: inline-flex;
    color: var(--accent-hover);
    margin-right: 7px;
    opacity: 0.9;
  }
  .suggest-head {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 4px 10px 3px;
  }
  .s-cmd {
    font-weight: 600;
  }
  /* Description trails the command in the UI font (the command stays mono). */
  .s-desc {
    flex: 1;
    min-width: 0;
    margin-left: 14px;
    font-family:
      system-ui,
      -apple-system,
      sans-serif;
    font-size: 12px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .composer {
    padding: 0 16px 16px;
  }
  /* Formatting toolbar: a whisper-quiet row above the input that comes up to
     full strength while the composer is hovered or focused. No transforms, so
     the global reduced-motion duration-zeroing covers it. */
  /* One seamless input shell: toolbar, attach, text, and emoji all live
     INSIDE a single rounded well — no borders between the pieces. */
  .input-shell {
    background: var(--bg-input);
    border: 1px solid transparent;
    border-radius: var(--radius-lg);
    transition:
      border-color 0.15s ease,
      background 0.15s ease,
      box-shadow 0.15s ease;
  }
  .input-shell.active:focus-within {
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent);
    background: color-mix(in srgb, var(--accent) 3%, var(--bg-input));
  }
  .attach-tray {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 10px 10px 6px;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
  }
  .att-chip {
    position: relative;
    border-radius: 8px;
    overflow: hidden;
    background: color-mix(in srgb, var(--border) 30%, var(--bg-input));
    border: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .att-chip img {
    display: block;
    width: 64px;
    height: 64px;
    object-fit: cover;
  }
  .att-chip.file {
    display: flex;
    align-items: center;
    min-height: 40px;
  }
  .att-file {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 34px 8px 10px;
    max-width: 220px;
    color: var(--text-muted);
  }
  .att-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
  }
  .att-x {
    position: absolute;
    top: 3px;
    right: 3px;
    width: 18px;
    height: 18px;
    display: grid;
    place-items: center;
    border: none;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.65);
    color: #fff;
    font-size: 10px;
    line-height: 1;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.12s ease;
  }
  .att-chip:hover .att-x,
  .att-x:focus-visible {
    opacity: 1;
  }
  .att-chip.loading {
    width: 64px;
    height: 64px;
    display: grid;
    place-items: center;
  }
  .att-spin {
    width: 20px;
    height: 20px;
    border: 2px solid color-mix(in srgb, var(--border) 60%, transparent);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: att-spin 0.7s linear infinite;
  }
  @keyframes att-spin {
    to {
      transform: rotate(360deg);
    }
  }
  /* Coarse pointers can't hover to reveal the remove button — keep it visible. */
  @media (pointer: coarse) {
    .att-x {
      opacity: 1;
    }
  }
  .fmt-bar {
    display: flex;
    align-items: center;
    gap: 1px;
    padding: 5px 8px 4px;
    /* Quiet until the composer is hovered or focused, but not invisible: at
       0.45 these icons sat at 1.9:1 against the input they float over. */
    opacity: 0.8;
    transition: opacity 0.15s ease;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
  }
  .composer:hover .fmt-bar,
  .composer:focus-within .fmt-bar {
    opacity: 1;
  }
  .fmtbtn {
    display: grid;
    place-items: center;
    width: 26px;
    height: 22px;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    transition:
      background 0.12s ease,
      color 0.12s ease;
  }
  .fmtbtn:hover:not(:disabled) {
    background: var(--bg-3);
    color: var(--text);
  }
  .fmtbtn:active:not(:disabled) {
    background: var(--bg-3);
  }
  .fmtbtn:disabled {
    opacity: 0.35;
  }
  .fmt-sep {
    width: 1px;
    height: 14px;
    background: var(--border);
    margin: 0 5px;
    flex: none;
  }
  /* One unified rounded bar — icons live inside it, Discord-style. */
  .input-box {
    display: flex;
    align-items: flex-end;
    gap: 3px;
    background: transparent;
    border: none;
    border-radius: 0;
    padding: 3px 8px;
  }
  .input-box.recording {
    align-items: center;
    padding: 8px 10px;
  }
  .rec-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--danger);
    flex-shrink: 0;
    animation: rec-pulse 1.1s ease-in-out infinite;
  }
  .rec-label {
    flex: 1;
    font-size: 13px;
    color: var(--text-muted);
  }
  .rec-clock {
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  .rec-cancel:hover :global(svg) {
    color: var(--danger-text);
  }
  @keyframes rec-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .rec-dot {
      animation: none;
    }
  }
  /* (Focus ring moved up to .input-shell — the whole well glows as one.) */
  .draft {
    flex: 1;
    min-width: 0;
    resize: none;
    overflow-y: auto;
    max-height: 200px;
    height: auto;
    /* Naked inside the shell: the global textarea "recessed well" styling
       would draw a SECOND border/inset inside the composer. */
    background: transparent !important;
    border: none !important;
    box-shadow: none !important;
    outline: none !important; /* the SHELL carries the focus ring */
    border-radius: 0;
    /* Vertical padding tuned so a single-line draft sits at the same height as
       the 34px icon buttons beside it — no more text baseline floating above the
       controls on an empty/one-line composer. */
    padding: 7px 6px;
    font-family: inherit;
    line-height: 1.4;
    box-sizing: border-box;
    width: auto;
  }
  .draft:focus {
    border: none;
  }
  /* Bare icon buttons: muted glyphs that brighten on hover, no box. Fixed square
     so every tray control occupies the same footprint regardless of its glyph's
     intrinsic size — the row reads as an even set, not a jumble of sizes. */
  .iconbtn {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    padding: 0;
    flex-shrink: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    align-self: flex-end;
    transition:
      color 0.12s ease,
      transform 0.12s ease;
  }
  .iconbtn:hover:not(:disabled) {
    background: transparent;
    color: var(--text);
    transform: scale(1.1);
  }
  .iconbtn:active:not(:disabled) {
    transform: scale(0.92);
  }
  .iconbtn:disabled {
    opacity: 0.4;
  }
  /* app.css already zeroes transition durations under reduced motion; also
     drop the scale so hover doesn't snap the glyph around. */
  @media (prefers-reduced-motion: reduce) {
    .iconbtn:hover:not(:disabled),
    .iconbtn:active:not(:disabled) {
      transform: none;
    }
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

  /* ---- mobile composer ---- */
  .composer.mobile {
    padding: 0 10px calc(10px + env(safe-area-inset-bottom));
  }
  /* On a phone the tray and the text can't share one line. Eight finger-sized
     buttons at 44px come to ~350px, which on a 390px-wide screen left the
     textarea about 8px — the placeholder rendered one character per line, and
     it looked like the styling had simply collapsed. So the text gets its own
     full-width row and the tray sits underneath it. */
  .composer.mobile .input-box {
    flex-wrap: wrap;
    row-gap: 2px;
    /* Seven 44px targets are 308px; a 360px handset leaves the tray 322px, and
       the 3px gaps pushed it to 326 — the emoji button orphaned onto a third
       row. Butt the targets together instead; the glyphs inside stay 24px
       apart, which is what the eye actually reads. */
    column-gap: 0;
  }
  /* Send sits WITH the text, not in the tray. Two reasons. The tray was eight
     44px targets in 390px of phone — 38px of slack, so a narrower handset or
     Android's font scaling wrapped the send button onto a third row of its own,
     which reads as a layout that broke. And it belongs here anyway: every
     messaging app puts send beside what you just typed, where your thumb
     already is. Taking it out of the tray also gives the remaining icons room
     to breathe. */
  .composer.mobile .draft {
    flex: 1 1 auto;
    order: -2;
    font-size: 16px;
    padding: 10px 6px;
  }
  .composer.mobile .sendbtn {
    order: -1;
    flex: none;
    align-self: flex-end;
    margin-bottom: 2px;
  }
  /* Everything else wraps to the tray row beneath. */
  .composer.mobile .iconbtn,
  .composer.mobile .fmt-toggle {
    order: 0;
  }
  .composer.mobile .input-box::after {
    /* Forces the tray onto its own line without depending on the icons being
       wide enough to wrap on their own — which is exactly the fragile bit that
       put send on a third row. */
    content: "";
    flex-basis: 100%;
    order: -1;
  }
  /* On touch the fmt bar can't hover-reveal — when toggled on, show it at
     full strength and give the buttons real finger targets. */
  .composer.mobile .fmt-bar {
    opacity: 1;
    padding-bottom: 6px;
    gap: 4px;
  }
  /* Eight buttons at a full 44px wide don't fit 360px of phone, so they share
     the row evenly and take the 44px on the axis that's free: height. */
  .composer.mobile .fmtbtn {
    flex: 1 1 0;
    width: auto;
    height: 44px;
  }
  /* Finger-sized (≥44px) targets for the icon row and send button; glyphs
     stay grid-centered so only the tap area grows. */
  .composer.mobile .iconbtn {
    min-width: 44px;
    min-height: 44px;
  }
  .composer.mobile .sendbtn {
    width: 44px;
    height: 44px;
  }
  .fmt-toggle {
    font-size: 14px;
    font-weight: 700;
    font-family: inherit;
    line-height: 1;
  }
  .fmt-toggle.on {
    color: var(--accent-hover);
  }
  .sendbtn {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    padding: 0; /* beat the global button padding — it un-centers the icon */
    flex-shrink: 0;
    align-self: flex-end;
    margin: 2px 0 2px 2px;
    border-radius: 50%;
    background: var(--accent);
    color: var(--accent-fg);
    transition: opacity 0.12s ease, transform 0.12s ease;
  }
  .sendbtn:active:not(:disabled) {
    transform: scale(0.9);
  }
  .sendbtn:disabled {
    opacity: 0.35;
  }
  /* On send the plane takes off up-right, then glides back in from the left —
     one quick whoosh, clipped to the round button. */
  .sendbtn {
    overflow: hidden;
  }
  .sendbtn.launch :global(svg) {
    animation: send-launch 0.45s cubic-bezier(0.4, 0, 0.6, 1);
  }
  @keyframes send-launch {
    45% {
      transform: translate(26px, -26px);
      opacity: 0;
    }
    46% {
      transform: translate(-20px, 20px);
      opacity: 0;
    }
    100% {
      transform: none;
      opacity: 1;
    }
  }
</style>
