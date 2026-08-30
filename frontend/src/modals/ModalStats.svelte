<script>
  // Read-only diagnostics: this conversation's local footprint + sync health,
  // and a whole-device network/storage view. Polls every 2s while open.
  import RailShell from "./RailShell.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import InfoDot from "./InfoDot.svelte";
  import { S, activeGuild, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { haptic } from "../lib/touch.js";

  let { onClose } = $props();

  const guildId = $derived(S.modal?.guildId || S.activeGuildId);
  const guild = $derived(S.guilds.find((g) => g.id === guildId) || activeGuild());

  let gs = $state(null); // guild stats
  let ns = $state(null); // network stats
  let props = $state(null); // top members by received props
  let activity = $state(null); // guild insights
  // Which week the pointer is on. Native `title` + cursor:help was a question
  // mark and a system tooltip; this is a chip on the chart instead.
  let hoverWeek = $state(-1);

  async function refresh() {
    try {
      if (guildId) {
        gs = await api.guildStats(guildId);
        props = await loadProps();
        // Insights change on the scale of days, so they are fetched once
        // rather than on the 2s diagnostics poll — three GROUP BYs over the
        // guild's whole history is not a thing to run every two seconds.
        if (activity === null) activity = (await api.guildInsights(guildId)) || {};
      }
      ns = await api.networkStats();
    } catch {
      /* transient — next tick retries */
    }
  }

  // ---- insights shaping ---------------------------------------------------
  const insChannels = $derived((activity?.channels || []).filter((c) => c.messages > 0));
  const busiest = $derived(insChannels.slice(0, 8));
  const chMax = $derived(Math.max(1, ...busiest.map((c) => c.messages)));
  // "Quietest" is not the bottom of the count: a room made yesterday with two
  // messages is new, not quiet. It is rooms nobody has spoken in for a month —
  // and rooms nobody has EVER spoken in, which are the ones to archive.
  const QUIET_MS = 30 * 86400000;
  const quiet = $derived(
    (activity?.channels || [])
      .filter((c) => c.type !== "voice" && (!c.lastUnix || Date.now() - c.lastUnix * 1000 > QUIET_MS))
      .sort((a, b) => (a.lastUnix || 0) - (b.lastUnix || 0))
      .slice(0, 5),
  );
  const weekMax = $derived(Math.max(1, ...(activity?.perWeek || [0])));
  const weekTotal = $derived((activity?.perWeek || []).reduce((n, x) => n + x, 0));
  const weekLabel = (i) => {
    const d = new Date((activity.weekStartUnix + i * 7 * 86400) * 1000);
    return d.toLocaleDateString([], { month: "short", day: "numeric" });
  };
  const peopleMax = $derived(Math.max(1, ...((activity?.people || []).map((p) => p.messages))));
  // Polyline over the weekly bars. x sits on each bar's centre so the line
  // tracks the columns rather than their left edges.
  const weekLine = $derived.by(() => {
    const w = activity?.perWeek || [];
    if (w.length < 2) return "";
    const max = weekMax;
    return w
      .map((n, i) => `${i + 0.5},${(100 - Math.max(1.5, (n / max) * 100)).toFixed(2)}`)
      .join(" ");
  });
  const weekArea = $derived.by(() => {
    const w = activity?.perWeek || [];
    if (w.length < 2 || !weekLine) return "";
    return `0.5,100 ${weekLine} ${w.length - 0.5},100`;
  });

  const noMotion =
    typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

  // Count from zero when the panel opens. The bars grow in CSS; the headline
  // would look frozen next to them if it printed the final number immediately.
  function countUp(node, n) {
    const fmt = (x) => Math.round(x).toLocaleString();
    const target = Number(n) || 0;
    if (noMotion || target === 0) {
      node.textContent = fmt(target);
      return;
    }
    let raf = 0;
    const t0 = performance.now();
    const dur = 620;
    const tick = (now) => {
      const p = Math.min(1, (now - t0) / dur);
      const e = 1 - (1 - p) ** 3;
      node.textContent = fmt(target * e);
      if (p < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return {
      destroy() {
        cancelAnimationFrame(raf);
      },
    };
  }

  // Props leaderboard: celebratory reactions (🏆 ⭐ 💯 ❤️ 👏) received on each
  // member's messages, tallied from THIS replica's reaction history. Every
  // viewer computes it locally — eventually consistent exactly like the
  // reaction counts it's derived from, and nobody's word to take for it.
  async function loadProps() {
    const [tally, members] = await Promise.all([api.propsTally(guildId), api.members(guildId)]);
    const nameOf = new Map((members || []).map((m) => [m.fingerprint, m.name]));
    return Object.entries(tally || {})
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5)
      .map(([fpr, n]) => ({ fpr, n, name: nameOf.get(fpr) || fpr.slice(0, 9) }));
  }

  $effect(() => {
    refresh();
    sampleInsets();
    const t = setInterval(() => {
      refresh();
      sampleInsets();
    }, 2000);
    return () => clearInterval(t);
  });

  // ---- display insets --------------------------------------------------
  // The numbers behind "the app draws under the status bar". Three layers,
  // sampled separately so a screenshot of this section says exactly which
  // layer lost the value:
  //   1. what the native side pushed (inline --sa-* on <html>, set by
  //      MainActivity's inset bridge — "unset" means the push never arrived),
  //   2. what CSS resolves (--safe-*, env(); measured via a probe element,
  //      because getComputedStyle on an unregistered custom property returns
  //      the raw var()/env() expression, not pixels),
  //   3. what the top bar is actually wearing (its computed padding-top).
  // Plus the viewport geometry (is the page really full-bleed?) and the
  // native push history (__saLog, written by the same script that sets
  // --sa-*), which shows a good value being overwritten by a bad one.
  let ins = $state(null);
  function probePx(expr) {
    const el = document.createElement("div");
    el.style.cssText = `position:absolute;visibility:hidden;pointer-events:none;padding-top:${expr};`;
    document.body.appendChild(el);
    const v = getComputedStyle(el).paddingTop;
    el.remove();
    return v;
  }
  function sampleInsets() {
    if (!S.isMobile) return;
    const st = document.documentElement.style;
    const raw = (p) => st.getPropertyValue(p).trim() || "unset";
    const bar = document.querySelector("header.mtopbar");
    ins = {
      saTop: raw("--sa-top"),
      saBottom: raw("--sa-bottom"),
      saLeft: raw("--sa-left"),
      saRight: raw("--sa-right"),
      kb: raw("--kb"),
      barsTop: raw("--sa-bars-top"),
      floorTop: raw("--sa-floor-top"),
      envTop: probePx("env(safe-area-inset-top, 0px)"),
      envBottom: probePx("env(safe-area-inset-bottom, 0px)"),
      safeTop: probePx("var(--safe-top, 0px)"),
      safeBottom: probePx("var(--safe-bottom, 0px)"),
      barPad: bar ? getComputedStyle(bar).paddingTop : "not mounted",
      innerH: window.innerHeight,
      innerW: window.innerWidth,
      screenH: window.screen.height,
      screenW: window.screen.width,
      dpr: Math.round(window.devicePixelRatio * 1000) / 1000,
      // The sub-pixel bottom of the visual viewport. Read next to "viewport vs
      // screen" it answers the question every round of this bug turned on: is
      // the page really spanning the whole display, or has something inset it?
      vvBottom: window.visualViewport
        ? Math.round((window.visualViewport.offsetTop + window.visualViewport.height) * 100) / 100
        : null,
      pushes: (window.__saLog || []).slice(-6).reverse(),
      now: Date.now(),
    };
  }
  const fmtPush = (p) =>
    `top ${p.top} · bottom ${p.bottom} · kb ${p.kb} (bars.top ${p.barsTopPx}px · wvTop ${p.wvTopPx}px · winH ${p.winHPx}px · ×${p.d})`;

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
  // (see internal/app/unlink.go). Removing the device's leaf from your guilds
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
        `${label} unlinked — it loses access to your guilds now, and erases itself the next time it comes online`,
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

<RailShell title="Insights & diagnostics" {onClose} wide>
  <!-- Reached from Settings there may be no guild open at all, and the panel is
       worth opening anyway — it is the only route to your linked devices. Drop
       the guild section entirely in that case rather than leaving a heading over
       a "Loading…" that never resolves, because nothing was ever requested. -->
  {#if guildId}
  <!-- INSIGHTS. What an organizer decides from: which rooms are alive, who is
       talking, and what the last quarter looked like. Every number is a query
       over rows already on this device — nothing is transmitted and no other
       member is asked, which is why a panel like this is free here. -->
  <section>
    <strong class="label">Activity</strong>
    {#if !activity}
      <p class="muted tiny">Loading…</p>
    {:else if !weekTotal && !insChannels.length}
      <p class="muted tiny">Nothing has been said here yet.</p>
    {:else}
      <div class="hero">
        <strong class="hero-n" use:countUp={weekTotal}></strong>
        <span class="hero-l">messages in {activity.weeks} weeks</span>
      </div>
      <div
        class="spark"
        class:peek={hoverWeek >= 0}
        role="img"
        aria-label={`${weekTotal} messages over the last ${activity.weeks} weeks`}
        onpointerleave={() => (hoverWeek = -1)}
      >
        {#if hoverWeek >= 0 && activity.perWeek[hoverWeek] != null}
          <div
            class="stip"
            style="--x: {((hoverWeek + 0.5) / activity.perWeek.length) * 100}%"
          >
            <b>{activity.perWeek[hoverWeek].toLocaleString()}</b>
            <span>{weekLabel(hoverWeek)}</span>
          </div>
        {/if}
        {#each activity.perWeek as n, i (i)}
          <span
            class="sbar"
            class:now={i === activity.perWeek.length - 1}
            class:hot={hoverWeek === i}
            style="--i:{i}"
            onpointerenter={() => (hoverWeek = i)}
          >
            <span class="sfill" style="--h:{Math.max(2, Math.round((n / weekMax) * 100))}%"></span>
          </span>
        {/each}
        {#if weekLine}
          <svg
            class="sline"
            viewBox="0 0 {activity.perWeek.length} 100"
            preserveAspectRatio="none"
            aria-hidden="true"
          >
            <polygon class="sarea" points={weekArea}></polygon>
            <polyline pathLength="1" points={weekLine}></polyline>
          </svg>
        {/if}
      </div>
      <div class="sparkline-foot muted tiny">
        <span>{weekLabel(0)}</span>
        <span>this week</span>
      </div>

      {#if busiest.length}
        <div class="rows" aria-label="Messages per channel">
          {#each busiest as c, i (c.id)}
            <div class="row" style="--i:{i}">
              <span class="rname">{c.type === "voice" ? c.name : `#${c.name}`}</span>
              <span class="rbar"><span class="rfill" style="--w:{Math.round((c.messages / chMax) * 100)}%"></span></span>
              <span class="rn" use:countUp={c.messages}></span>
            </div>
          {/each}
        </div>
      {/if}

      {#if activity.people?.length}
        <strong class="sub">Who is talking</strong>
        <div class="rows" aria-label="Messages per member">
          {#each activity.people.slice(0, 6) as p, i (p.fingerprint)}
            <div class="row" style="--i:{i}">
              <span class="rname">{p.name || p.fingerprint.slice(0, 9)}</span>
              <span class="rbar"><span class="rfill alt" style="--w:{Math.round((p.messages / peopleMax) * 100)}%"></span></span>
              <span class="rn" use:countUp={p.messages}></span>
            </div>
          {/each}
        </div>
      {/if}

      {#if quiet.length}
        <strong class="sub">Gone quiet</strong>
        <div class="quiet">
          {#each quiet as c, i (c.id)}
            <span class="qchip" style="--i:{i}">
              {c.type === "voice" ? c.name : `#${c.name}`}
              <em>{c.lastUnix ? fmtDate(c.lastUnix) : "never used"}</em>
            </span>
          {/each}
        </div>
        <p class="muted tiny note">
          Nothing said in a month. These are the rooms to merge or archive.
        </p>
      {/if}

      {#if activity.archived > 0}
        <!-- Counted separately and out loud. The old panel reported the live
             count alone, which contradicted the Archive panel three rows away
             when a guild had imported history. -->
        <p class="muted tiny note">
          Plus {activity.archived.toLocaleString()} imported messages in this guild's archive, which the
          numbers above do not count.
        </p>
      {/if}
    {/if}
  </section>

  <hr />

  <section>
    <strong class="label">Props</strong>
    {#if props === null}
      <p class="muted tiny">Loading…</p>
    {:else if props.length}
      <div class="peers">
        {#each props as p (p.fpr)}
          <div class="peer">
            <span class="pname">{p.name}</span>
            <span class="ptag">⭐ {p.n}</span>
          </div>
        {/each}
      </div>
      <p class="muted tiny note">
        Celebratory reactions (🏆 ⭐ 💯 ❤️ 👏) received on each member's messages,
        counted from this device's own history.
      </p>
    {:else}
      <p class="muted tiny">No props given yet — react with 🏆 ⭐ 💯 ❤️ 👏 to change that.</p>
    {/if}
  </section>

  {/if}

  <!-- Guild footprint, devices, network. This is the rest of the page, not a
       fold: the panel is named for it, and reaching it from Connection is
       specifically to see these numbers. -->
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
            <!-- What the device RUNS, not just whether it is here. A fix
                 "shipped to the phone" three times while the phone quietly ran
                 an old build; this is the field that would have said so. -->
            {#if d.appVersion}<span class="pver">{d.appVersion}</span>{/if}
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
        guilds immediately; it erases its own copy only if it comes back online.
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
      <!-- The answer to "my other device never comes online". A machine on a home
           connection is reachable from a phone on mobile data only through a relay;
           without a reservation it is advertising addresses nothing outside its own
           network can dial, and every symptom downstream ("offline", "messages don't
           arrive") follows from that one fact. -->
      <div class="stat">
        <span class="k">Reachable from outside</span>
        <span class="v" class:warn={!ns.relayReserved && !ns.directlyReachable}>
          {#if ns.directlyReachable}
            yes — directly
          {:else if ns.relayReserved}
            yes — via relay
          {:else}
            no relay slot
          {/if}
        </span>
      </div>
      {#if !ns.relayReserved && !ns.directlyReachable}
        <p class="muted tiny note">
          This device isn't advertising a relayed address, so a device on another
          network — a phone on mobile data, say — has no route to it. Check that
          the rendezvous is set and reachable under Connection.
        </p>
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

  {#if ins}
    <hr />
    <!-- Phone-only (S.isMobile gates the sampler): the numbers behind the
         status-bar / gesture-bar padding, for debugging insets on a device
         where logcat isn't reachable. "Pushed" is what the native bridge
         published; "resolved" is what CSS computed from it; "top bar wears"
         is the padding actually on screen. If the app touches a system bar,
         a screenshot of this section says which of the three lost the value. -->
    <section>
      <strong class="label">Display insets</strong>
      <div class="grid">
        <div class="stat">
          <span class="k">Pushed --sa-* (t/b/l/r)</span>
          <span class="v">{ins.saTop} / {ins.saBottom} / {ins.saLeft} / {ins.saRight}</span>
        </div>
        <div class="stat">
          <span class="k">Pushed bar height · floor · kb</span>
          <span class="v">{ins.barsTop} · {ins.floorTop} · {ins.kb}</span>
        </div>
        <div class="stat">
          <span class="k">env() top · bottom</span>
          <span class="v">{ins.envTop} · {ins.envBottom}</span>
        </div>
        <div class="stat">
          <span class="k">Resolved --safe-top · --safe-bottom</span>
          <span class="v">{ins.safeTop} · {ins.safeBottom}</span>
        </div>
        <div class="stat">
          <span class="k">Top bar wears</span>
          <span class="v">{ins.barPad}</span>
        </div>
        <div class="stat">
          <span class="k">Viewport vs screen</span>
          <span class="v">{ins.innerW}×{ins.innerH} / {ins.screenW}×{ins.screenH} @ {ins.dpr}×</span>
        </div>
        <div class="stat">
          <span class="k">Visual viewport bottom</span>
          <span class="v">{ins.vvBottom ?? "n/a"}</span>
        </div>
      </div>
      {#if ins.pushes.length}
        <div class="pushlog">
          {#each ins.pushes as p, i (i)}
            <div class="pushrow muted">
              <span class="pushage">{Math.round((ins.now - p.t) / 1000)}s ago</span>
              {fmtPush(p)}
            </div>
          {/each}
        </div>
        <p class="muted tiny note">
          Native pushes, newest first — a correct top later replaced by 0 shows up
          here as two rows.
        </p>
      {:else}
        <p class="muted tiny note">
          No native pushes recorded — the inset bridge never wrote to this page.
        </p>
      {/if}
    </section>
  {/if}
</RailShell>

{#if confirming}
  <ConfirmDialog
    title="Unlink {confirming.name || 'this device'}?"
    body={"It stops receiving anything new from your guilds straight away — that part is immediate and cannot be undone by whoever holds it.\n\nIt erases its own copy of your messages only when it next comes online with Concord running. A device that stays off keeps what it already downloaded, and this cannot reach it. If the device is in someone else's hands, treat everything on it as already read."}
    confirmLabel="Unlink"
    onConfirm={() => unlink(confirming)}
    onClose={() => (confirming = null)}
  />
{/if}

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
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
    gap: var(--sp-2);
  }
  .stat {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    /* Bandwidth is "↓ 12.5 KB/s ↑ 3.2 KB/s" — long enough to run past the
       tile's right edge and be sliced by it. Wrapping is the honest answer:
       the tile grows a line rather than losing half a number. */
    flex-wrap: wrap;
    gap: 10px;
    padding: var(--sp-2) var(--sp-3);
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
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .v.warn {
    color: var(--warn-text);
  }
  /* ---- insights ---------------------------------------------------------
     One accent, one weight, no chartjunk: the app already draws its data this
     way (the import wizard's histogram), and a stats panel that invents a
     second visual language is a stats panel nobody trusts. The motion is the
     same language arriving: bars grow from the axis, the line draws after. */
  .hero {
    display: flex;
    align-items: baseline;
    gap: var(--sp-3);
    animation: rise 0.45s var(--ease-spring) both;
  }
  .hero-n {
    font-size: var(--fs-display);
    font-weight: 800;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.03em;
    line-height: 1;
    color: var(--accent-hover);
  }
  .hero-l {
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .spark {
    position: relative;
    display: flex;
    align-items: flex-end;
    gap: 5px;
    height: 124px;
    padding: 32px 14px 10px;
    /* Grid lives in the plot, not the chip gutter. */
    background-color: var(--bg-1);
    background-image: repeating-linear-gradient(
      to top,
      color-mix(in srgb, var(--border) 55%, transparent) 0 1px,
      transparent 1px 25%
    );
    background-origin: content-box;
    background-clip: content-box;
    border: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
    border-radius: var(--radius-md);
    box-shadow: inset 0 -1px 0 color-mix(in srgb, var(--accent) 18%, transparent);
    animation: rise 0.45s var(--ease-spring) both;
    animation-delay: 40ms;
  }
  .stip {
    position: absolute;
    top: 7px;
    left: clamp(4.5rem, var(--x), calc(100% - 4.5rem));
    transform: translateX(-50%);
    display: flex;
    align-items: baseline;
    gap: 6px;
    padding: 3px 9px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: 999px;
    font-size: var(--fs-small);
    color: var(--text-muted);
    white-space: nowrap;
    pointer-events: none;
    z-index: 2;
    box-shadow: 0 8px 18px rgb(0 0 0 / 0.22);
  }
  .stip b {
    font-variant-numeric: tabular-nums;
    color: var(--text);
  }
  .sbar {
    flex: 1;
    display: flex;
    align-items: flex-end;
    height: 100%;
    min-width: 0;
    z-index: 0;
  }
  .sfill {
    width: 100%;
    height: var(--h, 2%);
    border-radius: 5px 5px 2px 2px;
    background: linear-gradient(180deg, var(--accent-hover), var(--accent));
    transform-origin: center bottom;
    animation: bar-up 0.55s var(--ease-spring) both;
    animation-delay: calc(60ms + var(--i, 0) * 28ms);
  }
  .sbar.now .sfill {
    box-shadow: 0 0 12px color-mix(in srgb, var(--accent) 42%, transparent);
  }
  .sline {
    position: absolute;
    left: 14px;
    right: 14px;
    top: 32px;
    bottom: 10px;
    width: calc(100% - 28px);
    height: calc(100% - 42px);
    pointer-events: none;
    overflow: visible;
    z-index: 1;
  }
  .sline .sarea {
    fill: color-mix(in srgb, var(--accent) 22%, transparent);
    opacity: 0;
    animation: fade-area 0.4s ease both;
    animation-delay: 0.42s;
  }
  .sline polyline {
    fill: none;
    stroke: color-mix(in srgb, var(--accent-hover) 78%, white);
    stroke-width: 2.25px;
    stroke-linejoin: round;
    stroke-linecap: round;
    vector-effect: non-scaling-stroke;
    stroke-dasharray: 1;
    stroke-dashoffset: 1;
    animation: draw-line 0.7s var(--ease-out) both;
    animation-delay: 0.22s;
  }
  .sparkline-foot {
    display: flex;
    justify-content: space-between;
    gap: var(--sp-2);
    animation: rise 0.4s var(--ease-spring) both;
    animation-delay: 80ms;
  }
  .sub {
    display: block;
    margin-top: var(--sp-2);
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 5px;
    animation: rise 0.4s var(--ease-spring) both;
    animation-delay: 130ms;
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    font-size: var(--fs-compact);
  }
  .rname {
    flex: 0 0 34%;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-muted);
  }
  .rbar {
    flex: 1;
    height: 8px;
    min-width: 0;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  .rfill {
    display: block;
    height: 100%;
    width: var(--w, 0%);
    border-radius: 999px;
    background: var(--accent);
    transform-origin: left center;
    animation: bar-across 0.5s var(--ease-spring) both;
    animation-delay: calc(160ms + var(--i, 0) * 36ms);
  }
  /* People get the second hue, so "which channel" and "which person" are not
     the same bar in two lists. */
  .rfill.alt {
    background: color-mix(in srgb, var(--accent) 55%, var(--ok, var(--accent-hover)));
  }
  @media (pointer: fine) {
    .row:hover .rfill {
      filter: brightness(1.18);
    }
    .spark.peek .sfill {
      opacity: 0.38;
    }
    .spark.peek .sbar.hot .sfill {
      opacity: 1;
      filter: brightness(1.12);
    }
    .spark.peek .sline {
      opacity: 0.55;
    }
  }
  @keyframes bar-up {
    from {
      transform: scaleY(0);
    }
    to {
      transform: scaleY(1);
    }
  }
  @keyframes bar-across {
    from {
      transform: scaleX(0);
    }
    to {
      transform: scaleX(1);
    }
  }
  @keyframes draw-line {
    to {
      stroke-dashoffset: 0;
    }
  }
  @keyframes fade-area {
    to {
      opacity: 1;
    }
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(8px) scale(0.97);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .hero,
    .spark,
    .sparkline-foot,
    .rows,
    .qchip,
    .sfill,
    .rfill,
    .sline polyline,
    .sline .sarea {
      animation: none;
    }
    .sfill,
    .rfill {
      transform: none;
    }
    .sline polyline {
      stroke-dashoffset: 0;
    }
    .sline .sarea {
      opacity: 1;
    }
  }
  .rn {
    flex: 0 0 auto;
    font-variant-numeric: tabular-nums;
    color: var(--text-faint);
  }
  .quiet {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }
  .qchip {
    display: inline-flex;
    align-items: baseline;
    gap: 5px;
    padding: 3px 9px;
    border-radius: 999px;
    background: var(--bg-3);
    font-size: var(--fs-small);
    animation: rise 0.35s var(--ease-spring) both;
    animation-delay: calc(200ms + var(--i, 0) * 40ms);
  }
  .qchip em {
    font-style: normal;
    color: var(--text-faint);
  }
  .peers {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    margin-top: var(--sp-2);
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
    gap: var(--sp-2);
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
  .pver {
    flex-shrink: 0;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-micro);
    font-variant-numeric: tabular-nums;
  }
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
    border-radius: var(--radius-sm);
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
    margin-top: var(--sp-2);
  }
  /* Inset push history: dense monospace rows, newest first. */
  .pushlog {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-top: var(--sp-2);
  }
  .pushrow {
    font-family: var(--mono, monospace);
    font-size: var(--fs-tiny);
    font-variant-numeric: tabular-nums;
  }
  .pushage {
    display: inline-block;
    min-width: 52px;
    color: var(--text-faint);
  }
</style>
