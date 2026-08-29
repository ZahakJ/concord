<script>
  // How this node reaches the outside world: the rendezvous address, and the
  // local-only extras that depend on what's installed on this machine.
  import SettingsShell from "./SettingsShell.svelte";
  import Icon from "../Icon.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import { onMount } from "svelte";
  import { api } from "../lib/api.js";
  import { flash, S } from "../lib/state.svelte.js";

  let { onClose, onSaved } = $props();

  let bootstrap = $state("");
  let publicDht = $state(false);
  // A number input binds to a number, or to null when it is empty or holds
  // something that isn't one — which is exactly the "pick one for me" case.
  let port = $state(null);
  onMount(async () => {
    try {
      bootstrap = ((await api.getBootstrap()) || []).join("\n");
      publicDht = !!(await api.getPublicDht());
      port = (await api.getListenPort()) || null;
    } catch {
      /* ignore */
    }
  });

  let savedPort = $state("");
  const portOk = $derived(port === null || (Number.isInteger(port) && port >= 1024 && port <= 65535));
  async function savePort() {
    if (!portOk) return;
    try {
      await api.setListenPort(port ?? 0);
      savedPort = port ? String(port) : "off";
      setTimeout(() => (savedPort = ""), 2400);
    } catch (err) {
      flash(err);
    }
  }

  // The switch flips optimistically and rolls back if the write fails, so the
  // row never shows a state the disk doesn't agree with.
  async function togglePublicDht() {
    const next = !publicDht;
    publicDht = next;
    try {
      await api.setPublicDht(next);
    } catch (err) {
      publicDht = !next;
      flash(err);
    }
  }

  // Display-first: the address shows as a copyable chip; editing (a once-ever
  // action for self-hosters) hides behind the pencil.
  let editing = $state(false);
  let draft = $state("");
  let copied = $state(false);

  function startEdit() {
    draft = bootstrap;
    editing = true;
  }
  function copyAddr() {
    navigator.clipboard?.writeText(bootstrap);
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }
  async function save() {
    try {
      await api.setBootstrapLive(draft);
      bootstrap = draft;
      editing = false;
      onSaved?.(); // parent toasts the confirmation
    } catch (err) {
      flash(err);
    }
  }

  // Live shape-check while editing: every non-blank line should be a
  // /…/p2p/<PeerID> multiaddr.
  const lines = $derived(draft.split("\n").filter((l) => l.trim()));
  const ok = $derived(lines.length > 0 && lines.every((l) => l.trim().startsWith("/") && l.includes("/p2p/")));
  const bad = $derived(lines.length > 0 && !ok);
</script>

<SettingsShell title="Connection" here="connection" {onClose}>
  <SettingGroup
    label="Rendezvous server"
    info="The tiny relay that lets friends on other networks find you. You only need to set this if you host it yourself — friends get it automatically from your invite code. Blank means same-Wi-Fi only."
  >
    <div class="conn">
      {#if editing}
        <div class="code-wrap" class:ok class:bad>
          <textarea
            class="code-box"
            rows="3"
            placeholder="/dns4/relay.example.org/tcp/4001/p2p/12D3Koo…"
            bind:value={draft}
          ></textarea>
          {#if ok}
            <span class="code-state"><Icon name="check" size={13} /> address looks good</span>
          {:else if bad}
            <span class="code-state">should start with /dns or /ip4 and contain /p2p/…</span>
          {/if}
        </div>
        <div class="foot">
          <span class="hint">Applies live to new connections.</span>
          <button class="ghost cancel" onclick={() => (editing = false)}>Cancel</button>
          <button class="save" disabled={bad} onclick={save}>Save</button>
        </div>
      {:else if bootstrap.trim()}
        <div class="addr-row">
          <code class="addr" title={bootstrap}>{bootstrap.trim()}</code>
          <button class="addr-act" onclick={copyAddr} aria-label="Copy address">
            {#if copied}<Icon name="check" size={15} />{:else}<Icon name="copy" size={15} />{/if}
          </button>
          <button class="addr-act" onclick={startEdit} aria-label="Edit address">
            <Icon name="edit" size={15} />
          </button>
        </div>
      {:else}
        <div class="addr-row empty">
          <span class="hint">Not set — you can only reach friends on the same Wi-Fi.</span>
          <button class="save" onclick={startEdit}>Set address</button>
        </div>
      {/if}
    </div>
  </SettingGroup>

  <SettingGroup
    label="Direct connection"
    note="Pinning a port publishes this computer's public IP address — to peers, and inside the invite codes you hand out. Applies after a restart."
  >
    <SettingRow
      icon="bolt"
      title="Fixed port"
      sub="Leave blank to keep picking one automatically"
      info="Normally Concord picks a new port every start, which works but sends your traffic the long way round. Pin one, forward it to this computer in your router, and friends connect straight to you — no relay, no rendezvous, less delay. The address is published whether or not the router rule actually works, so only turn this on if you're content for the people you invite to learn it."
    >
      <div class="port">
        <input
          class="port-box"
          class:bad={!portOk}
          type="number"
          inputmode="numeric"
          min="1024"
          max="65535"
          placeholder="e.g. 4001"
          bind:value={port}
          onblur={savePort}
        />
        <button class="save" disabled={!portOk} onclick={savePort}>Save</button>
      </div>
    </SettingRow>
    <!-- The fallback keeps the app running, so nothing else would ever hint
         that the forwarded port is dead this session. -->
    {#if S.netStatus?.pinnedPortTaken}
      <p class="note-line warn">
        The fixed port wasn't free when Concord started — something else on this computer already
        had it, possibly a second copy of Concord — so this session is on an automatic port and your
        router rule leads nowhere. Pick another port, or close the other program, and restart.
      </p>
    {/if}
    {#if !portOk}
      <p class="note-line">Pick a number between 1024 and 65535, or leave it blank.</p>
    {:else if savedPort}
      <p class="note-line">
        {#if savedPort === "off"}
          Back to an automatic port on the next restart.
        {:else}
          Saved. Forward TCP <b>and</b> UDP port {savedPort} to this computer, then restart Concord.
        {/if}
      </p>
    {/if}
  </SettingGroup>

  <SettingGroup
    label="Fallback discovery"
    note="Joining the public network makes your peer ID and IP visible to strangers there. Your messages stay encrypted either way. Applies after a restart."
  >
    <SettingRow
      icon="search"
      title="Use public DHT nodes"
      sub="Find new peers without any Concord server — at the cost of being visible"
      info="You don't need this to keep existing friends working: Concord already remembers everyone you've connected to and re-dials them itself, even with the rendezvous gone. This is for the other case — reaching someone you have never met when there's no server left. It works by joining the public IPFS network, where anyone watching can tell this address runs Concord."
      checked={publicDht}
      onclick={togglePublicDht}
    />
  </SettingGroup>

  <SettingGroup label="Diagnostics">
    <SettingRow
      icon="poll"
      title="Stats &amp; diagnostics"
      sub="Storage, peers &amp; connection health"
      to="stats"
      from="connection"
    />
  </SettingGroup>

</SettingsShell>

<style>
  .conn {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px 14px;
  }
  .hint {
    font-size: var(--fs-compact);
    line-height: 1.5;
    color: var(--text-muted);
  }
  /* Display state: the address as a copyable chip with quiet icon actions. */
  .addr-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .addr {
    flex: 1;
    min-width: 0;
    font-family: var(--mono-font);
    font-size: var(--fs-compact);
    line-height: 1.4;
    padding: 10px 12px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--bg-0) 42%, var(--bg-3));
    border: 1px solid color-mix(in srgb, var(--border) 62%, transparent);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .addr-act {
    flex-shrink: 0;
    width: 36px;
    height: 36px;
    padding: 0;
    display: grid;
    place-items: center;
    border-radius: var(--radius-md);
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .addr-act:hover,
  .addr-act:active {
    background: var(--bg-3);
    color: var(--text);
  }
  .port {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .port-box {
    width: 92px;
    font-family: var(--mono-font);
    font-size: var(--fs-compact);
    padding: 7px 10px;
    border-radius: var(--radius-md);
    text-align: right;
  }
  /* Also when focused — you are typing the bad value, which is when the field
     most needs to say so; the global accent halo would otherwise win. */
  .port-box.bad,
  .port-box.bad:focus {
    border-color: color-mix(in srgb, var(--danger) 55%, transparent);
    box-shadow:
      inset 0 1px 2px rgb(0 0 0 / 0.08),
      0 0 0 3px color-mix(in srgb, var(--danger) 22%, transparent);
  }
  .note-line {
    margin: 0;
    padding: 0 14px 12px;
    font-size: var(--fs-small);
    line-height: 1.5;
    color: var(--text-muted);
  }
  .note-line.warn {
    color: var(--danger-text);
  }
  .addr-row.empty {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }
  .code-wrap {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .code-box {
    width: 100%;
    box-sizing: border-box;
    min-height: 84px;
    font-family: var(--mono-font);
    font-size: var(--fs-compact);
    line-height: 1.65;
    white-space: pre-wrap;
    word-break: break-all;
    resize: none; /* it's an address, not an essay */
    border-radius: var(--radius-md);
    padding: 12px 14px;
  }
  /* The field answers back while you type: a green ring when the address
     parses, a warm hint when it doesn't. */
  .code-wrap.ok .code-box {
    border-color: color-mix(in srgb, var(--ok) 55%, transparent);
  }
  .code-wrap.bad .code-box {
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .code-state {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .code-wrap.ok .code-state {
    color: var(--ok-text);
  }
  .code-wrap.bad .code-state {
    color: color-mix(in srgb, var(--danger) 80%, var(--text));
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-3);
  }
  .foot .hint {
    flex: 1;
    min-width: 0;
  }
  .cancel {
    padding: 7px 14px;
    font-size: var(--fs-ui);
  }
  .save {
    flex-shrink: 0;
    padding: 7px 18px;
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  /* SettingRow drops the control onto its own line at this width — spend that
     line, rather than clipping "e.g. 4001" to "e.g. 400" in a 92px box. */
  @media (pointer: coarse), (max-width: 768px) {
    .port {
      width: 100%;
    }
    .port-box {
      flex: 1;
      width: auto;
    }
    /* Copy and Edit sit side by side and do very different things (one is
       silent, the other swaps the rendezvous address into an edit box). The
       modal's touch floor made them 44 tall but left them 36 wide, so their
       centres were 42px apart — under the fingertip minimum on the axis that
       actually has a neighbour. */
    .addr-act {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    .addr-row {
      gap: 10px;
    }
  }
</style>
