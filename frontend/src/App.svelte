<script>
  import { api, on } from "./lib/api.js";
  import { VoiceMesh } from "./lib/voice.js";
  import Login from "./Login.svelte";
  import ModalCreate from "./modals/ModalCreate.svelte";
  import ModalJoin from "./modals/ModalJoin.svelte";
  import ModalInvite from "./modals/ModalInvite.svelte";

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

  function msgById(id) {
    return messages.find((m) => m.id === id);
  }

  // Voice state
  let voice = $state(null); // { mesh, channelId }
  let voiceParticipants = $state([]);
  let muted = $state(false);

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
    await refreshGuilds();
    ready = true;

    // Live message feed.
    on("message", (m) => {
      if (m.channelId === activeChannelId) {
        messages = [...messages, m];
        scrollSoon();
      } else {
        flash(`New message in another channel`);
      }
    });
    on("presence", () => refreshRightPanel());

    // Route voice signaling to the active mesh.
    on("voice-presence", (v) => {
      if (voice && v.channelId === voice.channelId) {
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
    muted = false;
    await api.leaveVoice(ch);
  }

  function toggleMute() {
    muted = !muted;
    voice?.mesh.setMuted(muted);
  }

  async function saveName() {
    const n = displayName.trim();
    if (!n) return;
    await api.setDisplayName(n);
    flash("Name updated");
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
    typingList.forEach((t) => clearTimeout(t.timer));
    typingList = [];
    messages = (await api.messages(id)) || [];
    scrollSoon();
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
          {g.name}
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
            # {c.name}
          </button>
        {/each}
      {/if}

      <div class="me">
        <input
          class="name-input"
          bind:value={displayName}
          onblur={saveName}
          onkeydown={(e) => e.key === "Enter" && e.target.blur()}
          placeholder="Set your name"
          maxlength="32"
        />
        <div class="muted mono" title={identity.peerId}>{identity.fingerprint}</div>
      </div>
    </aside>

    <!-- Chat -->
    <main class="chat">
      <header class="chat-head">
        <div>
          {#if activeChannel}<strong># {activeChannel.name}</strong>{:else}<span class="muted">No channel</span>{/if}
        </div>
        <div class="row">
          {#if voice && voice.channelId === activeChannelId}
            <span class="voice-pill">🔊 {voiceParticipants.length + 1} in voice</span>
            <button class="ghost" onclick={toggleMute}>{muted ? "🔇 Unmute" : "🎙 Mute"}</button>
            <button class="ghost leave" onclick={leaveVoice}>Leave</button>
          {:else if activeChannel && !voice}
            <button class="ghost" onclick={joinVoice}>🔊 Join Voice</button>
          {/if}
          {#if activeGuild?.isOwner}
            <button class="ghost" onclick={showInvite}>Invite</button>
          {/if}
        </div>
      </header>

      <div class="feed" bind:this={feedEl}>
        {#each messages as m (m.id)}
          {#if m.kind === "system"}
            <div class="system-msg">
              <span>✨ <strong>{m.senderName || m.sender.slice(0, 9)}</strong> {m.content}</span>
            </div>
          {:else}
          <div class="msg">
            <div class="avatar">{(m.senderName || m.sender || "?").slice(0, 2)}</div>
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
              </div>
              <div class="body">{m.content}</div>
            </div>
            <button class="reply-btn" title="Reply" onclick={() => (replyingTo = m)}>↩</button>
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
          placeholder={activeChannel ? `Message #${activeChannel.name}` : "Select a channel"}
          bind:value={draft}
          disabled={!activeChannel}
          oninput={onDraftInput}
        />
        <button type="submit" disabled={!draft.trim()}>Send</button>
      </form>
    </main>

    <!-- Members + contacts -->
    <aside class="panel">
      <div class="section-head"><span>Members — {activeGuild?.name ?? ""}</span></div>
      {#each members as mem (mem.fingerprint)}
        <div class="member">
          <span class="row" style="gap:6px; min-width:0">
            <span class="dot" class:online={mem.online} title={mem.online ? "online" : "offline"}></span>
            <span class="member-name" title={mem.fingerprint}>
              {mem.name || mem.fingerprint.slice(0, 9)}{mem.isSelf ? " (you)" : ""}
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
    padding-top: 12px;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 4px;
    word-break: break-all;
  }
  .name-input {
    background: transparent;
    border: 1px solid transparent;
    padding: 4px 6px;
    font-weight: 600;
    font-size: 14px;
  }
  .name-input:hover {
    border-color: var(--border);
  }
  .name-input:focus {
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
  .reply-btn {
    position: absolute;
    top: -6px;
    right: 0;
    opacity: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text-muted);
    padding: 2px 8px;
    font-size: 13px;
  }
  .msg:hover .reply-btn {
    opacity: 1;
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
