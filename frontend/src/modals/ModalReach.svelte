<script>
  // "Can people reach me?" — the diagnosis, not the manual.
  //
  // The three settings that decide reachability live in three different places
  // in Connection, each explaining itself only to someone who already opened
  // the right one. The node knows which situation the user is actually in, so
  // this panel asks it, says the answer in a sentence, and offers the single
  // next action for that answer. The reasoning for each rung stays behind an
  // info dot, the way every other settings panel handles the long version.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import { onMount } from "svelte";
  import { api } from "../lib/api.js";
  import { flash, openPanel } from "../lib/state.svelte.js";

  let { onClose } = $props();

  let r = $state(null);
  let failed = $state(false);
  let busy = $state(false);

  async function check() {
    try {
      r = await api.reachability();
      failed = false;
    } catch {
      failed = true;
    }
  }
  onMount(check);

  // Nudge re-dials the rendezvous and drains the mailbox; the re-check after it
  // is what turns the button into an answer rather than a hope. The pause is
  // the dial timeout, not decoration — asking again immediately would report
  // the same "not answering" every time.
  async function reconnect() {
    busy = true;
    try {
      await api.nudge();
      await new Promise((done) => setTimeout(done, 2500));
      await check();
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  function openConnection() {
    openPanel("connection", "reach");
  }

  // The seven situations, in the order the node rules them out.
  //
  // The top two rungs are the only ones that rest on a measurement: inboundIPv4
  // / inboundIPv6 mean somebody out there opened a connection straight to this
  // machine, unprompted, and it arrived. Nothing else here is evidence of
  // anything — `reachable` reads our own address and infers, which is why it
  // sits below and says so.
  //
  // That inference is wrong in both directions and this panel used to inherit
  // both errors. Home routers hand out a globally routable IPv6 address and drop
  // every unsolicited packet to it, so "you have a public address" was told to
  // people nobody could reach; and a home server behind a port-forward holds a
  // private 192.168 address while the internet arrives on it all day, so the
  // people most obviously reachable were told to go set up a rendezvous. Proof
  // outranks the address reading for exactly that reason.
  const proven = $derived(!!r && (r.inboundIPv4 || r.inboundIPv6));
  const verdict = $derived.by(() => {
    if (!r) return null;
    if (r.inboundIPv4) return "provenV4";
    if (r.inboundIPv6) return "provenV6";
    if (r.reachable) return r.publicIPv4 ? "direct" : "directV6";
    if (r.hasRendezvous) return r.rendezvousReached ? "relayed" : "relayDown";
    return r.lanDiscovery ? "lanOnly" : "stuck";
  });

  // Reachable on a port that changes every launch: the address inside codes you
  // already handed out is stale the moment Concord restarts.
  const portDrifts = $derived(!!r && (r.reachable || proven) && !r.pinnedPort);

  const said = {
    provenV4: {
      tone: "ok",
      icon: "check",
      head: "People are reaching you directly.",
      body: "Somebody on the internet opened a connection straight to this computer this session — not through your rendezvous, not through anyone's relay. That is measured rather than assumed: it already happened, so the route in demonstrably works.",
    },
    provenV6: {
      tone: "ok",
      icon: "check",
      head: "People are reaching you directly, over IPv6.",
      body: "Somebody on the internet opened a connection straight to this computer over IPv6 this session, so that route demonstrably works — your router is letting it through. What it cannot cover is a friend whose own network does not speak IPv6, and many still do not.",
    },
    direct: {
      tone: "ok",
      icon: "check",
      head: "Your address says people can reach you directly.",
      body: "This computer holds a public address of its own rather than sitting behind your router, so anyone you invite should connect straight to you with nothing in between. Nobody has actually come in that way yet this session, so this is what your address says rather than something Concord has watched work — a firewall on this machine could still be refusing the port.",
    },
    directV6: {
      tone: "ok",
      icon: "check",
      head: "Some people may be able to reach you directly, over IPv6.",
      body: "Your only public address is an IPv6 one, nothing has come in on it yet, and two things outside Concord decide whether it ever will: your router has to allow incoming connections to it, and most block them by default; and your friend's network has to speak IPv6 at all, which many still do not.",
    },
    relayed: {
      tone: "ok",
      icon: "check",
      head: "Friends on other networks reach you through your rendezvous.",
      body: "Your router will not accept incoming connections, so a rendezvous node introduces you and carries the traffic. It is encrypted end to end, so the rendezvous cannot read a word of it.",
    },
    relayDown: {
      tone: "warn",
      icon: "alert",
      head: "Your rendezvous is set but not answering, so nobody off this network can reach you.",
      body: "It is the only thing that could introduce you to someone elsewhere, and right now nothing responds at its address.",
    },
    lanOnly: {
      tone: "warn",
      icon: "alert",
      head: "Only people on this same Wi-Fi can reach you.",
      body: "Your router will not accept incoming connections and no rendezvous is set, so there is nothing to introduce you to anyone outside this network.",
    },
    stuck: {
      tone: "bad",
      icon: "alert",
      head: "Nobody can reach you right now.",
      body: "No rendezvous is set, and finding peers on the local network is unavailable on this device too, so there is no route in at all.",
    },
  };

  const now = $derived(verdict ? said[verdict] : null);

  const lanSub = $derived(
    !r ? "" : r.lanDiscovery ? "Working on this device" : "Unavailable on this device",
  );
  const portSub = $derived.by(() => {
    if (!r) return "";
    if (!r.pinnedPort) {
      // "Concord takes a new one each start" was true and said nothing about
      // what it costs. What it costs is that issued invite codes go stale.
      return r.reachable || proven
        ? "No fixed port — invite codes you hand out go stale when Concord restarts"
        : "No fixed port — Concord takes a new one each start";
    }
    if (r.pinnedPortTaken) return `Port ${r.pinnedPort} was already in use at startup`;
    return r.reachable || proven
      ? `Port ${r.pinnedPort}, and it stays put across restarts`
      : `Port ${r.pinnedPort} pinned`;
  });
  const rvSub = $derived(
    !r
      ? ""
      : !r.hasRendezvous
        ? "None set"
        : r.rendezvousReached
          ? "Set and answering"
          : "Set, but not answering",
  );
</script>

<Modal title="Can people reach me?" {onClose} wide>
  {#if failed}
    <p class="lead">Concord could not read this node's connection state. Unlock the app and try again.</p>
  {:else if !now}
    <p class="lead">Checking…</p>
  {:else}
    <div class="verdict {now.tone}">
      <span class="mark"><Icon name={now.icon} size={17} /></span>
      <div class="say">
        <strong>{now.head}</strong>
        <p>{now.body}</p>
        {#if verdict === "directV6" || verdict === "provenV6"}
          <p>
            {#if r.hasRendezvous}
              When IPv6 is not available to them your rendezvous introduces you instead, so treat
              it as a bonus route rather than the one to count on.
            {:else}
              Nothing else is set up to introduce you, so anyone whose router or network cannot
              use IPv6 will not reach you at all. A rendezvous covers them.
            {/if}
          </p>
        {/if}
        {#if verdict === "relayDown"}
          <button class="act" disabled={busy} onclick={reconnect}>
            {busy ? "Trying…" : "Try reconnecting"}
          </button>
        {:else if verdict === "lanOnly" || verdict === "stuck" || ((verdict === "directV6" || verdict === "provenV6") && !r.hasRendezvous)}
          <button class="act" onclick={openConnection}>Set a rendezvous address</button>
        {:else if portDrifts}
          <button class="act" onclick={openConnection}>Pin a port</button>
        {:else}
          <span class="quiet">Nothing to do.</span>
        {/if}
      </div>
    </div>

    {#if portDrifts}
      <p class="lead warn">
        You are reachable on a port Concord picked at random this launch, and it will pick a
        different one next time. Every invite code you have already handed out carries the old
        one, so the direct route in those codes stops working the moment you restart.
        {#if r.hasRendezvous}
          Friends fall back to your rendezvous, which still gets them in — just the slower way.
        {:else}
          With no rendezvous set there is nothing to fall back to, so those codes stop working
          entirely.
        {/if}
        Pinning a port keeps the codes you give out valid.
      </p>
    {/if}

    {#if r.pinnedPortTaken}
      <p class="lead warn">
        You pinned port {r.pinnedPort}, but something else on this computer already had it when
        Concord started, so the rule you added to your router leads nowhere this session. Pick
        another port, or close the other program, and restart.
      </p>
    {/if}

    <SettingGroup
      label="The three ways in"
      info="Reaching you is a different problem from finding you. These are the three routes by which someone's messages can actually arrive; whichever one applies, everything stays encrypted end to end."
    >
      <SettingRow
        icon="members"
        title="Same network"
        sub={lanSub}
        info="Concord shouts a question at the local network and dials whatever answers back. No server, no internet, no setup. It never starts on Android, whose sandbox refuses the socket this needs, and many workplace and campus networks drop the packets, so in practice this is desktop to desktop on a home network."
      />
      <SettingRow
        icon="bolt"
        title="A port you forward"
        sub={portSub}
        info="Pin one port, add a rule in your router sending it to this computer, and the invite codes you hand out carry your own address so friends dial you directly. No server is involved anywhere. The cost is that your home IP address is inside every invite code you give away, whether or not the router rule works."
      />
      <SettingRow
        icon="door"
        title="Somebody's rendezvous"
        sub={rvSub}
        info="A rendezvous is a small always-on node that introduces two people who cannot dial each other, and forwards their traffic when neither side can accept a connection. It only ever sees ciphertext. It does not have to be yours: a friend's works, and joining with their invite code sets yours to theirs automatically."
      />
    </SettingGroup>

    <SettingGroup
      label="Finding, not reaching"
      note="Discovery answers where somebody is. Reachability answers whether anything can get to them. This one is only the first."
    >
      <SettingRow
        icon="search"
        title="Public IPFS DHT"
        sub={r.publicDHT ? "On" : "Off"}
        info="This joins a public network of strangers' nodes that will remember where you are, so two people who have never met can find each other with no Concord server alive. It does not make you reachable: if neither side accepts incoming connections, knowing the address changes nothing. The price is metadata — your peer ID and addresses sit on a public network, and the key Concord advertises under is guessable, so an observer can list Concord nodes."
      />
    </SettingGroup>

    <SettingGroup label="Settings">
      <SettingRow
        icon="gear"
        title="Connection settings"
        sub="Rendezvous address, fixed port, fallback discovery"
        to="connection"
        from="reach"
      />
    </SettingGroup>
  {/if}
</Modal>

<style>
  .lead {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .lead.warn {
    color: var(--warn-text);
  }
  /* The answer, before any of the options: one tinted card whose colour is the
     verdict, so the situation lands before a word is read. */
  .verdict {
    display: flex;
    gap: 11px;
    padding: 13px 14px;
    border-radius: var(--radius-md);
    background: var(--bg-1);
    border: 1px solid var(--border);
  }
  .verdict.ok {
    background: color-mix(in srgb, var(--ok) 10%, var(--bg-1));
    border-color: color-mix(in srgb, var(--ok) 38%, transparent);
  }
  .verdict.warn {
    background: color-mix(in srgb, var(--warn) 11%, var(--bg-1));
    border-color: color-mix(in srgb, var(--warn) 38%, transparent);
  }
  .verdict.bad {
    background: color-mix(in srgb, var(--danger) 10%, var(--bg-1));
    border-color: color-mix(in srgb, var(--danger) 38%, transparent);
  }
  .mark {
    flex: none;
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    border-radius: var(--radius-md);
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .verdict.ok .mark {
    color: var(--ok-text);
  }
  .verdict.warn .mark {
    color: var(--warn-text);
  }
  .verdict.bad .mark {
    color: var(--danger-text);
  }
  .say {
    display: flex;
    flex-direction: column;
    gap: 7px;
    min-width: 0;
  }
  .say strong {
    font-size: var(--fs-ui);
    line-height: 1.45;
  }
  .say p {
    margin: 0;
    font-size: var(--fs-compact);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .act {
    align-self: flex-start;
    margin-top: 2px;
    padding: 7px 16px;
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .act:disabled {
    opacity: 0.6;
  }
  .quiet {
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  @media (pointer: coarse), (max-width: 768px) {
    .act {
      align-self: stretch;
      min-height: var(--tap-min);
    }
  }
</style>
