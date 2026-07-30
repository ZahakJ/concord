<script>
  // RichEditor — the writing surface both modal composers are built on: the
  // advanced composer (a message) and the forum-post composer (a post's opening
  // message). One surface, so a formatting toolbar exists in exactly TWO places
  // in this app (here and the one-line Composer) instead of four.
  //
  // The design brief was "many more knobs" AND "best design principles", which
  // pull in opposite directions. The resolution is progressive disclosure, and
  // it is the organising idea of this file:
  //
  //   AT REST you see twelve formatting controls, grouped by INTENT — emphasis,
  //   then structure, then insertion — plus the view switcher. Nothing else.
  //   Everything further is exactly one obvious gesture away:
  //     · heading levels          → the ▾ on the heading button
  //     · code-block languages    → the ▾ on the code button
  //     · nine colours + custom   → the colour button's popover
  //     · the whole emoji set     → the emoji button's popover
  //     · underline, inline code, numbered list, block quote, clear formatting
  //                               → the ⋯ menu
  //     · per-attachment spoiler / name / alt text
  //                               → the chip's own tools
  //   A wall of forty buttons is not power, it is a search problem.
  //
  // Two rules the toolbar obeys, and they are why it can be trusted:
  //   1. Nothing here emits syntax lib/markdown.js cannot render. Every button
  //      maps onto a rule that exists in the renderer — no "tables" that arrive
  //      as literal pipes.
  //   2. The preview calls the SAME renderMarkdown the feed calls, with the same
  //      mention/emoji tables, so preview and reality cannot diverge.
  import { tick } from "svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import EmojiPicker from "../EmojiPicker.svelte";
  import { renderMarkdown, COLOR_NAMES } from "../lib/markdown.js";
  import { activeShortcode, searchEmoji, replaceShortcodes } from "../lib/emoji.js";
  import { S, customEmojiMap, mentionRefs, selfMember, flash } from "../lib/state.svelte.js";
  import { stagedImage } from "../lib/attachopts.js";
  import {
    wrap,
    linePrefix,
    orderedList,
    heading,
    fence,
    link as mdLink,
    colorize,
    insert as mdInsert,
    continueList,
    bodyStats,
    sendsAsIs,
    dataUrlBytes,
    prettyBytes,
    IMAGE_MAX_BYTES,
    FILE_MAX_BYTES,
  } from "../lib/postdraft.js";

  let {
    body = $bindable(""),
    // Staged attachments, shape-compatible with Composer.svelte's tray so the
    // send path is identical: { id, dataUrl, w, h, isImage, spoiler, name,
    // origName, desc }.
    pending = $bindable([]),
    // "write" | "split" | "preview". Bindable so the parent can remember it.
    mode = $bindable("split"),
    // Distraction-free. Bindable so the HOST can also stand down: the advanced
    // composer hides its embed builder in focus mode, because "everything but
    // the text" cannot mean "everything but the text and this six-field form".
    zen = $bindable(false),
    placeholder = "Write…",
    attachments = true,
    autofocus = false,
    minHeight = 200,
    // Rendered above the preview body — the post title, for the forum composer.
    previewTitle = "",
    // One line under the tray, when where the attachments land needs saying.
    attachNote = "",
    // Snippets: extra toolbar items (the embed toggle) and extra preview content
    // (the embed card), so the parent extends this surface without forking it.
    toolbarExtra = null,
    previewExtra = null,
    // Whether previewExtra has anything in it — decides "empty preview".
    previewExtraFilled = false,
    onSubmit = null,
    submitHint = "",
    onInput = null,
  } = $props();

  let ta = $state(null);
  let fileInput = $state(null);
  let reading = $state(0); // files being decoded into the tray
  let pop = $state(""); // "" | "color" | "more" | "lang" | "head"
  let emojiOpen = $state(false);
  let editingAtt = $state("");
  let focused = $state(false);
  let suggest = $state(null); // { kind, start, items, sel }

  const cemoji = $derived(customEmojiMap());
  const refs = $derived(mentionRefs());
  const me = $derived(selfMember());
  const stats = $derived(bodyStats(body));
  const hasBody = $derived(!!body.trim());
  // Split is only offered where two columns are honest. Below that the mode
  // BEHAVES as "write" without being rewritten, so widening the window brings
  // the split back rather than silently forgetting the choice.
  let wideView = $state(true);
  $effect(() => {
    const mq = window.matchMedia("(min-width: 761px)");
    const sync = () => (wideView = mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  });
  const view = $derived(!wideView && mode === "split" ? "write" : mode);
  const showPreview = $derived(!zen && (view === "preview" || view === "split"));
  const showEditor = $derived(zen || view !== "preview");

  // Narrow screens: the emphasis/structure rows live behind an "Aa" toggle, the
  // same pattern Composer.svelte already uses on a phone. Four permanent rows of
  // 44px thumb targets is most of a 390×844 sheet, and hover-reveal doesn't
  // exist on touch — so the tools you reach for while writing (link, colour,
  // emoji, attach) stay out, and the marks you reach for once are one tap away.
  let fmtOpen = $state(false);
  const showFmt = $derived(wideView || fmtOpen);

  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent || "");
  const MOD = isMac ? "⌘" : "Ctrl+";

  // @everyone/@here plus the roster, exactly as Message.svelte builds it — the
  // preview must highlight the same names the sent message will.
  const mentionNames = $derived([
    { name: "everyone", self: true },
    { name: "here", self: true },
    ...S.members.filter((m) => m.name).map((m) => ({ name: m.name, self: m.isSelf })),
  ]);

  export function focus() {
    ta?.focus();
  }
  export function textarea() {
    return ta;
  }

  // ---- applying a transform -------------------------------------------------
  // Every transform is a pure function in lib/postdraft.js. This is the only
  // place that touches the DOM: assign, then restore the selection AFTER Svelte
  // has flushed the new value. tick(), not queueMicrotask — microtask ordering
  // relative to the flush isn't guaranteed, and the caret lands in the old text.
  function apply(fn) {
    const el = ta;
    if (!el) return;
    const s = el.selectionStart ?? body.length;
    const e = el.selectionEnd ?? s;
    const r = fn(body, s, e);
    body = r.text;
    pop = "";
    suggest = null;
    onInput?.();
    tick().then(() => {
      el.focus();
      el.setSelectionRange(r.start, r.end);
    });
  }

  const noSteal = (e) => e.preventDefault(); // a tool press must not blur the textarea

  // ---- the toolbar ---------------------------------------------------------
  // Grouped by what the author is DOING, not by what was added last: emphasis,
  // structure, insertion. `key` is only the tooltip hint; the bindings live in
  // onKeydown, which is the single source of truth for them.
  const EMPHASIS = [
    { id: "bold", icon: "bold", label: "Bold", key: "B", run: (t, s, e) => wrap(t, s, e, "**", "**", "bold") },
    { id: "italic", icon: "italic", label: "Italic", key: "I", run: (t, s, e) => wrap(t, s, e, "*", "*", "italic") },
    { id: "strike", icon: "strike", label: "Strikethrough", run: (t, s, e) => wrap(t, s, e, "~~", "~~", "struck") },
    { id: "spoiler", icon: "spoiler", label: "Spoiler", key: "Shift+X", run: (t, s, e) => wrap(t, s, e, "||", "||", "spoiler") },
  ];
  const STRUCTURE = [
    { id: "quote", icon: "quote", label: "Quote", key: "Shift+.", run: (t, s, e) => linePrefix(t, s, e, "> ") },
    { id: "list", icon: "list", label: "Bulleted list", key: "Shift+8", run: (t, s, e) => linePrefix(t, s, e, "- ") },
  ];

  // Everything the ⋯ menu holds, and why each one is in there rather than on the
  // bar: underline and inline code are rarer than the four emphasis marks;
  // numbered lists are rarer than bullets; ">>>" is a power move; "clear
  // formatting" is a repair, not an action. Six of the sixteen controls are
  // behind this one press.
  const MORE = [
    { id: "underline", icon: "underline", label: "Underline", key: "U", run: (t, s, e) => wrap(t, s, e, "__", "__", "underline") },
    { id: "code", icon: "code", label: "Inline code", key: "E", run: (t, s, e) => wrap(t, s, e, "`", "`", "code") },
    { id: "ol", icon: "list", label: "Numbered list", key: "Shift+7", run: orderedList },
    { id: "bq", icon: "quote", label: "Block quote (rest of the message)", run: (t, s, e) => linePrefix(t, s, e, ">>> ") },
    { id: "clear", icon: "close", label: "Clear formatting", run: clearFormatting },
  ];

  // Languages the fence label is worth setting. Not a list of every language
  // that exists — the renderer only prints the label, so this is about the five
  // or six people actually paste.
  const LANGS = ["", "js", "go", "py", "sh", "json", "html", "css", "sql", "diff"];
  const HEADINGS = [
    { level: 1, label: "Large heading" },
    { level: 2, label: "Medium heading" },
    { level: 3, label: "Small heading" },
  ];
  const PALETTE = Object.entries(COLOR_NAMES); // [name, hex]
  let customColor = $state("#14a394");

  // clearFormatting strips the markers this editor can produce from the
  // selection — the escape hatch for pasted-in soup. Conservative on purpose: it
  // only removes syntax, it never touches words.
  function clearFormatting(text, s, e) {
    const sel = text.slice(s, e);
    if (!sel) return { text, start: s, end: e };
    const bare = sel
      .replace(/\{(?:#[0-9a-fA-F]{3,6}|[a-z]{3,10})\|([^{}]*)\}/g, "$1")
      .replace(/(\*\*\*|\*\*|\*|__|~~|\|\||`)/g, "")
      .replace(/^(\s*)(?:#{1,3}\s|[-*]\s|\d+[.)]\s|>{1,3}\s?)/gm, "$1");
    return { text: text.slice(0, s) + bare + text.slice(e), start: s, end: s + bare.length };
  }

  // ---- autocomplete (:shortcode, @mention, #channel) ----------------------
  // Deliberately NOT slash commands: "/meme" and "/clear" are actions the inline
  // composer runs, and offering them in a dialog that only sends text would
  // promise something that doesn't happen.
  const SUGGEST_HEADS = { emoji: "Emoji", mention: "Members & roles", channel: "Channels" };

  function activeMention(text, caret) {
    const upto = text.slice(0, caret);
    const at = upto.lastIndexOf("@");
    if (at < 0 || (at > 0 && /[\w@]/.test(upto[at - 1]))) return null;
    const query = upto.slice(at + 1);
    if (/\s[\s]/.test(query) || query.length > 24) return null;
    return { start: at, query };
  }
  // "#" also starts a markdown heading, so any whitespace closes the list — by
  // the time you've typed "# " you're writing a heading, not naming a channel.
  function activeChannelRef(text, caret) {
    const upto = text.slice(0, caret);
    const at = upto.lastIndexOf("#");
    if (at < 0 || (at > 0 && /[\w#]/.test(upto[at - 1]))) return null;
    const query = upto.slice(at + 1);
    if (/\s/.test(query) || query.length > 32) return null;
    return { start: at, query };
  }

  const roleTint = (item) =>
    item.kind === "role" && /^#[0-9a-fA-F]{3,6}$/.test(item.color || "") ? `color:${item.color}` : "";

  function updateSuggest() {
    const caret = ta?.selectionStart ?? body.length;
    const emoji = activeShortcode(body, caret);
    if (emoji) {
      const items = searchEmoji(emoji.query, 8);
      suggest = items.length ? { kind: "emoji", start: emoji.start, items, sel: 0 } : null;
      return;
    }
    const mention = activeMention(body, caret);
    if (mention) {
      const q = mention.query.toLowerCase();
      const hit = (name) => name && name.toLowerCase().includes(q);
      const items = [
        ...S.members.filter((m) => !m.isSelf && hit(m.name)).map((m) => ({ key: `m:${m.fingerprint}`, name: m.name, kind: "member" })),
        ...refs.roles.filter((r) => hit(r.name)).map((r) => ({ key: `r:${r.name}`, name: r.name, kind: "role", color: r.color })),
      ].slice(0, 6);
      suggest = items.length ? { kind: "mention", start: mention.start, items, sel: 0 } : null;
      return;
    }
    const chref = activeChannelRef(body, caret);
    if (chref) {
      const q = chref.query.toLowerCase();
      const items = refs.channels
        .filter((c) => c.name.toLowerCase().includes(q))
        .map((c) => ({ key: `c:${c.id}`, name: c.name }))
        .slice(0, 6);
      suggest = items.length ? { kind: "channel", start: chref.start, items, sel: 0 } : null;
      return;
    }
    suggest = null;
  }

  function accept(idx = null) {
    if (!suggest) return;
    const caret = ta?.selectionStart ?? body.length;
    const item = suggest.items[idx ?? suggest.sel];
    const ins =
      suggest.kind === "emoji" ? item[1] + " " : suggest.kind === "mention" ? `@${item.name} ` : `#${item.name} `;
    const start = suggest.start;
    body = body.slice(0, start) + ins + body.slice(caret);
    suggest = null;
    onInput?.();
    const pos = start + ins.length;
    tick().then(() => {
      ta?.focus();
      ta?.setSelectionRange(pos, pos);
    });
  }

  // ---- keyboard -----------------------------------------------------------
  // Full parity with the mouse: every toolbar action has a chord, the popovers
  // are arrow-navigable, and Escape unwinds one layer at a time (suggestion →
  // popover → zen) before it ever reaches the dialog's own close.
  function onKeydown(e) {
    if ((e.ctrlKey || e.metaKey) && !e.altKey) {
      const k = e.key.toLowerCase();
      if (k === "enter") {
        e.preventDefault();
        onSubmit?.();
        return;
      }
      // Shifted digits and punctuation arrive as the SHIFTED character
      // ("&" for Shift+7, ">" for Shift+.), and which character depends on the
      // keyboard layout — so the physical key (e.code) is the reliable half of
      // the pair and e.key is only a fallback.
      const hit = !e.shiftKey
        ? { b: "bold", i: "italic", u: "underline", e: "code", k: "link" }[k]
        : e.code === "Digit7" || k === "7"
          ? "ol"
          : e.code === "Digit8" || k === "8"
            ? "list"
            : e.code === "Period" || k === "." || k === ">"
              ? "quote"
              : k === "x"
                ? "spoiler"
                : null;
      if (hit) {
        e.preventDefault();
        if (hit === "link") apply(mdLink);
        else {
          const tool = [...EMPHASIS, ...STRUCTURE, ...MORE].find((t) => t.id === hit);
          if (tool) apply(tool.run);
        }
        return;
      }
      // Ctrl/Cmd+Alt+1..3 sets the heading level. Alt keeps it clear of the
      // browser's own tab-switching chords.
      return;
    }
    if ((e.ctrlKey || e.metaKey) && e.altKey && "123".includes(e.key)) {
      e.preventDefault();
      apply((t, s, en) => heading(t, s, en, Number(e.key)));
      return;
    }
    if (suggest) {
      if (e.key === "ArrowDown" || (e.key === "Tab" && !e.shiftKey)) {
        e.preventDefault();
        suggest = { ...suggest, sel: (suggest.sel + 1) % suggest.items.length };
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        suggest = { ...suggest, sel: (suggest.sel - 1 + suggest.items.length) % suggest.items.length };
      } else if (e.key === "Enter" && !e.isComposing) {
        e.preventDefault();
        accept();
      } else if (e.key === "Escape") {
        e.stopPropagation();
        suggest = null;
      }
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      // Continue a list / quote. Enter is a NEWLINE in this editor (you submit
      // with ⌘↵), so this is purely about not retyping the marker — and about
      // an Enter on an empty bullet ending the list instead of adding another.
      const caret = ta?.selectionStart ?? 0;
      if (caret === (ta?.selectionEnd ?? caret)) {
        const r = continueList(body, caret);
        if (r) {
          e.preventDefault();
          body = r.text;
          onInput?.();
          tick().then(() => ta?.setSelectionRange(r.caret, r.caret));
        }
      }
    }
  }

  // Escape unwinds ONE layer per press, and it has to beat Modal.svelte's own
  // window handler (which closes the dialog outright). A CAPTURE-phase listener
  // on window runs before any bubble-phase one, so stopping propagation here is
  // what keeps "Escape out of the emoji picker" from also throwing away a
  // half-written post.
  function onWindowKeyCapture(e) {
    if (e.key !== "Escape") return;
    if (escapeLayer()) {
      e.preventDefault();
      e.stopPropagation();
    }
  }

  // escapeLayer: consumed one layer per press. Returns true when this editor
  // handled it, so the host dialog leaves the draft alone.
  export function escapeLayer() {
    if (suggest) {
      suggest = null;
      return true;
    }
    if (emojiOpen) {
      emojiOpen = false;
      return true;
    }
    if (pop) {
      pop = "";
      return true;
    }
    if (zen) {
      zen = false;
      return true;
    }
    return false;
  }

  function handleInput() {
    updateSuggest();
    onInput?.();
  }

  // ---- attachments --------------------------------------------------------
  // Same rules and the same limits as Composer.svelte (lib/postdraft.js holds
  // them): 5 MB images, 25 MB files, the four native image types travel
  // untouched so a GIF keeps animating, and everything else is canvas-
  // normalized to JPEG — which is also what fixes "my HEIC arrived as text".
  const uid = () => crypto?.randomUUID?.() ?? String(Date.now()) + Math.random();

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
      if (dataUrlBytes(dataUrl) <= IMAGE_MAX_BYTES) return { dataUrl, w, h };
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

  export async function attachFile(file) {
    if (!file || !attachments) return;
    if (file.type.startsWith("image/")) {
      reading++;
      try {
        let dataUrl, w, h;
        if (sendsAsIs(file.type, file.size)) {
          dataUrl = await readAsDataURL(file);
          ({ w, h } = await imageDims(dataUrl));
        } else {
          ({ dataUrl, w, h } = await normalizeToJpeg(file));
        }
        // stagedImage (lib/attachopts.js) keeps `name` EMPTY even though we know
        // the file name, because a non-empty name forces the v2 attachment token
        // that peers on the last release render as ~190 characters of raw text.
        // The file name is kept as origName, for the rename field's placeholder
        // only. This exact shortcut has shipped as a bug once already.
        pending = [...pending, stagedImage({ id: uid(), dataUrl, w, h, fileName: file.name || "" })];
      } catch (err) {
        const msg = String(err?.message || err);
        flash(msg.includes("too large") ? "Image too large (max 5 MB, even after compression)" : "Couldn't read that image format", "error");
      } finally {
        reading--;
      }
      onInput?.();
      return;
    }
    if (file.size > FILE_MAX_BYTES) {
      flash("File too large (max 25 MB)", "error");
      return;
    }
    reading++;
    try {
      const dataUrl = await readAsDataURL(file);
      pending = [
        ...pending,
        { id: uid(), dataUrl, name: file.name || "file", isImage: false, isVideo: file.type.startsWith("video/"), bytes: file.size },
      ];
    } catch (err) {
      flash(err);
    } finally {
      reading--;
    }
    onInput?.();
  }

  function setAtt(id, key, value) {
    pending = pending.map((p) => (p.id === id ? { ...p, [key]: value } : p));
  }
  function removeAtt(id) {
    pending = pending.filter((p) => p.id !== id);
    if (editingAtt === id) editingAtt = "";
    onInput?.();
  }

  function onPaste(e) {
    if (!attachments) return;
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (item) {
      e.preventDefault();
      attachFile(item.getAsFile());
    }
  }
  let dropping = $state(false);
  function onDrop(e) {
    dropping = false;
    if (!attachments) return;
    const files = [...(e.dataTransfer?.files || [])];
    if (!files.length) return;
    e.preventDefault();
    files.forEach(attachFile);
  }

  function pickEmoji(ch) {
    // Shortcodes are expanded on send by the inline composer; here the preview
    // must be honest immediately, so a unicode char goes in as itself and a
    // guild :name: goes in as the shortcode the renderer understands.
    apply((t, s, e) => mdInsert(t, s, e, ch));
    emojiOpen = false;
  }

  // Preview body: shortcodes resolved the same way send() resolves them, so
  // ":tada:" previews as 🎉 rather than as text that will change on send.
  const previewBody = $derived(replaceShortcodes(body));
  const previewEmpty = $derived(!hasBody && !previewExtraFilled && pending.length === 0);

  // A popover is closed by clicking anywhere else. Registered on the whole
  // editor rather than the window so it can't fight the dialog's own handlers.
  function onPointerDown(e) {
    if (pop && !e.target.closest?.(".pop, .popbtn")) pop = "";
  }
</script>

<svelte:window onkeydowncapture={onWindowKeyCapture} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="rx" class:zen style="--work-min:{minHeight}px" onpointerdown={onPointerDown}>
  <div class="bar" role="toolbar" aria-label="Formatting">
    {#if showFmt}
    <div class="grp" role="group" aria-label="Emphasis">
      {#each EMPHASIS as t (t.id)}
        <button
          type="button"
          class="tb"
          title={t.key ? `${t.label} (${MOD}${t.key})` : t.label}
          aria-label={t.label}
          onmousedown={noSteal}
          onclick={() => apply(t.run)}><Icon name={t.icon} size={15} /></button>
      {/each}
    </div>
    <span class="sep" aria-hidden="true"></span>
    <div class="grp" role="group" aria-label="Structure">
      <!-- Heading is a split button: press it for H2 (the one you want 90% of
           the time), open the ▾ for the three levels. -->
      <div class="split">
        <button
          type="button"
          class="tb"
          title="Heading ({MOD}Alt+2)"
          aria-label="Heading"
          onmousedown={noSteal}
          onclick={() => apply((t, s, e) => heading(t, s, e, 2))}><Icon name="heading" size={15} /></button>
        <button
          type="button"
          class="caret popbtn"
          class:on={pop === "head"}
          aria-label="Heading level"
          aria-expanded={pop === "head"}
          title="Heading level"
          onmousedown={noSteal}
          onclick={() => (pop = pop === "head" ? "" : "head")}><Icon name="chevron" size={9} /></button>
        {#if pop === "head"}
          <div class="pop menu" role="menu">
            {#each HEADINGS as h (h.level)}
              <button type="button" class="mi" role="menuitem" onmousedown={noSteal} onclick={() => apply((t, s, e) => heading(t, s, e, h.level))}>
                <span class="mi-h" style="font-size:{18 - h.level * 2}px">{"#".repeat(h.level)}</span>
                <span class="mi-l">{h.label}</span>
                <kbd>{MOD}Alt+{h.level}</kbd>
              </button>
            {/each}
          </div>
        {/if}
      </div>
      {#each STRUCTURE as t (t.id)}
        <button
          type="button"
          class="tb"
          title={t.key ? `${t.label} (${MOD}${t.key})` : t.label}
          aria-label={t.label}
          onmousedown={noSteal}
          onclick={() => apply(t.run)}><Icon name={t.icon} size={15} /></button>
      {/each}
      <div class="split">
        <button
          type="button"
          class="tb"
          title="Code block"
          aria-label="Code block"
          onmousedown={noSteal}
          onclick={() => apply((t, s, e) => fence(t, s, e, ""))}><Icon name="codeblock" size={15} /></button>
        <button
          type="button"
          class="caret popbtn"
          class:on={pop === "lang"}
          aria-label="Code block language"
          aria-expanded={pop === "lang"}
          title="Language"
          onmousedown={noSteal}
          onclick={() => (pop = pop === "lang" ? "" : "lang")}><Icon name="chevron" size={9} /></button>
        {#if pop === "lang"}
          <div class="pop menu narrow" role="menu">
            {#each LANGS as l (l)}
              <button type="button" class="mi" role="menuitem" onmousedown={noSteal} onclick={() => apply((t, s, e) => fence(t, s, e, l))}>
                <span class="mi-l mono">{l || "plain"}</span>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    </div>
    <span class="sep" aria-hidden="true"></span>
    {/if}
    <div class="grp" role="group" aria-label="Insert">
      <button type="button" class="tb" title="Link ({MOD}K)" aria-label="Link" onmousedown={noSteal} onclick={() => apply(mdLink)}>
        <Icon name="link" size={15} />
      </button>
      <div class="split">
        <button
          type="button"
          class="tb popbtn swatchbtn"
          class:on={pop === "color"}
          aria-label="Text colour"
          aria-expanded={pop === "color"}
          title="Text colour"
          onmousedown={noSteal}
          onclick={() => (pop = pop === "color" ? "" : "color")}>
          <span class="swatch-ring" aria-hidden="true"></span>
        </button>
        {#if pop === "color"}
          <div class="pop colors" role="menu" aria-label="Text colour">
            <div class="pop-head">Colour the selection</div>
            <div class="swatches">
              {#each PALETTE as [name, hex] (name)}
                <button
                  type="button"
                  class="sw"
                  style="--sw:{hex}"
                  role="menuitem"
                  title={name}
                  aria-label={name}
                  onmousedown={noSteal}
                  onclick={() => apply((t, s, e) => colorize(t, s, e, name))}></button>
              {/each}
            </div>
            <div class="pop-row">
              <label class="custom">
                <input type="color" bind:value={customColor} aria-label="Custom colour" />
                <span>Custom</span>
              </label>
              <button type="button" class="linkish" onmousedown={noSteal} onclick={() => apply((t, s, e) => colorize(t, s, e, customColor))}>Apply</button>
            </div>
            <!-- The way back out. A colour picker with no "none" is a trap. -->
            <button type="button" class="mi wide" role="menuitem" onmousedown={noSteal} onclick={() => apply((t, s, e) => colorize(t, s, e, ""))}>
              <Icon name="close" size={12} /><span class="mi-l">Remove colour</span>
            </button>
          </div>
        {/if}
      </div>
      <div class="split">
        <button
          type="button"
          class="tb"
          class:on={emojiOpen}
          aria-label="Emoji"
          aria-expanded={emojiOpen}
          title="Emoji"
          onmousedown={noSteal}
          onclick={() => (emojiOpen = !emojiOpen)}><Icon name="smile" size={15} /></button>
        {#if emojiOpen}
          <!-- EmojiPicker positions itself against the nearest positioned
               ancestor; .split is that ancestor, and the override below drops it
               BELOW the toolbar instead of above it (its default is anchored to
               a composer that sits at the bottom of the screen). -->
          <div class="epwrap">
            <EmojiPicker onPick={pickEmoji} onClose={() => (emojiOpen = false)} />
          </div>
        {/if}
      </div>
      {#if attachments}
        <button
          type="button"
          class="tb"
          title="Attach an image or file (or paste / drop one)"
          aria-label="Attach a file"
          disabled={reading > 0}
          onmousedown={noSteal}
          onclick={() => fileInput?.click()}><Icon name="attach" size={15} /></button>
      {/if}
      {@render toolbarExtra?.()}
      {#if showFmt}
        <!-- The overflow menu rides with the insert tools rather than sitting in
             its own group: on a phone each group is its own row, and a row
             holding one button is a row wasted. -->
        <div class="split">
          <button
            type="button"
            class="tb popbtn"
            class:on={pop === "more"}
            aria-label="More formatting"
            aria-expanded={pop === "more"}
            title="More formatting"
            onmousedown={noSteal}
            onclick={() => (pop = pop === "more" ? "" : "more")}><Icon name="dots" size={15} /></button>
          {#if pop === "more"}
            <div class="pop menu" role="menu">
              {#each MORE as t (t.id)}
                <button type="button" class="mi" role="menuitem" onmousedown={noSteal} onclick={() => apply(t.run)}>
                  <Icon name={t.icon} size={13} /><span class="mi-l">{t.label}</span>
                  {#if t.key}<kbd>{MOD}{t.key}</kbd>{/if}
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
      {#if !wideView}
        <button
          type="button"
          class="tb aa"
          class:on={fmtOpen}
          aria-pressed={fmtOpen}
          aria-label="Formatting marks"
          title="Bold, italic, headings, lists…"
          onmousedown={noSteal}
          onclick={() => (fmtOpen = !fmtOpen)}>Aa</button>
      {/if}
    </div>

    <!-- The view switcher. Three states, not a pile of toggles: what you see is
         one choice. Split collapses out below 760px, where two columns is a
         lie. -->
    <div class="grp right" role="group" aria-label="View">
      <div class="seg" role="group" aria-label="View mode">
        <button type="button" class="segb" class:on={view === "write" && !zen} aria-pressed={view === "write"} onclick={() => ((mode = "write"), (zen = false))}>Write</button>
        <button type="button" class="segb wide-only" class:on={view === "split" && !zen} aria-pressed={view === "split"} onclick={() => ((mode = "split"), (zen = false))}>Split</button>
        <button type="button" class="segb" class:on={view === "preview" && !zen} aria-pressed={view === "preview"} onclick={() => ((mode = "preview"), (zen = false))}>Preview</button>
      </div>
      <!-- A word, not a glyph: the spoiler tool is already an eye-with-a-slash,
           and two eyes on one toolbar meaning different things is a puzzle. -->
      <button
        type="button"
        class="segb solo"
        class:on={zen}
        aria-pressed={zen}
        title={zen ? "Leave focus mode (Esc)" : "Focus mode — hide everything but the text"}
        aria-label="Focus mode"
        onclick={() => (zen = !zen)}>Focus</button>
    </div>
  </div>

  {#if attachments}
    <input
      type="file"
      bind:this={fileInput}
      style="display:none"
      onchange={(e) => {
        [...(e.target.files || [])].forEach(attachFile);
        e.target.value = "";
      }} />
  {/if}

  {#if attachments && (pending.length > 0 || reading > 0)}
    <div class="tray">
      {#each pending as p (p.id)}
        <div class="chip" class:file={!p.isImage}>
          {#if p.isImage}
            <img src={p.dataUrl} alt="" class:blur={p.spoiler} />
            {#if p.spoiler}<span class="tag">SPOILER</span>{/if}
          {:else}
            <span class="fmeta">
              <Icon name={p.isVideo ? "play" : "attach"} size={15} />
              <span class="fname">{p.name}</span>
              {#if p.bytes}<span class="fsize">{prettyBytes(p.bytes)}</span>{/if}
            </span>
          {/if}
          <div class="tools">
            {#if p.isImage}
              <button
                type="button"
                class="tool"
                class:on={p.spoiler}
                aria-pressed={!!p.spoiler}
                title={p.spoiler ? "Not a spoiler" : "Mark as spoiler"}
                aria-label={p.spoiler ? "Not a spoiler" : "Mark as spoiler"}
                onclick={() => setAtt(p.id, "spoiler", !p.spoiler)}><Icon name="spoiler" size={12} /></button>
            {/if}
            <button
              type="button"
              class="tool"
              class:on={editingAtt === p.id}
              title="Name and description"
              aria-label="Name and description"
              onclick={() => (editingAtt = editingAtt === p.id ? "" : p.id)}><Icon name="edit" size={12} /></button>
            <button type="button" class="tool danger" title="Remove" aria-label="Remove attachment" onclick={() => removeAtt(p.id)}>
              <Icon name="trash" size={12} />
            </button>
          </div>
        </div>
      {/each}
      {#if reading > 0}<div class="chip loading"><span class="spin"></span></div>{/if}
    </div>
    {#if attachNote}<p class="attnote">{attachNote}</p>{/if}
    {#if editingAtt && pending.some((p) => p.id === editingAtt)}
      {@const p = pending.find((x) => x.id === editingAtt)}
      <div class="attedit">
        <label>
          <span>File name</span>
          <!-- Placeholder, NOT value: an untouched field must stay empty so the
               send path can still use the v1 token older peers can render. -->
          <input value={p.name || ""} oninput={(e) => setAtt(p.id, "name", e.currentTarget.value)} placeholder={p.origName || "image.png"} />
        </label>
        <label>
          <span>Description</span>
          <input
            value={p.desc || ""}
            maxlength="500"
            oninput={(e) => setAtt(p.id, "desc", e.currentTarget.value)}
            placeholder="Describe it for people who can't see it" />
        </label>
        <button type="button" class="done" onclick={() => (editingAtt = "")}>Done</button>
      </div>
    {/if}
  {/if}

  <div class="work" class:split={showPreview && showEditor}>
    {#if showEditor}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="pane edit"
        class:focused
        class:dropping
        ondragover={(e) => {
          if (attachments) {
            e.preventDefault();
            dropping = true;
          }
        }}
        ondragleave={() => (dropping = false)}
        ondrop={onDrop}>
        <!-- svelte-ignore a11y_autofocus -->
        <textarea
          bind:this={ta}
          bind:value={body}
          class="draft"
          {placeholder}
          autofocus={autofocus && !S.isMobile}
          oninput={handleInput}
          onkeydown={onKeydown}
          onpaste={onPaste}
          onfocus={() => (focused = true)}
          onblur={() => {
            focused = false;
            setTimeout(() => (suggest = null), 150);
          }}></textarea>
        {#if suggest}
          <div class="suggest">
            <div class="s-head">{SUGGEST_HEADS[suggest.kind]}</div>
            {#each suggest.items as item, i (suggest.kind === "emoji" ? item[0] : item.key)}
              <button type="button" class="s-item" class:sel={i === suggest.sel} onmousedown={noSteal} onclick={() => accept(i)}>
                {#if suggest.kind === "emoji"}
                  <span class="s-glyph">{item[1]}</span> :{item[0]}:
                {:else if suggest.kind === "channel"}
                  <span class="s-glyph">#</span>{item.name}
                {:else}
                  <span class="s-glyph">@</span><span style={roleTint(item)}>{item.name}</span>
                  {#if item.kind === "role"}<span class="s-note">role</span>{/if}
                {/if}
                <kbd class="s-enter" aria-hidden="true">↵</kbd>
              </button>
            {/each}
          </div>
        {/if}
        {#if dropping}<div class="dropnote"><Icon name="attach" size={18} /> Drop to attach</div>{/if}
      </div>
    {/if}

    {#if showPreview}
      <div class="pane preview">
        <div class="plabel"><span class="dot" aria-hidden="true"></span> Live preview</div>
        <div class="pbody">
          {#if previewEmpty}
            <p class="empty">Nothing yet. What you write appears here rendered exactly as everyone else will see it — same markdown, same emoji, same embed.</p>
          {:else}
            <div class="pmsg">
              <Avatar name={me.name} emoji={me.emoji} color={me.color} image={me.avatar} size={38} />
              <div class="pcol">
                <div class="phead"><span class="pname">{me.name || "You"}</span><span class="ptime">now</span></div>
                {#if previewTitle}<div class="ptitle">{previewTitle}</div>{/if}
                {#if hasBody}
                  <!-- The SAME renderer the feed uses, with the same mention and
                       emoji tables — the only way a preview can be trusted. -->
                  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                  <div class="md">{@html renderMarkdown(previewBody, mentionNames, cemoji, refs)}</div>
                {/if}
                {#each pending as p (p.id)}
                  {#if p.isImage}
                    <img class="patt" class:blur={p.spoiler} src={p.dataUrl} alt={p.desc || ""} />
                  {:else}
                    <span class="pfile"><Icon name="attach" size={13} />{p.name}</span>
                  {/if}
                {/each}
                {@render previewExtra?.()}
              </div>
            </div>
          {/if}
        </div>
      </div>
    {/if}
  </div>

  <div class="status">
    <span class="counts" class:over={stats.over}>
      <strong>{stats.words}</strong> word{stats.words === 1 ? "" : "s"}
      <span class="dotsep" aria-hidden="true">·</span>
      <strong>{stats.chars}</strong> character{stats.chars === 1 ? "" : "s"}
      {#if stats.minutes}
        <span class="dotsep" aria-hidden="true">·</span>{stats.minutes} min read
      {/if}
    </span>
    {#if stats.over}
      <span class="warn"><Icon name="alert" size={12} /> Long enough that people will skim it — consider a heading or two.</span>
    {/if}
    {#if submitHint}<span class="hint">{submitHint}</span>{/if}
  </div>
</div>

<style>
  /* Two layout regimes, because the dialog has two:
     · WIDE — the dialog is a fixed-height workspace, so this column negotiates
       within it: min-height:0 lets the panes take the slack and give it back to
       the embed panel, and nothing ever scrolls.
     · NARROW — the dialog is an auto-height sheet that scrolls. There, flex
       negotiation is actively harmful: min-height:0 let the sheet crush the
       toolbar, tray, editor and preview to nothing and they painted on top of
       each other (a block box does not clip), while leaving it at min-content
       made a wrapping toolbar's intrinsic height balloon the column and the
       panes grew to 700px. `flex: none` on the phone sidesteps both: every
       block is its natural height and the sheet scrolls. */
  .rx {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    gap: 10px;
    text-align: left;
  }
  @media (max-width: 760px), (pointer: coarse) {
    .rx {
      flex: none;
      min-height: auto;
    }
  }

  /* ---- toolbar ---------------------------------------------------------- */
  /* One rhythm everywhere: 10px between blocks, 6px inside a block, 2px
     between siblings in a group. The grouping is the hierarchy — separators
     carry it, not colour. */
  .bar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    padding: 6px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .grp {
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .grp.right {
    margin-left: auto;
  }
  .sep {
    width: 1px;
    align-self: stretch;
    margin: 2px 4px;
    background: var(--border);
  }
  .tb {
    width: 30px;
    height: 30px;
    display: grid;
    place-items: center;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    transition:
      background 0.12s ease,
      color 0.12s ease;
  }
  .tb:hover:not(:disabled) {
    background: var(--bg-3);
    color: var(--text);
  }
  .tb:disabled {
    opacity: 0.4;
  }
  /* An active tool is the ONE place accent appears in the toolbar, which is why
     it means something. */
  .tb.on {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .split {
    position: relative;
    display: flex;
    align-items: center;
  }
  .caret {
    width: 14px;
    height: 30px;
    display: grid;
    place-items: center;
    padding: 0;
    background: transparent;
    color: var(--text-faint);
    border-radius: var(--radius-sm);
  }
  .caret :global(svg) {
    transform: rotate(90deg);
  }
  .caret:hover,
  .caret.on {
    background: var(--bg-3);
    color: var(--text);
  }
  .bar .aa {
    width: auto;
    padding: 0 9px;
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.01em;
  }
  .swatchbtn .swatch-ring {
    width: 15px;
    height: 15px;
    border-radius: 50%;
    /* The colour tool wears the palette it opens — a conic sample, so it reads
       as "colour" without a glyph that would need explaining. */
    background: conic-gradient(#e0555b, #e8873c, #e0b341, #3ba55d, #2dd4bf, #4b8bf5, #a06bff, #eb6f9e, #e0555b);
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.3);
  }

  /* Segmented view switcher. */
  .seg {
    display: flex;
    padding: 2px;
    background: var(--bg-3);
    border-radius: var(--radius-sm);
  }
  .segb {
    padding: 4px 10px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text-muted);
    background: transparent;
    border-radius: 4px;
    transition:
      background 0.14s ease,
      color 0.14s ease;
  }
  .segb:hover {
    color: var(--text);
  }
  .segb.on {
    background: var(--bg-1);
    color: var(--text);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.22);
  }
  /* Focus is a mode of its own, not a fourth view — same type, its own well. */
  .segb.solo {
    margin-left: 4px;
    background: var(--bg-3);
  }
  .segb.solo.on {
    background: var(--accent-soft);
    color: var(--accent-hover);
    box-shadow: none;
  }

  /* ---- popovers -------------------------------------------------------- */
  .pop {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    z-index: 40;
    min-width: 190px;
    padding: 5px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    transform-origin: top left;
    animation: pop-in 0.13s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  .pop.narrow {
    min-width: 120px;
    max-height: 260px;
    overflow-y: auto;
  }
  @keyframes pop-in {
    from {
      opacity: 0;
      transform: translateY(-4px) scale(0.98);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pop,
    .suggest {
      animation: none;
    }
  }
  .pop-head {
    padding: 4px 8px 6px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.07em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .mi {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 7px 9px;
    font-size: 13px;
    color: var(--text);
    background: transparent;
    border-radius: var(--radius-sm);
    text-align: left;
  }
  .mi:hover {
    background: var(--bg-3);
  }
  .mi :global(svg) {
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .mi-l {
    flex: 1;
    min-width: 0;
  }
  .mi-l.mono {
    font-family: var(--mono-font, monospace);
  }
  .mi-h {
    width: 22px;
    font-weight: 700;
    color: var(--text-muted);
  }
  .mi kbd {
    font-family: inherit;
    font-size: 11px;
    color: var(--text-faint);
    white-space: nowrap;
  }
  .mi.wide {
    margin-top: 3px;
    border-top: 1px solid var(--border);
    border-radius: 0 0 var(--radius-sm) var(--radius-sm);
    padding-top: 8px;
  }
  .colors {
    min-width: 216px;
  }
  .swatches {
    display: grid;
    grid-template-columns: repeat(9, 1fr);
    gap: 4px;
    padding: 0 6px 6px;
  }
  /* Descendant selector on purpose: Modal's mobile sheet puts a 44px min-height
     on `.dialog :global(button)`, which outranks a bare `.sw` — and a swatch is
     a dot, so 20×44 renders as an ellipse. */
  .swatches .sw {
    min-height: 18px;
    width: 18px;
    height: 18px;
    padding: 0;
    border-radius: 50%;
    background: var(--sw);
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.28);
    transition: transform 0.12s ease;
  }
  .swatches .sw:hover {
    transform: scale(1.18);
  }
  .pop-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 8px 4px;
  }
  .custom {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--text-muted);
  }
  /* Four classes deep on purpose: Modal's mobile sheet puts a 44px min-height on
     `.dialog :global(input:not([type=checkbox]):not([type=radio]))`, whose
     specificity (0,3,1) beats a plain `.custom input[type=color]` — and a
     44px-tall colour dot is a lozenge. */
  .pop .pop-row .custom input[type="color"] {
    width: 26px;
    height: 26px;
    min-height: 26px;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: none;
    cursor: pointer;
  }
  .linkish {
    margin-left: auto;
    padding: 4px 8px;
    font-size: 12px;
    font-weight: 600;
    color: var(--accent-hover);
    background: transparent;
    border-radius: var(--radius-sm);
  }
  .linkish:hover {
    background: var(--accent-soft);
  }
  /* EmojiPicker anchors itself to the nearest positioned ancestor with
     `bottom: 54px; right: 12px` — correct above a bottom-docked composer, wrong
     under a toolbar. Re-anchor it downward.
     Scoped to exactly the case where the picker is a floating card: its own
     mobile rule (`@media (pointer: coarse), (max-width: 700px)`) turns it into a
     fixed bottom sheet, and this override has the higher specificity, so
     applying it unconditionally would drag that sheet back into the middle of
     the toolbar. The condition below is the complement of that rule. */
  @media (pointer: fine) and (min-width: 701px) {
    .epwrap :global(.picker) {
      top: calc(100% + 6px);
      bottom: auto;
      right: auto;
      left: -190px;
      transform-origin: top left;
    }
  }
  .attnote {
    margin: 0;
    font-size: 11.5px;
    color: var(--text-muted);
  }

  /* ---- attachment tray ------------------------------------------------- */
  .tray {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .chip {
    position: relative;
    border-radius: 8px;
    overflow: hidden;
    background: var(--bg-1);
    border: 1px solid var(--border);
  }
  .chip img {
    display: block;
    width: 68px;
    height: 68px;
    object-fit: cover;
    transition: filter 0.18s ease;
  }
  /* The staged preview blurs too, so "spoiler" is a state you can SEE before
     you send rather than a promise about what the other side gets. */
  .chip img.blur {
    filter: blur(8px);
  }
  .chip.file {
    display: flex;
    align-items: center;
    min-height: 68px;
    padding: 8px 34px 8px 10px;
    max-width: 220px;
  }
  .fmeta {
    display: flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
    font-size: 12px;
    color: var(--text);
  }
  .fmeta :global(svg) {
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  .fname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .fsize {
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
    flex-shrink: 0;
  }
  .tag {
    position: absolute;
    left: 3px;
    bottom: 3px;
    padding: 0 4px;
    border-radius: 3px;
    background: rgba(0, 0, 0, 0.72);
    color: #fff;
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.4px;
    pointer-events: none;
  }
  .tools {
    position: absolute;
    top: 3px;
    right: 3px;
    display: flex;
    gap: 2px;
    opacity: 0;
    transition: opacity 0.14s ease;
  }
  .chip:hover .tools,
  .chip:focus-within .tools {
    opacity: 1;
  }
  /* No hover on a touchscreen, so the controls are simply always there. */
  @media (pointer: coarse) {
    .tools {
      opacity: 1;
    }
  }
  /* Descendant selector, same reason as the swatches: the sheet's 44px button
     floor would turn a 19px overlay control into a bar across the thumbnail. */
  .tools .tool {
    width: 19px;
    height: 19px;
    min-height: 19px;
    display: grid;
    place-items: center;
    padding: 0;
    border-radius: 4px;
    background: rgba(0, 0, 0, 0.68);
    color: #fff;
  }
  .tools .tool:hover {
    background: rgba(0, 0, 0, 0.88);
  }
  .tools .tool.on {
    background: var(--accent);
  }
  .tools .tool.danger:hover {
    background: var(--danger);
  }
  .chip.loading {
    width: 68px;
    height: 68px;
    display: grid;
    place-items: center;
  }
  .spin {
    width: 18px;
    height: 18px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  /* app.css already neutralises every animation under prefers-reduced-motion
     (duration 0.01ms + iteration-count 1, both !important), so this spinner
     needs no rule of its own — and could not override it if it wanted to. */
  .attedit {
    display: flex;
    align-items: flex-end;
    gap: 8px;
    flex-wrap: wrap;
    padding: 9px;
    border-radius: var(--radius-sm);
    background: var(--bg-1);
    border: 1px solid var(--border);
  }
  .attedit label {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: 1;
    min-width: 140px;
  }
  .attedit span {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .done {
    padding: 6px 12px;
    font-size: 12px;
    font-weight: 600;
  }

  /* ---- workspace -------------------------------------------------------- */
  .work {
    flex: 1;
    /* A FLOOR, not zero. When the embed builder opens below, a flex column with
       min-height:0 happily crushes the editor to 90px; with a floor the column
       overflows instead and the dialog (overflow-y:auto) scrolls — the editor
       never becomes unusable to make room for an optional panel. */
    min-height: var(--work-min, 200px);
    display: grid;
    grid-template-columns: 1fr;
    gap: 12px;
    /* Declared, not assumed: measured in a real browser, leaving this to the
       initial value left both panes at content height and vertically CENTRED in
       a 484px row — a 200px editor floating in the middle of the dialog. */
    align-items: stretch;
    align-content: stretch;
  }
  /* 54/46: the editor is the subject, the preview is the confirmation. */
  .work.split {
    grid-template-columns: 54fr 46fr;
  }
  .pane {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    align-self: stretch;
  }
  .edit {
    position: relative;
    /* Clip, because the textarea inside is the thing that must scroll. Without
       this, a pane shrunk below the textarea's height paints the draft straight
       over whatever is underneath. */
    overflow: hidden;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    transition:
      border-color 0.15s ease,
      box-shadow 0.15s ease;
  }
  .edit.focused {
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent);
  }
  .edit.dropping {
    border-color: var(--accent);
    border-style: dashed;
  }
  .draft {
    flex: 1;
    min-height: 0;
    width: 100%;
    resize: none;
    background: transparent !important;
    border: none !important;
    box-shadow: none !important;
    outline: none !important;
    padding: 16px 18px;
    font-family: inherit;
    font-size: 15px;
    line-height: 1.7;
    color: var(--text);
  }
  .dropnote {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    gap: 6px;
    grid-auto-flow: column;
    font-size: 13px;
    font-weight: 600;
    color: var(--accent-hover);
    background: color-mix(in srgb, var(--accent) 10%, var(--bg-1));
    border-radius: var(--radius-md);
    pointer-events: none;
  }

  /* Focus mode: nothing moves, things simply stop being drawn. The measure
     narrows to a readable column and the type grows a step. */
  .rx.zen .bar .grp:not(.right),
  .rx.zen .status .counts {
    opacity: 0.35;
    transition: opacity 0.2s ease;
  }
  .rx.zen .bar:hover .grp,
  .rx.zen .bar:focus-within .grp {
    opacity: 1;
  }
  .rx.zen .draft {
    font-size: 16.5px;
    line-height: 1.85;
    max-width: 68ch;
    margin: 0 auto;
  }

  /* ---- autocomplete ---------------------------------------------------- */
  .suggest {
    position: absolute;
    left: 12px;
    bottom: 12px;
    z-index: 30;
    min-width: 240px;
    max-width: calc(100% - 24px);
    padding: 4px;
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    transform-origin: bottom left;
    animation: pop-in 0.13s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  .s-head {
    padding: 4px 10px 3px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .s-item {
    display: flex;
    align-items: center;
    padding: 6px 10px;
    font-size: 13px;
    font-family: var(--mono-font, monospace);
    color: var(--text);
    background: transparent;
    border-radius: var(--radius-sm);
    text-align: left;
  }
  .s-item.sel,
  .s-item:hover {
    background: var(--bg-3);
  }
  .s-item.sel {
    background: color-mix(in srgb, var(--accent) 12%, var(--bg-3));
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .s-glyph {
    font-size: 15px;
    margin-right: 6px;
  }
  .s-note {
    margin-left: 8px;
    font-family: var(--ui-font, sans-serif);
    font-size: 11px;
    color: var(--text-muted);
  }
  .s-enter {
    margin-left: auto;
    padding-left: 12px;
    font-family: inherit;
    font-size: 11px;
    color: var(--accent-hover);
    opacity: 0;
    transition: opacity 0.12s ease;
  }
  .s-item.sel .s-enter {
    opacity: 0.9;
  }

  /* ---- preview --------------------------------------------------------- */
  .plabel {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 8px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .plabel .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--ok);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 22%, transparent);
  }
  .pbody {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .pmsg {
    display: flex;
    gap: 11px;
  }
  .pcol {
    flex: 1;
    min-width: 0;
  }
  .phead {
    display: flex;
    align-items: baseline;
    gap: 7px;
    margin-bottom: 2px;
  }
  .pname {
    font-weight: 600;
    font-size: 14px;
  }
  .ptime {
    font-size: 11px;
    color: var(--text-muted);
  }
  .ptitle {
    font-size: 17px;
    font-weight: 700;
    line-height: 1.25;
    letter-spacing: -0.01em;
    margin: 2px 0 6px;
    overflow-wrap: anywhere;
  }
  .empty {
    margin: 4px 0 0;
    font-size: 13px;
    line-height: 1.6;
    color: var(--text-muted);
  }
  .patt {
    display: block;
    max-width: 100%;
    max-height: 200px;
    margin-top: 6px;
    border-radius: var(--radius-sm);
  }
  .patt.blur {
    filter: blur(10px);
  }
  .pfile {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
    padding: 5px 9px;
    font-size: 12px;
    color: var(--text-muted);
    background: var(--bg-3);
    border-radius: var(--radius-sm);
  }

  /* The rendered-markdown styles. Message.svelte owns the canonical set; this is
     the subset a preview needs, and it is here because a preview that styles
     code and quotes differently from the feed is not a preview. (A shared
     stylesheet in app.css would be better — see the report.) */
  .md {
    font-size: 14.5px;
    line-height: 1.55;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }
  .md :global(code) {
    background: var(--bg-3);
    padding: 1px 5px;
    border-radius: 4px;
    font-family: var(--mono-font, monospace);
    font-size: 12px;
  }
  .md :global(pre) {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    overflow-x: auto;
    margin: 4px 0;
    white-space: pre;
  }
  .md :global(pre code) {
    background: transparent;
    padding: 0;
  }
  .md :global(pre[data-lang]) {
    position: relative;
    padding-top: 20px;
  }
  .md :global(pre[data-lang])::before {
    content: attr(data-lang);
    position: absolute;
    top: 3px;
    right: 8px;
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .md :global(blockquote) {
    margin: 2px 0;
    padding: 0 0 0 8px;
    border-left: 3px solid var(--border);
    color: var(--text-muted);
  }
  .md :global(ul),
  .md :global(ol) {
    margin: 2px 0;
    padding-left: 22px;
  }
  .md :global(a) {
    color: var(--accent-hover);
    text-decoration: none;
  }
  .md :global(.md-h) {
    margin: 4px 0 2px;
    font-weight: 700;
    line-height: 1.25;
  }
  .md :global(h3.md-h) {
    font-size: 1.25em;
  }
  .md :global(h4.md-h) {
    font-size: 1.1em;
  }
  .md :global(h5.md-h) {
    font-size: 1em;
  }
  .md :global(u) {
    text-decoration: underline;
  }
  .md :global(s) {
    text-decoration: line-through;
    opacity: 0.85;
  }
  .md :global(.mention) {
    background: color-mix(in srgb, var(--text-muted) 22%, transparent);
    color: var(--text);
    border-radius: 4px;
    padding: 0 3px;
    font-weight: 600;
  }
  .md :global(.mention-self) {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  /* Spoilers stay hidden in the preview: that IS what it looks like. */
  .md :global(.spoiler) {
    filter: blur(0.35em);
    background: color-mix(in srgb, var(--text-muted) 16%, transparent);
    border-radius: 4px;
    padding: 0 3px;
    user-select: none;
  }
  .md :global(img.emoji) {
    width: 1.375em;
    height: 1.375em;
    vertical-align: -0.3em;
    margin: 0 0.5px;
    object-fit: contain;
  }
  .md :global(img.cemoji) {
    height: 1.375em;
    width: auto;
    vertical-align: -0.2em;
    margin: 0 1px;
    object-fit: contain;
  }
  .md :global(img.attachment) {
    max-width: 100%;
    border-radius: var(--radius-sm);
  }

  /* ---- status line ----------------------------------------------------- */
  .status {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    font-size: 11.5px;
    color: var(--text-muted);
  }
  .counts strong {
    color: var(--text);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .counts.over strong {
    color: var(--warn-text);
  }
  .dotsep {
    margin: 0 5px;
    opacity: 0.5;
  }
  .warn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--warn-text);
  }
  .hint {
    margin-left: auto;
    color: var(--text-faint);
    white-space: nowrap;
  }

  /* ---- narrow ---------------------------------------------------------- */
  @media (max-width: 760px) {
    /* Two columns at 390px is a lie: the switcher drops to Write/Preview and
       the panes stack, sized by their content so the sheet scrolls instead of
       the panes fighting over a fixed height. */
    .wide-only {
      display: none;
    }
    .work.split {
      grid-template-columns: 1fr;
    }
    .bar {
      gap: 4px;
    }
    .grp.right {
      margin-left: 0;
      width: 100%;
      justify-content: space-between;
    }
    /* Modal's sheet floor makes every button 44px tall on touch. That floor is
       right — these are thumb targets — so the toolbar does NOT fight it; it
       widens to match instead, because a 30×44 tool is a sliver. Two wrapped
       rows of proper targets beats one row of misses. */
    .rx .bar .tb {
      width: 40px;
    }
    .rx .bar .caret {
      width: 22px;
    }
    /* Each .grp is one flex item, so it wraps as a unit: on a phone the rows ARE
       the groups. The separators then land at row ends as stray marks, saying
       nothing the line break isn't already saying. */
    .sep {
      display: none;
    }
    .draft {
      font-size: 16px; /* ≥16px stops iOS zooming the sheet on focus */
    }
  }
</style>
