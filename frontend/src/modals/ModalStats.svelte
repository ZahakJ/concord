<script>
  // Read-only diagnostics: this conversation's local footprint + sync health,
  // and a whole-device network/storage view. Polls every 2s while open.
  import Modal from "./Modal.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import Icon from "../Icon.svelte";
  import InfoDot from "./InfoDot.svelte";
  import { S, activeGuild, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { haptic } from "../lib/touch.js";

  let { onClose } = $props();

  const guildId = $derived(S.modal?.guildId || S.activeGuildId);
  const guild = $derived(S.guilds.find((g) => g.id === guildId) || activeGuild());

  let gs = $state(null); // guild stats
  let ns = $state(null); // network stats

  async function refresh() {
    try {
      if (guildId) gs = await api.guildStats(guildId);
      ns = await api.networkStats();
    } catch {
      /* transient — next tick retries */
    }
  }

  $effect(() => {
    refresh();
    const t = setInterval(refresh, 2000);
    return () => clearInterval(t);
  });

  function fmtBytes(n) {
    if (!n) return "0 B";
    const u = ["B", "KB", "MB", "GB"];
    let i = 0;
    while (n >= 1024 && i < u.length - 1) {
      n /= 1024;
      i++;
    }
    return `${n.toFixed(i ? 1 : 0)} ${u[i]}`;
  }
  function fmtDate(sec) {
    return sec ? new Date(sec * 1000).toLocaleDateString([], { month: "short", day: "numeric", year: "numeric" }) : "—";
  }
  // "just now" / "14m ago" / a date. A device list is read to answer "when was
  // that thing last here", which an absolute timestamp makes you do arithmetic
  // for whenever the answer is "a minute ago".
  function fmtAgo(sec) {
    if (!sec) return "never";
    const s = Math.max(0, Math.floor(Date.now() / 1000) - sec);
    if (s < 45) return "just now";
    if (s < 3600) return `${Math.round(s / 60)}m ago`;
    if (s < 86400) return `${Math.round(s / 3600)}h ago`;
    if (s < 86400 * 30) return `${Math.round(s / 86400)}d ago`;
    return fmtDate(sec);
  }
  const transportLabel = { quic: "QUIC", tcp: "TCP", relay: "Relay", p2p: "P2P" };

  // The full peer ID is the one thing you open a diagnostics panel to read, and
  // it only ever existed in a title= tooltip — which a finger cannot produce.
  // Tap the chip to put it on the clipboard instead.
  let copied = $state("");
  async function copyId(id) {
    try {
      await navigator.clipboard?.writeText(id);
      haptic("light");
      copied = id;
      setTimeout(() => copied === id && (copied = ""), 1400);
    } catch {
      flash("Couldn't copy", "error");
    }
  }

  // ---- unlinking ----
  //
  // The confirm text is deliberately exact about what this can and cannot do
  // (see internal/app/unlink.go). Removing the device's leaf from your servers
  // is real and immediate; the self-erase only happens if that device comes back
  // online running an honest client. Promising a remote wipe would be a promise
  // no serverless design can keep, and the one place someone would rely on it is
  // the one place it matters.
  let unlinking = $state("");
  let confirming = $state(null); // the device awaiting confirmation
  async function unlink(dev) {
    confirming = null;
    const label = dev.name || "that device";
    unlinking = dev.key;
    try {
      await api.unlinkDevice(dev.key);
      flash(
        `${label} unlinked — it loses access to your servers now, and erases itself the next time it comes online`,
        "success",
      );
    } catch (err) {
      flash(err);
    }
    unlinking = "";
    refresh();
  }

  // One row per ACCOUNT, not per connection. Linked devices share a fingerprint,
  // so the same person on a laptop and a phone was showing up as two strangers
  // with two different key hashes and no hint they were the same someone.
  // Peers with no fingerprint (unknown, or the rendezvous) can't be grouped —
  // there's nothing to group them BY — so each stays its own row.
  function peerGroups(list) {
    const byAccount = new Map();
    const out = [];
    for (const p of list) {
      const g = p.fingerprint && byAccount.get(p.fingerprint);
      if (g) {
        g.conns.push(p);
        if (!g.transports.includes(p.transport)) g.transports.push(p.transport);
        // Show the best latency we have across their devices.
        if (p.rttMs > 0 && (g.rtt === 0 || p.rttMs < g.rtt)) g.rtt = p.rttMs;
        continue;
      }
      const entry = {
        key: p.fingerprint || p.id,
        name: p.name,
        role: p.role,
        rtt: p.rttMs > 0 ? p.rttMs : 0,
        transports: [p.transport],
        conns: [p],
      };
      if (p.fingerprint) byAccount.set(p.fingerprint, entry);
      out.push(entry);
    }
    return out;
  }
</script>

<Modal title="Stats & diagnostics" {onClose} wide>
  <!-- Reached from Settings there may be no guild open at all, and the panel is
       worth opening anyway — it is the only route to your linked devices. Drop
       the guild section entirely in that case rather than leaving a heading over
       a "Loading…" that never resolves, because nothing was ever requested. -->
  {#if guildId}
  <section>
    <strong class="label">{guild?.kind === "dm" ? "This conversation" : guild?.name || "This guild"}</strong>
    {#if gs}
      <div class="grid">
        <div class="stat"><span class="k">Messages</span><span class="v">{gs.messages.toLocaleString()}</span></div>
        <div class="stat"><span class="k">Message data</span><span class="v">{fmtBytes(gs.messageBytes)}</span></div>
        <div class="stat"><span class="k">Members</span><span class="v">{gs.members}</span></div>
        <div class="stat"><span class="k">Channels</span><span class="v">{gs.channels}</span></div>
        <div class="stat"><span class="k">First message</span><span class="v">{fmtDate(gs.oldestUnix)}</span></div>
        <div class="stat"><span class="k">Latest</span><span class="v">{fmtDate(gs.newestUnix)}</span></div>
        <div class="stat"><span class="k">MLS epoch</span><span class="v">{gs.epoch}</span></div>
        <div class="stat">
          <span class="k">Sync</span>
          <span class="v" class:warn={gs.outOfSync}>{gs.outOfSync ? "healing…" : "in sync"}</span>
        </div>
      </div>
    {:else}
      <p class="muted tiny">Loading…</p>
    {/if}
  </section>

  <hr />
  {/if}

  <!-- YOUR DEVICES. Its own section, above the peer list, because a device of
       yours is not one of the peers your rendezvous introduced you to — it used
       to sit down there labelled "unknown peer", which is the worst possible
       thing for your desktop to call your phone. -->
  <section>
    <strong class="label">Your devices</strong>
    {#if ns?.deviceList?.length}
      <div class="peers">
        {#each ns.deviceList as d (d.key)}
          <div class="peer dev" class:gone={!d.online} class:revoked={d.revokedAt}>
            <span class="pdot" class:relay={d.relayed} class:off={!d.online} class:infra={d.revokedAt}></span>
            <span class="pname">{d.name || "Linked device"}</span>
            {#if d.thisOne}
              <span class="pdev">this one</span>
            {:else if d.revokedAt}
              <span class="pdev">unlinked</span>
              <InfoDot
                text="Unlinked — it loses new traffic now and erases itself when it next connects."
                label="What does unlinked mean?"
              />
            {/if}
            <button class="pid" onclick={() => copyId(d.peerId)} title="Copy full ID">
              {copied === d.peerId ? "copied" : `${d.peerId.slice(0, 12)}…${d.peerId.slice(-6)}`}
            </button>
            <span class="prtt muted">
              {d.online ? "online" : `last seen ${fmtAgo(d.lastSeen)}`}
            </span>
            {#if d.rttMs > 0}<span class="prtt muted">{d.rttMs} ms</span>{/if}
            {#if d.transport}<span class="ptag">{transportLabel[d.transport] || d.transport}</span>{/if}
            {#if d.direction}<span class="pdir muted">{d.direction}</span>{/if}
            {#if !d.thisOne && !d.revokedAt}
              <button class="unlink" disabled={unlinking === d.key} onclick={() => (confirming = d)}>
                {unlinking === d.key ? "…" : "Unlink"}
              </button>
            {/if}
          </div>
        {/each}
      </div>
      <p class="muted tiny note">
        Every device signed in to this account. Unlinking one removes it from your
        servers immediately; it erases its own copy only if it comes back online.
      </p>
    {:else}
      <p class="muted tiny">Loading…</p>
    {/if}
  </section>

  <hr />

  <section>
    <strong class="label">Network &amp; storage</strong>
    {#if ns}
      <div class="grid">
        <div class="stat"><span class="k">Database</span><span class="v">{fmtBytes(ns.dbSizeBytes)}</span></div>
        <div class="stat"><span class="k">Attachments</span><span class="v">{ns.attachmentCount} · {fmtBytes(ns.attachmentBytes)}</span></div>
        <div class="stat">
          <span class="k">
            Peers online<InfoDot
              text="People and devices you're connected to, excluding rendezvous and relay nodes."
              label="What counts as a peer?"
            />
          </span><span class="v">{ns.memberPeers}</span>
        </div>
        <div class="stat">
          <span class="k">
            Connections<InfoDot
              text="All libp2p connections, including rendezvous and relay infrastructure."
              label="What counts as a connection?"
            />
          </span><span class="v">{ns.peers}</span>
        </div>
        <div class="stat">
          <span class="k">Rendezvous</span>
          <span class="v" class:warn={ns.hasBootstrap && !ns.bootstrapReached}>
            {ns.hasBootstrap ? (ns.bootstrapReached ? "connected" : "unreachable") : "not configured"}
          </span>
        </div>
        <div class="stat">
          <span class="k">Bandwidth</span>
          <span class="v">↓ {fmtBytes(ns.rateIn)}/s&nbsp; ↑ {fmtBytes(ns.rateOut)}/s</span>
        </div>
        <div class="stat">
          <span class="k">Transferred</span>
          <span class="v">↓ {fmtBytes(ns.totalIn)}&nbsp; ↑ {fmtBytes(ns.totalOut)}</span>
        </div>
      </div>
      {#if ns.peerList?.length}
        <div class="peers">
          {#each peerGroups(ns.peerList) as grp (grp.key)}
            <div class="peer" class:multi={grp.conns.length > 1}>
              <span
                class="pdot"
                class:relay={grp.conns.every((c) => c.relayed)}
                class:infra={grp.role === "rendezvous"}
              ></span>
              {#if grp.name}
                <span class="pname">{grp.name}</span>
              {:else if grp.role !== "rendezvous"}
                <span class="pname unknown">
                  unknown peer<InfoDot
                    text="A connected peer whose profile you've never learned — someone else using your rendezvous, or a stray test instance."
                    label="What is an unknown peer?"
                  />
                </span>
              {/if}
              {#if grp.conns.length > 1}
                <span class="pdev">{grp.conns.length} devices</span>
              {/if}
              <button class="pid" onclick={() => copyId(grp.conns.map((c) => c.id).join("\n"))} title="Copy full ID">
                {copied.startsWith(grp.conns[0].id)
                  ? "copied"
                  : `${grp.conns[0].id.slice(0, 12)}…${grp.conns[0].id.slice(-6)}`}
              </button>
              {#if grp.role === "rendezvous"}<span class="prole">rendezvous</span>{/if}
              {#if grp.rtt > 0}<span class="prtt muted">{grp.rtt} ms</span>{/if}
              {#each grp.transports as t (t)}
                <span class="ptag">{transportLabel[t] || t}</span>
              {/each}
              <span class="pdir muted">{grp.conns[0].direction}</span>
            </div>
          {/each}
        </div>
      {:else}
        <p class="muted tiny">No peers connected right now.</p>
      {/if}
      {#if ns.backgroundPeers > 0}
        <!-- One line instead of hundreds of rows. These are Kademlia's own mesh:
             connections that never identified themselves as a Concord account,
             which with the public-DHT setting on is mostly unrelated IPFS nodes.
             They are worth knowing the size of and worth nothing individually. -->
        <p class="muted tiny note">
          + {ns.backgroundPeers} background {ns.backgroundPeers === 1 ? "connection" : "connections"}
          holding the network open. They aren't people — that's the DHT itself.
        </p>
      {/if}
      <p class="muted tiny note">Attachment totals are for the whole device — blobs are shared and de-duplicated across guilds.</p>
    {:else}
      <p class="muted tiny">Loading…</p>
    {/if}
  </section>
</Modal>

{#if confirming}
  <ConfirmDialog
    title="Unlink {confirming.name || 'this device'}?"
    body={"It stops receiving anything new from your servers straight away — that part is immediate and cannot be undone by whoever holds it.\n\nIt erases its own copy of your messages only when it next comes online with Concord running. A device that stays off keeps what it already downloaded, and this cannot reach it. If the device is in someone else's hands, treat everything on it as already read."}
    confirmLabel="Unlink"
    onConfirm={() => unlink(confirming)}
    onClose={() => (confirming = null)}
  />
{/if}

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    text-align: left;
  }
  .label {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  hr {
    border: none;
    border-top: 1px solid var(--border);
    margin: 14px 0;
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }
  .stat {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 10px;
    padding: 8px 12px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .k {
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .v {
    font-size: var(--fs-ui);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .v.warn {
    color: var(--warn-text);
  }
  .peers {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-top: 8px;
  }
  /* Wraps, always. A peer row carries a dot, a name, a 19-character ID, a
     latency, a transport pill and a direction — over 350px of unshrinkable
     content in a sheet whose content box is 320px at 360px wide. Held on one
     line it overflowed, and .dialog then computed overflow-x: auto and let the
     whole settings sheet pan sideways under a thumb. */
  .peer {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    row-gap: 2px;
    padding: 6px 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-size: var(--fs-compact);
  }
  .pdot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--ok, #3ba55d);
    flex-shrink: 0;
  }
  .pdot.relay {
    background: var(--warn);
  }
  .pdot.infra {
    background: var(--text-faint);
  }
  .peer.dev .pname {
    flex: none;
    white-space: nowrap;
  }
  .peer.dev .pid {
    white-space: nowrap;
  }
  /* The action goes to the far right, wherever the row happens to break. */
  .peer.dev .ptag {
    margin-left: 0;
  }
  .peer.dev .unlink {
    margin-left: auto;
  }
  /* An offline device is a row you still want to read — dimmed, not hidden. */
  .pdot.off {
    background: var(--text-faint);
  }
  .peer.gone {
    opacity: 0.72;
  }
  .peer.revoked .pname {
    text-decoration: line-through;
  }
  .unlink {
    flex: none;
    padding: 2px 9px;
    border-radius: 999px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-size: var(--fs-small);
  }
  .unlink:hover:not(:disabled),
  .unlink:active:not(:disabled) {
    border-color: var(--danger);
    color: var(--danger);
  }
  .unlink:disabled {
    opacity: 0.5;
  }
  .prole {
    font-size: var(--fs-tiny);
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .pname {
    font-weight: 600;
    color: var(--text);
  }
  /* "2 devices" — quiet, next to the name it belongs to. */
  .pdev {
    flex: none;
    font-size: var(--fs-tiny);
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .pname.unknown {
    font-weight: 500;
    font-style: italic;
    color: var(--warn, #f0b232);
  }
  /* A button, not a <code>: the full ID only ever lived in a title= tooltip,
     which a touch device cannot open. Styled to stay a quiet inline chip. */
  .pid {
    font-family: var(--mono, monospace);
    color: var(--text-muted);
    font-size: var(--fs-small);
    background: transparent;
    border: none;
    padding: 2px 4px;
    margin: -2px -4px;
    border-radius: 5px;
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pid:hover,
  .pid:active {
    background: var(--bg-3);
    color: var(--text);
  }
  .prtt {
    font-size: var(--fs-small);
    font-variant-numeric: tabular-nums;
  }
  .ptag {
    margin-left: auto;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-size: var(--fs-small);
    font-weight: 600;
  }
  .pdir {
    font-size: var(--fs-small);
    min-width: 58px;
    text-align: right;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  /* One stat per row on a phone. Two columns left each cell ~148px of usable
     width, and "↓ 12.4 KB/s ↑ 3.1 KB/s" simply does not fit in that. */
  @media (pointer: coarse), (max-width: 768px) {
    .grid {
      grid-template-columns: 1fr;
    }
    .stat {
      flex-wrap: wrap;
    }
    /* The direction reserve is only there to line columns up on desktop; on a
       wrapped row it just pushes the last item onto a line of its own. */
    .pdir {
      min-width: 0;
      text-align: left;
    }
    /* The touch floor would turn the ID chip into a 44px block sitting in the
       middle of a line of text. Keep it inline and buy the reach with padding
       that the negative margin gives straight back to the layout. */
    .pid {
      min-height: 0;
      padding: 7px 6px;
      margin: -7px -6px;
    }
  }
  .note {
    margin-top: 8px;
  }
</style>
