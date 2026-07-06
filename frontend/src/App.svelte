<script>
  import { api, on } from "./lib/api.js";
  import { VoiceMesh } from "./lib/voice.js";
  import Login from "./Login.svelte";
  import ModalCreate from "./modals/ModalCreate.svelte";
  import ModalJoin from "./modals/ModalJoin.svelte";
  import ModalInvite from "./modals/ModalInvite.svelte";
  import ModalProfile from "./modals/ModalProfile.svelte";

  let ready = $state(false);
  let identity = $state({ peerId: "", fingerprint: "", displayName: "" });
  let displayName = $state("");
  let guilds = $state([]);
  let activeGuildId = $state("");
  let activeChannelId = $state("");
  let messages = $state([]);
  let members = $state([]);
  let contacts = $state([]);
  let draft = $state("");
  let toast = $state("");
  let replyingTo = $state(null); // message being replied to
  let unreadChannels = $state({}); // channelId -> true
  let showPins = $state(false);
  let searchQuery = $state("");
  let searchResults = $state(null); // null = closed, [] = no hits

  const pinnedMessages = $derived(messages.filter((m) => m.pinned && !m.deleted));

  // memberByFpr powers avatars: color + emoji come from the sender's profile.
  function memberByFpr(fpr) {
    return members.find((x) => x.fingerprint === fpr);
  }
  function avatarStyle(fpr) {
    const c = memberByFpr(fpr)?.color;
    return c ? `background:${c}` : "";
  }
  function avatarGlyph(fpr, fallbackName) {
    const mem = memberByFpr(fpr);
    return mem?.emoji || (mem?.name || fallbackName || "?").slice(0, 2);
  }

  function applyAccent(color) {
    if (!color) return;
    document.documentElement.style.setProperty("--accent", color);
  }

  async function runSearch(e) {
    e?.preventDefault();
    const q = searchQuery.trim();
    if (!q) {
      searchResults = null;
      return;
    }
    try {
      searchResults = (await api.searchMessages(q)) || [];
    } catch (err) {
      flash(String(err?.message || err));
    }
  }

  function channelName(chId) {
    for (const g of guilds) {
      const c = g.channels.find((x) => x.id === chId);
      if (c) return `${g.name} #${c.name}`;
    }
    return "unknown channel";
  }

  async function openSearchResult(m) {
    searchResults = null;
    searchQuery = "";
    for (const g of guilds) {
      if (g.channels.some((c) => c.id === m.channelId)) {
        if (activeGuildId !== g.id) await selectGuild(g.id);
        await selectChannel(m.channelId);
        return;
      }
    }
  }

  function exportChannel() {
    if (!activeChannel) return;
    const lines = messages
      .filter((m) => !m.deleted)
      .map((m) =>
        m.kind === "system"
          ? `> ✨ ${m.senderName || m.sender} ${m.content}`
          : `**${m.senderName || m.sender}** (${m.sent}):\n${m.content}\n`,
      );
    const blob = new Blob([`# ${channelName(activeChannelId)}\n\n` + lines.join("\n")], {
      type: "text/markdown",
    });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `${activeChannel.name}-history.md`;
    a.click();
    URL.revokeObjectURL(a.href);
    flash("History exported");
  }

  function msgById(id) {
    return messages.find((m) => m.id === id);
  }

  function guildHasUnread(g) {
    return g.channels.some((c) => unreadChannels[c.id]);
  }

  function escapeHtml(s) {
    return s.replace(
      /[&<>"']/g,
      (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c],
    );
  }

  // Minimal, XSS-safe markdown: escape first, then add our own safe tags.
  function renderContent(text) {
    let s = escapeHtml(text);
    // Inline image attachments (strict data-URI whitelist, so no script URIs).
    s = s.replace(
      /!\[image\]\((data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+)\)/g,
      '<img class="attachment" src="$1" alt="attachment" />',
    );
    s = s.replace(/`([^`]+)`/g, "<code>$1</code>");
    s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<em>$2</em>");
    s = s.replace(
      /(https?:\/\/[^\s<]+)/g,
      '<a href="$1" target="_blank" rel="noopener noreferrer">$1</a>',
    );
    return s;
  }

  // Voice state
  let voice = $state(null); // { mesh, channelId }
  let voiceParticipants = $state([]); // peer IDs
  let voiceSpeaking = $state([]); // keys currently speaking: "self" or peerId
  let voicePeerFpr = $state({}); // peerId -> fingerprint
  let muted = $state(false);

  function voiceName(peerId) {
    const fpr = voicePeerFpr[peerId];
    const mem = members.find((m) => m.fingerprint === fpr);
    return mem?.name || (fpr ? fpr.slice(0, 9) : peerId.slice(0, 8));
  }

  // Typing state
  let typingList = $state([]); // [{ from, timer }]
  let lastTypingSent = 0;
  const typingLabel = $derived(
    typingList.length === 0
      ? ""
      : typingList.length === 1
        ? `${typingList[0].label} is typing…`
        : `${typingList.length} people are typing…`,
  );

  // Simple modal state: { kind, ... }
  let modal = $state(null);

  const activeGuild = $derived(guilds.find((g) => g.id === activeGuildId) || null);
  const activeChannel = $derived(
    activeGuild?.channels.find((c) => c.id === activeChannelId) || null,
  );

  async function onLogin() {
    identity = await api.identity();
    displayName = identity.displayName || "";
    applyAccent(identity.color);
    if (typeof Notification !== "undefined" && Notification.permission === "default") {
      Notification.requestPermission().catch(() => {});
    }
    await refreshGuilds();
    ready = true;

    // Live message feed.
    on("message", (m) => {
      if (m.channelId === activeChannelId) {
        const i = messages.findIndex((x) => x.id === m.id);
        if (i >= 0) {
          messages = messages.map((x) => (x.id === m.id ? m : x)); // update (e.g. delete)
        } else {
          messages = [...messages, m];
          scrollSoon();
        }
      } else if (m.channelId) {
        unreadChannels = { ...unreadChannels, [m.channelId]: true };
      }
      // Desktop notification when the window isn't focused (normal chat only).
      if (
        m.kind === "" &&
        !m.deleted &&
        m.sender !== identity.fingerprint &&
        typeof Notification !== "undefined" &&
        Notification.permission === "granted" &&
        document.hidden
      ) {
        new Notification(m.senderName || m.sender.slice(0, 9), { body: m.content.slice(0, 120) });
      }
    });
    on("presence", () => refreshRightPanel());

    // Route voice signaling to the active mesh.
    on("voice-presence", (v) => {
      if (voice && v.channelId === voice.channelId) {
        if (v.action === "join") {
          voicePeerFpr = { ...voicePeerFpr, [v.from]: v.fingerprint };
        } else {
          const c = { ...voicePeerFpr };
          delete c[v.from];
          voicePeerFpr = c;
        }
        voice.mesh.handlePresence(v.from, v.action);
      }
    });
    on("voice-signal", (v) => {
      if (voice) voice.mesh.handleSignal(v.from, v.data);
    });
    on("guild-updated", async () => {
      await refreshGuilds();
      await refreshRightPanel();
    });
    on("typing", (t) => {
      if (t.channelId !== activeChannelId) return;
      const label = t.name || (t.from || "").slice(0, 9);
      typingList = typingList.filter((x) => x.from !== t.from);
      const timer = setTimeout(() => {
        typingList = typingList.filter((x) => x.from !== t.from);
      }, 4000);
      typingList = [...typingList, { from: t.from, label, timer }];
    });
  }

  function onDraftInput() {
    const now = Date.now();
    if (now - lastTypingSent > 2000 && activeChannelId) {
      lastTypingSent = now;
      api.sendTyping(activeChannelId).catch(() => {});
    }
  }

  async function joinVoice() {
    if (!activeChannelId || voice) return;
    const mesh = new VoiceMesh({
      selfPeerId: identity.peerId,
      channelId: activeChannelId,
      relay: api.relaySignal,
      onRoster: (ids) => (voiceParticipants = ids),
      onSpeaking: (keys) => (voiceSpeaking = keys),
    });
    try {
      await mesh.start();
    } catch {
      flash("Microphone access denied");
      return;
    }
    voice = { mesh, channelId: activeChannelId };
    await api.joinVoice(activeChannelId);
    flash("Joined voice");
  }

  async function leaveVoice() {
    if (!voice) return;
    const ch = voice.channelId;
    voice.mesh.stop();
    voice = null;
    voiceParticipants = [];
    voiceSpeaking = [];
    voicePeerFpr = {};
    muted = false;
    await api.leaveVoice(ch);
  }

  function toggleMute() {
    muted = !muted;
    voice?.mesh.setMuted(muted);
  }

  async function saveProfile(p) {
    await api.setProfile(p.name, p.status, p.emoji, p.color);
    identity = await api.identity();
    displayName = identity.displayName || "";
    applyAccent(identity.color);
    await refreshRightPanel();
    modal = null;
    flash("Profile updated");
  }

  async function refreshGuilds() {
    guilds = (await api.guilds()) || [];
    if (!activeGuildId && guilds.length) selectGuild(guilds[0].id);
  }

  async function selectGuild(id) {
    activeGuildId = id;
    const g = guilds.find((x) => x.id === id);
    if (g && g.channels.length) await selectChannel(g.channels[0].id);
    await refreshRightPanel();
  }

  async function selectChannel(id) {
    activeChannelId = id;
    if (unreadChannels[id]) {
      const u = { ...unreadChannels };
      delete u[id];
      unreadChannels = u;
    }
    typingList.forEach((t) => clearTimeout(t.timer));
    typingList = [];
    replyingTo = null;
    messages = (await api.messages(id)) || [];
    scrollSoon();
  }

  async function deleteMsg(m) {
    try {
      await api.deleteMessage(m.channelId, m.id);
    } catch (err) {
      flash(String(err?.message || err));
    }
  }

  let editing = $state(null); // message being edited
  let editDraft = $state("");
  function startEdit(m) {
    editing = m;
    editDraft = m.content;
  }
  async function saveEdit() {
    const text = editDraft.trim();
    const m = editing;
    editing = null;
    if (!m || !text || text === m.content) return;
    try {
      await api.editMessage(m.channelId, m.id, text);
    } catch (err) {
      flash(String(err?.message || err));
    }
  }

  // Image attachments: sent inline as data-URIs inside the E2EE message.
  const MAX_IMAGE_BYTES = 300 * 1024;
  let fileInput;
  async function attachImage(file) {
    if (!file || !file.type.startsWith("image/") || !activeChannelId) return;
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
      await api.sendMessage(activeChannelId, `![image](${dataUrl})`, "");
    } catch (err) {
      flash(String(err?.message || err));
    }
  }
  function onPaste(e) {
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (item) {
      e.preventDefault();
      attachImage(item.getAsFile());
    }
  }

  const QUICK_EMOJIS = ["👍", "❤️", "😂", "🎉"];
  async function react(m, emoji) {
    try {
      await api.toggleReaction(m.channelId, m.id, emoji);
    } catch (err) {
      flash(String(err?.message || err));
    }
  }

  async function refreshRightPanel() {
    if (activeGuildId) members = (await api.members(activeGuildId)) || [];
    contacts = (await api.contacts()) || [];
  }

  async function send(e) {
    e?.preventDefault();
    const text = draft.trim();
    if (!text || !activeChannelId) return;
    draft = "";
    const replyTo = replyingTo?.id || "";
    replyingTo = null;
    try {
      await api.sendMessage(activeChannelId, text, replyTo);
    } catch (err) {
      flash(String(err?.message || err));
    }
  }

  async function createGuild(name) {
    if (!name?.trim()) return;
    await api.createGuild(name.trim());
    await refreshGuilds();
    modal = null;
  }

  async function createChannel(name) {
    if (!name?.trim() || !activeGuildId) return;
    await api.createChannel(activeGuildId, name.trim());
    await refreshGuilds();
    modal = null;
  }

  async function renameGuild(name) {
    if (!name?.trim() || !activeGuildId) return;
    await api.renameGuild(activeGuildId, name.trim());
    await refreshGuilds();
    modal = null;
  }

  async function showInvite() {
    const code = await api.inviteCode(activeGuildId);
    modal = { kind: "invite", code };
  }

  async function joinGuild(code) {
    if (!code?.trim()) return;
    try {
      await api.joinViaInvite(code.trim());
      await refreshGuilds();
      modal = null;
    } catch (err) {
      modal = { ...modal, error: String(err?.message || err) };
    }
  }

  async function verify(peerId) {
    await api.verify(peerId);
    await refreshRightPanel();
    flash("Contact verified");
  }

  async function kick(fingerprint) {
    try {
      await api.removeMember(activeGuildId, fingerprint);
      await refreshRightPanel();
      flash("Member removed");
    } catch (err) {
      flash(String(err?.message || err));
    }
  }

  function flash(msg) {
    toast = msg;
    setTimeout(() => (toast = ""), 2500);
  }

  let feedEl;
  function scrollSoon() {
    requestAnimationFrame(() => {
      if (feedEl) feedEl.scrollTop = feedEl.scrollHeight;
    });
  }

  function copy(text) {
    navigator.clipboard?.writeText(text);
    flash("Copied to clipboard");
  }

  function fmtTime(iso) {
    try {
      return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    } catch {
      return "";
    }
  }
</script>

{#if !ready}
  <Login {onLogin} />
{:else}
  <div class="app">
    <!-- Guild + channel sidebar -->
    <aside class="sidebar">
      <div class="brand">◆ Concord</div>

      <div class="section-head">
        <span>Guilds</span>
        <button class="mini" onclick={() => (modal = { kind: "create" })} title="Create guild">+</button>
      </div>

      {#each guilds as g (g.id)}
        <button
          class="guild {g.id === activeGuildId ? 'active' : ''}"
          onclick={() => selectGuild(g.id)}
        >
          <span>{g.name}</span>
          {#if g.id !== activeGuildId && guildHasUnread(g)}<span class="unread-dot"></span>{/if}
        </button>
      {/each}

      <button class="ghost joinbtn" onclick={() => (modal = { kind: "join", code: "" })}>
        Join with invite…
      </button>

      {#if activeGuild}
        <div class="section-head">
          <span>Channels</span>
          <button class="mini" onclick={() => (modal = { kind: "channel" })} title="Add channel">+</button>
        </div>
        {#each activeGuild.channels as c (c.id)}
          <button
            class="channel {c.id === activeChannelId ? 'active' : ''}"
            onclick={() => selectChannel(c.id)}
          >
            <span># {c.name}</span>
            {#if c.id !== activeChannelId && unreadChannels[c.id]}<span class="unread-dot"></span>{/if}
          </button>
        {/each}
      {/if}

      <button class="me" onclick={() => (modal = { kind: "profile" })} title="Edit profile">
        <div class="me-avatar" style={identity.color ? `background:${identity.color}` : ""}>
          {identity.emoji || (displayName || "?").slice(0, 2)}
        </div>
        <div class="me-text">
          <strong>{displayName || "Set your name"}</strong>
          <span class="muted small-status">{identity.status || "click to edit profile"}</span>
        </div>
      </button>
    </aside>

    <!-- Chat -->
    <main class="chat">
      <header class="chat-head">
        <div>
          {#if activeChannel}<strong># {activeChannel.name}</strong>{:else}<span class="muted">No channel</span>{/if}
        </div>
        <div class="row">
          <form onsubmit={runSearch}>
            <input class="search-box" placeholder="Search…" bind:value={searchQuery} />
          </form>
          {#if activeChannel}
            <button
              class="ghost"
              class:pin-active={showPins}
              title="Pinned messages"
              onclick={() => (showPins = !showPins)}>📌 {pinnedMessages.length}</button
            >
            <button class="ghost" title="Export history" onclick={exportChannel}>⤓</button>
          {/if}
          {#if voice && voice.channelId === activeChannelId}
            <span class="voice-pill">🔊 {voiceParticipants.length + 1} in voice</span>
            <button class="ghost" onclick={toggleMute}>{muted ? "🔇 Unmute" : "🎙 Mute"}</button>
            <button class="ghost leave" onclick={leaveVoice}>Leave</button>
          {:else if activeChannel && !voice}
            <button class="ghost" onclick={joinVoice}>🔊 Join Voice</button>
          {/if}
          {#if activeGuild?.isOwner}
            <button class="ghost" onclick={showInvite}>Invite</button>
            <button class="ghost" title="Guild settings" onclick={() => (modal = { kind: "rename" })}>⚙</button>
          {/if}
        </div>
      </header>

      {#if voice && voice.channelId === activeChannelId}
        <div class="voice-panel">
          <div class="voice-tile" class:speaking={voiceSpeaking.includes("self")}>
            <div class="voice-avatar">{(displayName || "Y").slice(0, 2)}</div>
            <span>You{muted ? " 🔇" : ""}</span>
          </div>
          {#each voiceParticipants as pid (pid)}
            <div class="voice-tile" class:speaking={voiceSpeaking.includes(pid)}>
              <div class="voice-avatar">{voiceName(pid).slice(0, 2)}</div>
              <span>{voiceName(pid)}</span>
            </div>
          {/each}
        </div>
      {/if}

      {#if showPins}
        <div class="pins-panel">
          {#each pinnedMessages as m (m.id)}
            <div class="pin-item">
              <span>📌 <strong>{m.senderName || m.sender.slice(0, 9)}</strong>: {m.content.slice(0, 80)}</span>
              <button class="mini" title="Unpin" onclick={() => api.pinMessage(m.channelId, m.id)}>✕</button>
            </div>
          {:else}
            <div class="muted small">No pinned messages — hover a message and hit 📌.</div>
          {/each}
        </div>
      {/if}

      {#if searchResults !== null}
        <div class="search-panel">
          <div class="search-head">
            <span class="muted">{searchResults.length} result{searchResults.length === 1 ? "" : "s"}</span>
            <button class="mini" onclick={() => ((searchResults = null), (searchQuery = ""))}>✕</button>
          </div>
          {#each searchResults as m (m.id)}
            <button class="search-hit" onclick={() => openSearchResult(m)}>
              <span class="muted small">{channelName(m.channelId)}</span>
              <span><strong>{m.senderName || m.sender.slice(0, 9)}</strong>: {m.content.slice(0, 100)}</span>
            </button>
          {/each}
        </div>
      {/if}

      <div class="feed" bind:this={feedEl}>
        {#each messages as m (m.id)}
          {#if m.kind === "system"}
            <div class="system-msg">
              <span>✨ <strong>{m.senderName || m.sender.slice(0, 9)}</strong> {m.content}</span>
            </div>
          {:else}
          <div class="msg">
            <div class="avatar" style={avatarStyle(m.sender)}>
              {avatarGlyph(m.sender, m.senderName)}
            </div>
            <div class="msg-main">
              {#if m.replyTo}
                {@const r = msgById(m.replyTo)}
                <div class="reply-ref">
                  ↩ {r ? `${r.senderName || r.sender.slice(0, 9)}: ${r.content.slice(0, 60)}` : "(original message)"}
                </div>
              {/if}
              <div class="msg-head">
                <span class="sender">{m.senderName || m.sender}</span>
                <span class="muted mono verify-fpr" title="verified identity">{m.sender.slice(0, 9)}</span>
                <span class="muted time">{fmtTime(m.sent)}</span>
                {#if m.pinned}<span title="Pinned">📌</span>{/if}
              </div>
              {#if m.deleted}
                <div class="body deleted"><em>message deleted</em></div>
              {:else if editing?.id === m.id}
                <input
                  class="edit-input"
                  bind:value={editDraft}
                  autofocus
                  onkeydown={(e) => {
                    if (e.key === "Enter") saveEdit();
                    else if (e.key === "Escape") editing = null;
                  }}
                  onblur={saveEdit}
                />
              {:else}
                <div class="body">
                  {@html renderContent(m.content)}{#if m.edited}<span class="edited-tag"> (edited)</span>{/if}
                </div>
              {/if}
              {#if m.reactions && Object.keys(m.reactions).length}
                <div class="reactions">
                  {#each Object.entries(m.reactions) as [emoji, fprs] (emoji)}
                    <button
                      class="reaction"
                      class:mine={fprs.includes(identity.fingerprint)}
                      onclick={() => react(m, emoji)}
                    >
                      {emoji} {fprs.length}
                    </button>
                  {/each}
                </div>
              {/if}
            </div>
            {#if !m.deleted}
              <div class="msg-actions">
                {#each QUICK_EMOJIS as e (e)}
                  <button title="React {e}" onclick={() => react(m, e)}>{e}</button>
                {/each}
                <button title="Reply" onclick={() => (replyingTo = m)}>↩</button>
                <button title={m.pinned ? "Unpin" : "Pin"} onclick={() => api.pinMessage(m.channelId, m.id)}>📌</button>
                {#if m.sender === identity.fingerprint}
                  <button title="Edit" onclick={() => startEdit(m)}>✏️</button>
                  <button title="Delete" onclick={() => deleteMsg(m)}>🗑</button>
                {/if}
              </div>
            {/if}
          </div>
          {/if}
        {:else}
          <div class="empty muted">No messages yet. Say hello 👋</div>
        {/each}
      </div>

      {#if replyingTo}
        <div class="reply-banner">
          <span class="muted"
            >Replying to <strong>{replyingTo.senderName || replyingTo.sender.slice(0, 9)}</strong></span
          >
          <button class="mini" onclick={() => (replyingTo = null)}>✕</button>
        </div>
      {/if}
      <div class="typing-line muted">{typingLabel}</div>
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
          class="ghost"
          title="Attach image (or paste one)"
          disabled={!activeChannel}
          onclick={() => fileInput.click()}>📎</button
        >
        <input
          placeholder={activeChannel ? `Message #${activeChannel.name}` : "Select a channel"}
          bind:value={draft}
          disabled={!activeChannel}
          oninput={onDraftInput}
          onpaste={onPaste}
        />
        <button type="submit" disabled={!draft.trim()}>Send</button>
      </form>
    </main>

    <!-- Members + contacts -->
    <aside class="panel">
      <div class="section-head"><span>Members — {activeGuild?.name ?? ""}</span></div>
      {#each members as mem (mem.fingerprint)}
        <div class="member">
          <span class="row" style="gap:8px; min-width:0">
            <span class="member-avatar" style={mem.color ? `background:${mem.color}` : ""}>
              {mem.emoji || (mem.name || mem.fingerprint).slice(0, 2)}
              <span class="dot presence" class:online={mem.online}></span>
            </span>
            <span class="member-text">
              <span class="member-name" title={mem.fingerprint}>
                {mem.name || mem.fingerprint.slice(0, 9)}{mem.isSelf ? " (you)" : ""}
              </span>
              {#if mem.status}<span class="muted member-status">{mem.status}</span>{/if}
            </span>
          </span>
          {#if activeGuild?.isOwner && !mem.isSelf}
            <button class="mini danger" title="Remove" onclick={() => kick(mem.fingerprint)}>✕</button>
          {/if}
        </div>
      {/each}

      <div class="section-head"><span>Contacts</span></div>
      {#each contacts as c (c.peerId)}
        <div class="contact">
          <div class="mono">{c.fingerprint}</div>
          {#if c.verified}
            <span class="badge verified">verified</span>
          {:else}
            <button class="mini" onclick={() => verify(c.peerId)}>verify</button>
          {/if}
        </div>
      {:else}
        <div class="muted small">No contacts yet.</div>
      {/each}
    </aside>
  </div>

  {#if toast}<div class="toast">{toast}</div>{/if}

  <!-- Modals -->
  {#if modal?.kind === "create"}
    <ModalCreate onSubmit={createGuild} onClose={() => (modal = null)} />
  {:else if modal?.kind === "channel"}
    <ModalCreate
      onSubmit={createChannel}
      onClose={() => (modal = null)}
      title="Create a channel"
      hint="Adds a channel visible to all guild members."
      placeholder="Channel name"
    />
  {:else if modal?.kind === "rename"}
    <ModalCreate
      onSubmit={renameGuild}
      onClose={() => (modal = null)}
      title="Rename guild"
      hint="Renames the guild for everyone."
      placeholder={activeGuild?.name || "New name"}
    />
  {:else if modal?.kind === "profile"}
    <ModalProfile {identity} onSubmit={saveProfile} onClose={() => (modal = null)} />
  {:else if modal?.kind === "join"}
    <ModalJoin error={modal.error} onSubmit={joinGuild} onClose={() => (modal = null)} />
  {:else if modal?.kind === "invite"}
    <ModalInvite code={modal.code} onCopy={copy} onClose={() => (modal = null)} />
  {/if}
{/if}

<style>
  .app {
    display: grid;
    grid-template-columns: 240px 1fr 260px;
    height: 100%;
  }
  .sidebar {
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-y: auto;
  }
  .brand {
    font-weight: 600;
    font-size: 16px;
    color: var(--accent);
    padding: 6px 8px 12px;
  }
  .section-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    text-transform: uppercase;
    font-size: 11px;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    margin: 14px 8px 4px;
  }
  .guild,
  .channel {
    text-align: left;
    background: transparent;
    color: var(--text);
    padding: 8px 10px;
    border-radius: 6px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .guild:hover,
  .channel:hover {
    background: var(--bg-input);
  }
  .guild.active,
  .channel.active {
    background: var(--accent);
    color: white;
  }
  .channel {
    font-size: 14px;
    color: var(--text-muted);
  }
  .channel.active {
    color: white;
  }
  .joinbtn {
    margin-top: 6px;
    font-size: 13px;
  }
  .mini {
    padding: 2px 8px;
    font-size: 13px;
    background: var(--bg-input);
    color: var(--text);
    border-radius: 5px;
  }
  .mini.danger {
    background: transparent;
    color: var(--danger);
  }
  .me {
    margin-top: auto;
    padding: 12px 6px 6px;
    border-top: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: 10px;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: 8px;
  }
  .me:hover {
    background: var(--bg-input);
  }
  .me-avatar {
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: var(--accent);
    color: white;
    display: grid;
    place-items: center;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
  }
  .me-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: 13px;
  }
  .small-status {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .member-avatar {
    position: relative;
    width: 30px;
    height: 30px;
    border-radius: 50%;
    background: var(--accent);
    color: white;
    display: grid;
    place-items: center;
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
  }
  .presence {
    position: absolute;
    bottom: -1px;
    right: -1px;
    border: 2px solid var(--bg-sidebar);
  }
  .member-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: 13px;
  }
  .member-status {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .search-box {
    width: 150px;
    padding: 5px 10px;
    font-size: 13px;
  }
  .pin-active {
    color: var(--accent-hover);
  }
  .pins-panel,
  .search-panel {
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
    padding: 8px 18px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 200px;
    overflow-y: auto;
  }
  .pin-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }
  .search-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 12px;
  }
  .search-hit {
    background: transparent;
    color: var(--text);
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 8px;
    border-radius: 6px;
    font-size: 13px;
  }
  .search-hit:hover {
    background: var(--bg-input);
  }
  .sender {
    font-weight: 600;
    color: var(--text);
  }
  .verify-fpr {
    font-size: 10px;
  }
  .chat {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .chat-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 18px;
    border-bottom: 1px solid var(--border);
  }
  .voice-pill {
    font-size: 12px;
    color: var(--verified);
    padding: 4px 10px;
    background: rgba(59, 165, 93, 0.12);
    border-radius: 12px;
  }
  .leave {
    color: var(--danger);
  }
  .voice-panel {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    padding: 12px 18px;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
  }
  .voice-tile {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }
  .voice-avatar {
    width: 34px;
    height: 34px;
    border-radius: 50%;
    background: var(--accent);
    color: white;
    display: grid;
    place-items: center;
    font-weight: 600;
    text-transform: uppercase;
    border: 2px solid transparent;
    transition: border-color 0.1s ease;
  }
  .voice-tile.speaking .voice-avatar {
    border-color: var(--verified);
    box-shadow: 0 0 0 2px rgba(59, 165, 93, 0.4);
  }
  .feed {
    flex: 1;
    overflow-y: auto;
    padding: 16px 18px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .empty {
    margin: auto;
  }
  .system-msg {
    text-align: center;
    font-size: 12px;
    color: var(--text-muted);
    padding: 2px 0;
  }
  .system-msg strong {
    color: var(--text);
  }
  .msg {
    display: flex;
    gap: 12px;
    position: relative;
  }
  .msg-main {
    min-width: 0;
    flex: 1;
  }
  .reply-ref {
    font-size: 12px;
    color: var(--text-muted);
    border-left: 2px solid var(--border);
    padding-left: 8px;
    margin-bottom: 2px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .msg-actions {
    position: absolute;
    top: -8px;
    right: 0;
    display: flex;
    gap: 4px;
    opacity: 0;
  }
  .msg:hover .msg-actions {
    opacity: 1;
  }
  .msg-actions button {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-muted);
    padding: 2px 8px;
    font-size: 13px;
  }
  .body.deleted {
    color: var(--text-muted);
  }
  .edited-tag {
    font-size: 10px;
    color: var(--text-muted);
  }
  .edit-input {
    margin-top: 2px;
  }
  .reactions {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }
  .reaction {
    background: var(--bg-input);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 1px 8px;
    font-size: 12px;
    border-radius: 10px;
  }
  .reaction.mine {
    border-color: var(--accent);
    background: rgba(91, 110, 245, 0.15);
  }
  .body :global(code) {
    background: var(--bg-input);
    padding: 1px 5px;
    border-radius: 4px;
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
  .body :global(a) {
    color: var(--accent-hover);
  }
  .body :global(img.attachment) {
    max-width: 380px;
    max-height: 280px;
    border-radius: 8px;
    display: block;
    margin-top: 4px;
  }
  .unread-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    flex-shrink: 0;
  }
  .reply-banner {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 18px;
    font-size: 13px;
    border-top: 1px solid var(--border);
  }
  .avatar {
    width: 38px;
    height: 38px;
    border-radius: 50%;
    background: var(--accent);
    color: white;
    display: grid;
    place-items: center;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
  }
  .msg-head {
    display: flex;
    gap: 8px;
    align-items: baseline;
  }
  .sender {
    color: var(--accent-hover);
  }
  .time {
    font-size: 11px;
  }
  .body {
    margin-top: 2px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .composer {
    display: flex;
    gap: 8px;
    padding: 0 18px 14px;
  }
  .typing-line {
    height: 18px;
    font-size: 12px;
    font-style: italic;
    padding: 2px 18px 0;
    border-top: 1px solid var(--border);
  }
  .panel {
    background: var(--bg-sidebar);
    border-left: 1px solid var(--border);
    padding: 12px;
    overflow-y: auto;
  }
  .member,
  .contact {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    word-break: break-all;
  }
  .member-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-muted);
    flex-shrink: 0;
  }
  .dot.online {
    background: var(--verified);
  }
  .badge {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 10px;
  }
  .badge.verified {
    background: rgba(59, 165, 93, 0.15);
    color: var(--verified);
  }
  .small {
    font-size: 12px;
    padding: 6px 8px;
  }
  .toast {
    position: fixed;
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    padding: 10px 16px;
    border-radius: 8px;
    font-size: 13px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
  }
</style>
