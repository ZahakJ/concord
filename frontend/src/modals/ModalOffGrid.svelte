<script>
  // Settings → Off-grid links: Concord's messages over Bluetooth, radio,
  // satellite and plain files, for when the internet is not there.
  //
  // Every link is off until switched on here — this page is the only thing
  // that powers a radio or opens a microphone. What crosses any of them is
  // the same end-to-end ciphertext gossip carries; the link never sees a word.
  import RailShell from "./RailShell.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import { flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { statusLine, usable, fmtCost } from "../lib/offgrid.js";

  let { onClose } = $props();

  let view = $state({ links: [], offline: false, relay: true });
  let paying = $state(null); // link whose paid outbox awaits a yes
  let fileInput = $state();

  async function refresh() {
    try {
      view = await api.offGrid();
    } catch (err) {
      flash(err);
    }
  }
  refresh();
  const timer = setInterval(refresh, 3000);
  $effect(() => () => clearInterval(timer));

  async function act(fn) {
    try {
      await fn();
    } catch (err) {
      flash(err);
    }
    await refresh();
  }

  function release(l) {
    if (l.caps.costMicroUSD) paying = l;
    else act(() => api.flushLink(l.name, l.queued, 0));
  }

  async function saveFile(l) {
    await act(async () => {
      const b64 = await api.exportLink(l.name);
      if (!b64) return;
      const a = document.createElement("a");
      a.href = "data:application/octet-stream;base64," + b64;
      a.download = `concord-${new Date().toISOString().slice(0, 10)}.bundles`;
      a.click();
      flash("Saved — carry the file to the other device and import it there", "success");
    });
  }

  async function importFile(e) {
    const f = e.target.files?.[0];
    e.target.value = "";
    if (!f) return;
    const bytes = new Uint8Array(await f.arrayBuffer());
    let bin = "";
    for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
    await act(async () => {
      const n = await api.importLinkPack(btoa(bin));
      flash(`Imported ${n} sealed message${n === 1 ? "" : "s"}`, "success");
    });
  }
</script>

<RailShell title="Off-grid links" {onClose}>
  <SettingGroup
    label="Mode"
    info="Messages still travel end-to-end encrypted on every link. A phone relaying for you can't read them, and neither can anyone listening to the radio."
  >
    <SettingRow
      icon="lock"
      title="Offline mode"
      sub="Use only local links — no satellite, nothing paid"
      checked={view.offline}
      onclick={() => act(() => api.setLinksOffline(!view.offline))}
    />
    <SettingRow
      icon="members"
      title="Relay for people nearby"
      sub="Pass on sealed messages a few hops; you can't read them"
      checked={view.relay}
      onclick={() => act(() => api.setLinksRelay(!view.relay))}
    />
  </SettingGroup>

  <SettingGroup label="Links">
    {#each view.links as l (l.name)}
      <SettingRow
        title={l.label}
        sub={statusLine(l)}
        checked={l.enabled}
        disabled={!usable(l)}
        onclick={() => act(() => api.setLinkEnabled(l.name, !l.enabled))}
      />
      {#if l.enabled && l.queued > 0}
        {#if l.name === "file"}
          <SettingRow icon="download" title="Save {l.queued} queued as a file" onclick={() => saveFile(l)} />
        {:else}
          <SettingRow
            icon="send"
            title="Send {l.queued} queued"
            sub={l.caps.costMicroUSD ? `Costs ${fmtCost(l.queuedCostMicroUSD)}` : "Plays or transmits now"}
            onclick={() => release(l)}
          />
        {/if}
      {/if}
      {#if l.name === "file" && l.enabled}
        <SettingRow icon="folder" title="Import a file" sub="Messages someone carried to you" onclick={() => fileInput.click()} />
      {/if}
    {/each}
  </SettingGroup>
  <input type="file" accept=".bundles" hidden bind:this={fileInput} onchange={importFile} />
</RailShell>

{#if paying}
  <ConfirmDialog
    title="Send over {paying.label}?"
    body={`${paying.queued} frame${paying.queued === 1 ? "" : "s"} will be sent. This is billed by your provider: ${fmtCost(paying.queuedCostMicroUSD)}.`}
    confirmLabel="Send for {fmtCost(paying.queuedCostMicroUSD)}"
    onConfirm={() => {
      const l = paying;
      paying = null;
      act(() => api.flushLink(l.name, l.queued, l.queuedCostMicroUSD));
    }}
    onClose={() => (paying = null)}
  />
{/if}
