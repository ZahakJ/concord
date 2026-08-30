<script>
  // Export history — a PAGE, because of what it does.
  //
  // It used to be a chevron row in the hub, identical in shape to the ten rows
  // beside it that merely open a panel. It took no confirmation: one click
  // wrote every channel's full plaintext history to the disk and said so with a
  // toast that named neither the file nor where it went. Two rows below it,
  // Message history spends three paragraphs explaining that retention "protects
  // you against a member's device being lost or seized later" — and this undid
  // that in one click with no ceremony at all.
  //
  // So the ceremony is the screen. It states what the file will contain and how
  // big it is BEFORE anything is written — measured, not estimated, because the
  // transcript is built in memory here and the button only hands over what is
  // already in hand — it says the honest sentence out loud, it is a button
  // rather than a chevron, and the toast names the file.
  import RailShell from "./RailShell.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { saveText } from "../lib/savefile.js";
  import { plural } from "../lib/plural.js";
  import { fmtBytes, fmtCount } from "../lib/chronicle.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const filename = $derived(`${(g?.name || "guild").replace(/[^\w.-]+/g, "-")}-history.md`);

  let md = $state("");
  let bytes = $state(0);
  let building = $state(true);
  let err = $state("");
  let saving = $state(false);
  let messages = $state(-1);
  let archived = $state(0);

  // The transcript itself, up front. It is the only way to say "1.4 MB" and
  // mean it, and it costs a query over rows already on this device — the same
  // read the button was making anyway, just before you decide instead of after.
  $effect(() => {
    const id = S.activeGuildId;
    if (!id) return;
    let live = true;
    building = true;
    err = "";
    api.exportMarkdown(id, "").then(
      (text) => {
        if (!live) return;
        md = text || "";
        bytes = new TextEncoder().encode(md).length;
        building = false;
      },
      (e) => {
        if (!live) return;
        err = String(e?.message || e).replace(/^app:\s*/, "");
        building = false;
      },
    );
    // Counts come from the same local query the Insights page runs; they are
    // context for the size, not a second source of truth about it.
    api.guildInsights(id).then(
      (ins) => {
        if (!live || !ins) return;
        messages = (ins.channels || []).reduce((n, c) => n + (c.messages || 0), 0);
        archived = ins.archived || 0;
      },
      () => {
        /* the size and the channel list stand on their own */
      },
    );
    return () => (live = false);
  });

  const channels = $derived((g?.channels || []).filter((c) => !c.parent).length);

  async function run() {
    if (!md || saving) return;
    saving = true;
    try {
      const how = await saveText(filename, md, "text/markdown");
      if (how === "file") flash(`Saved ${filename}`, "success");
      else if (how === "clipboard") flash(`${filename} copied to the clipboard`, "success");
      // "" is the picker dismissed, or nothing worked. Claiming a save then
      // would be the lie this whole page exists to stop.
    } catch (e) {
      flash(e);
    }
    saving = false;
  }
</script>

<RailShell title="Export history" {onClose}>
  <p class="lead">
    This writes <strong>{g?.name || "this guild"}</strong>'s conversation to a single Markdown
    file on this device.
  </p>

  <div class="card">
    <div class="rows">
      <div class="r">
        <span class="k">File</span>
        <span class="v mono">{filename}</span>
      </div>
      <div class="r">
        <span class="k">Size</span>
        <span class="v">
          {#if building}
            Working it out…
          {:else if err}
            —
          {:else}
            {fmtBytes(bytes)}
          {/if}
        </span>
      </div>
      <div class="r">
        <span class="k">Contains</span>
        <span class="v">
          {plural(channels, "channel")}{messages >= 0 ? `, ${fmtCount(messages)} ${messages === 1 ? "message" : "messages"}` : ""}
          {#if archived > 0}
            <em class="note">Plus {fmtCount(archived)} archived messages this guild imported.</em>
          {/if}
          <em class="note">
            Text only — pictures, files and voice notes stay where they are and are named, not
            copied.
          </em>
        </span>
      </div>
    </div>
  </div>

  <!-- The sentence the row never said. It is the whole reason this is a page. -->
  <p class="warn-line">
    <Icon name="alert" size={14} />
    <span>
      This writes an <strong>unencrypted</strong> copy of everything to your disk. Anyone who can
      read that file can read the guild, and no retention setting reaches it once it is written.
    </span>
  </p>

  {#if err}
    <p class="err">{err}</p>
  {/if}

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button disabled={building || !!err || saving} onclick={run}>
      <Icon name="download" size={14} />
      {saving ? "Saving…" : building ? "Preparing…" : `Export ${fmtBytes(bytes)}`}
    </button>
  </div>
</RailShell>

<style>
  .lead {
    margin: 0;
    line-height: 1.55;
  }
  .card {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .rows {
    display: flex;
    flex-direction: column;
  }
  .r {
    display: grid;
    grid-template-columns: 92px minmax(0, 1fr);
    gap: var(--sp-3);
    padding: 10px 14px;
    align-items: baseline;
  }
  .r + .r {
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .k {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .v {
    min-width: 0;
    font-size: var(--fs-ui);
    overflow-wrap: anywhere;
  }
  .mono {
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-small);
  }
  .note {
    display: block;
    margin-top: 3px;
    font-style: normal;
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .warn-line {
    display: flex;
    gap: var(--sp-2);
    align-items: flex-start;
    margin: 0;
    padding: 10px 12px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--warn) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--warn) 32%, transparent);
    color: var(--warn-text);
    font-size: var(--fs-small);
    line-height: 1.5;
  }
  .warn-line :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .err {
    margin: 0;
    color: var(--danger-text);
    font-size: var(--fs-small);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: auto;
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
</style>
