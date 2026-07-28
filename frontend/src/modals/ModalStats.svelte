<script>
  // Read-only diagnostics: this conversation's local footprint + sync health,
  // and a whole-device network/storage view. Polls every 2s while open.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

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
  const transportLabel = { quic: "QUIC", tcp: "TCP", relay: "Relay", p2p: "P2P" };

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

  <section>
    <strong class="label">Network &amp; storage</strong>
    {#if ns}
      <div class="grid">
        <div class="stat"><span class="k">Database</span><span class="v">{fmtBytes(ns.dbSizeBytes)}</span></div>
        <div class="stat"><span class="k">Attachments</span><span class="v">{ns.attachmentCount} · {fmtBytes(ns.attachmentBytes)}</span></div>
        <div class="stat" title="People/devices you're connected to, excluding rendezvous & relay nodes">
          <span class="k">Peers online</span><span class="v">{ns.memberPeers}</span>
        </div>
        <div class="stat" title="All libp2p connections, including rendezvous & relay infrastructure">
          <span class="k">Connections</span><span class="v">{ns.peers}</span>
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
                <span
                  class="pname unknown"
                  title="Connected peer whose profile you've never learned — someone else using your rendezvous, or a stray test instance"
                >unknown peer</span>
              {/if}
              {#if grp.conns.length > 1}
                <span class="pdev" title="One account, connected from more than one device">
                  {grp.conns.length} devices
                </span>
              {/if}
              <code class="pid" title={grp.conns.map((c) => c.id).join("\n")}>
                {grp.conns[0].id.slice(0, 12)}…{grp.conns[0].id.slice(-6)}
              </code>
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
      <p class="muted tiny note">Attachment totals are for the whole device — blobs are shared and de-duplicated across guilds.</p>
    {:else}
      <p class="muted tiny">Loading…</p>
    {/if}
  </section>
</Modal>

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    text-align: left;
  }
  .label {
    font-size: 11px;
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
    font-size: 12px;
    color: var(--text-muted);
  }
  .v {
    font-size: 13px;
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
  .peer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-size: 12px;
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
  .prole {
    font-size: 10px;
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
    font-size: 10.5px;
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
  .pid {
    font-family: var(--mono, monospace);
    color: var(--text-muted);
    font-size: 11px;
  }
  .prtt {
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .ptag {
    margin-left: auto;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-size: 11px;
    font-weight: 600;
  }
  .pdir {
    font-size: 11px;
    min-width: 58px;
    text-align: right;
  }
  .tiny {
    font-size: 11px;
  }
  .note {
    margin-top: 8px;
  }
</style>
