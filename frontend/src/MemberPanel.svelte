<script>
  // Right panel: guild members (with the fingerprint-verification card) and
  // the "known peers" network log. Rows are proper buttons; the owner's kick
  // control is its own sibling button (no more button-in-button nesting).
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, activeGuild, refreshRightPanel, flash } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";

  let showPeers = $state(false);

  const g = $derived(activeGuild());

  async function kick(fingerprint) {
    try {
      await api.removeMember(S.activeGuildId, fingerprint);
      await refreshRightPanel();
      flash("Member removed");
    } catch (err) {
      flash(err);
    }
  }

  async function verify(mem) {
    try {
      await api.verifyFingerprint(mem.fingerprint);
      await refreshRightPanel();
      S.memberPopover = null;
      flash("Member verified ✓");
    } catch (err) {
      flash(err);
    }
  }
</script>

<aside class="panel">
  <div class="section-head"><span>Members — {g?.name ?? ""}</span></div>
  {#each S.members as mem (mem.fingerprint)}
    <div class="member-row">
      <button
        class="member"
        onclick={() =>
          (S.memberPopover = S.memberPopover === mem.fingerprint ? null : mem.fingerprint)}
      >
        <Avatar
          name={mem.name || mem.fingerprint}
          emoji={mem.emoji}
          color={mem.color}
          image={mem.avatar}
          size={30}
          online={mem.online}
        />
        <span class="member-text">
          <span class="member-name" title={mem.fingerprint}>
            {mem.name || mem.fingerprint.slice(0, 9)}{mem.isSelf ? " (you)" : ""}
            {#if mem.verified && !mem.isSelf}
              <span class="v-badge" title="Identity verified"><Icon name="check" size={11} /></span>
            {/if}
          </span>
          {#if mem.status}<span class="muted member-status">{mem.status}</span>{/if}
        </span>
      </button>
      {#if g?.isOwner && !mem.isSelf}
        <button class="kick" title="Remove from server" aria-label="Remove {mem.name || 'member'} from server" onclick={() => kick(mem.fingerprint)}>
          <Icon name="close" size={12} />
        </button>
      {/if}
    </div>
    {#if S.memberPopover === mem.fingerprint}
      <div class="member-card">
        {#if mem.isSelf}
          <p class="muted small">
            This is you. Others confirm it's really you by comparing this fingerprint with you
            over a call or in person:
          </p>
        {:else if mem.verified}
          <p class="muted small">
            ✓ You've verified this member — you compared their fingerprint out-of-band, so you
            know no one is impersonating them.
          </p>
        {:else}
          <p class="muted small">
            Names and pictures are self-chosen and can be faked; the
            <strong>fingerprint below cannot</strong>. Read it aloud with
            {mem.name || "this member"} over a call (or in person) — if it matches what they see
            on their own profile, hit Verify.
          </p>
        {/if}
        <code class="mono fpr-code">{mem.fingerprint}</code>
        {#if !mem.isSelf && !mem.verified}
          <button onclick={() => verify(mem)}>Verify identity</button>
        {/if}
      </div>
    {/if}
  {/each}

  <div class="section-head">
    <span>Known peers</span>
    <button class="mini" aria-label={showPeers ? "Collapse" : "Expand"} onclick={() => (showPeers = !showPeers)}>
      <span class="chev" class:open={showPeers}><Icon name="chevron" size={11} /></span>
    </button>
  </div>
  {#if showPeers}
    <p class="muted small peers-info">
      Every Concord node your device has ever connected to — including strangers on the same
      Wi-Fi discovered automatically. They can't read anything (messages are end-to-end
      encrypted); this is just a network log. To verify a <em>friend</em>, click them in the
      Members list above.
    </p>
    {#each S.contacts as c (c.peerId)}
      <div class="contact">
        <span class="mono peer-fpr" title={c.peerId}>{c.fingerprint.slice(0, 19)}…</span>
        {#if c.verified}<span class="badge verified">✓</span>{/if}
      </div>
    {:else}
      <div class="muted small">No peers seen yet.</div>
    {/each}
  {/if}
</aside>

<style>
  .panel {
    background: var(--bg-1);
    border-left: 1px solid var(--border);
    padding: 12px 8px;
    overflow-y: auto;
  }
  .section-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    text-transform: uppercase;
    font-size: 11px;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    margin: 10px 8px 4px;
  }
  .member-row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
  }
  .member-row:hover {
    background: var(--bg-3);
  }
  .member {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px 8px;
    min-width: 0;
  }
  .member:hover {
    background: transparent;
  }
  .member-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: 13px;
  }
  .member-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .member-status {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .v-badge {
    color: var(--ok);
    display: inline-grid;
    place-items: center;
  }
  .kick {
    background: transparent;
    color: var(--danger);
    padding: 4px 8px;
    opacity: 0;
  }
  .member-row:hover .kick,
  .kick:focus-visible {
    opacity: 1;
  }
  .kick:hover {
    background: var(--danger-soft);
  }
  .member-card {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 10px;
    margin: 2px 4px 8px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .member-card p {
    margin: 0;
    line-height: 1.45;
  }
  .fpr-code {
    font-size: 11px;
    word-break: break-all;
    background: var(--bg-3);
    padding: 6px 8px;
    border-radius: var(--radius-sm);
  }
  .peers-info {
    padding: 4px 8px;
    line-height: 1.45;
    margin: 0;
  }
  .contact {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    word-break: break-all;
  }
  .peer-fpr {
    font-size: 11px;
  }
  .badge {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 10px;
  }
  .badge.verified {
    background: var(--ok-soft);
    color: var(--ok);
  }
  .small {
    font-size: 12px;
    padding: 6px 8px;
  }
  .mini {
    padding: 2px 6px;
    background: transparent;
    color: var(--text-muted);
  }
  .mini:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .chev {
    display: inline-grid;
    place-items: center;
    transition: transform 0.15s ease;
  }
  .chev.open {
    transform: rotate(90deg);
  }
</style>
