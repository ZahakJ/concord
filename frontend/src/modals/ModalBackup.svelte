<script>
  // Backup and restore of the whole account's history.
  //
  // Not the same thing as "Export history" in the channel menu: that writes a
  // readable Markdown transcript of one conversation, which is lossy and cannot
  // be restored from. This writes a sealed archive of everything and can put it
  // back. The copy keeps them apart because someone who confuses the two finds
  // out on the worst possible day.
  import RailShell from "./RailShell.svelte";
  import { saveBlob } from "../lib/savefile.js";
  import Icon from "../Icon.svelte";
  import { S, flash, refreshGuilds } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { plural } from "../lib/plural.js";

  let { onClose } = $props();

  let pass = $state("");
  let confirmPass = $state("");
  let withAttachments = $state(false);
  let busy = $state("");
  let restorePass = $state("");
  let fileInput = $state(null);
  let result = $state(null);

  const canBackUp = $derived(pass.length >= 8 && pass === confirmPass);

  function stamp() {
    const d = new Date();
    const p = (n) => String(n).padStart(2, "0");
    return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}`;
  }

  async function backUp() {
    busy = "backup";
    result = null;
    try {
      const r = await api.exportArchive(pass, withAttachments);
      // The archive arrives base64 because the RPC surface is JSON; turn it
      // back into bytes so the file on disk is the archive itself.
      const bin = Uint8Array.from(atob(r.data), (c) => c.charCodeAt(0));
      const how = await saveBlob(
        `concord-backup-${stamp()}.archive`,
        new Blob([bin], { type: "application/octet-stream" }),
      );
      if (!how) {
        flash("The backup wasn't written — nothing was saved");
        return;
      }
      result = { kind: "backup", ...r.stats };
      pass = confirmPass = "";
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }

  async function restore(e) {
    const file = e.target.files?.[0];
    if (!file) return;
    if (!restorePass) {
      flash("Enter the archive's passphrase first");
      e.target.value = "";
      return;
    }
    busy = "restore";
    result = null;
    try {
      const buf = new Uint8Array(await file.arrayBuffer());
      let s = "";
      for (let i = 0; i < buf.length; i += 0x8000) {
        s += String.fromCharCode.apply(null, buf.subarray(i, i + 0x8000));
      }
      const stats = await api.importArchive(btoa(s), restorePass);
      result = { kind: "restore", ...stats };
      restorePass = "";
      await refreshGuilds();
      flash(`Restored ${plural(stats.messages, "message")}`, "success");
    } catch (err) {
      // The sealed envelope cannot tell a wrong key from damaged bytes, and
      // says so in the vocabulary of the thing it normally protects: "wrong
      // passphrase or corrupted keystore". The keystore is the account. The
      // person standing here typed a passphrase for a FILE, and telling them
      // their keystore is corrupted is both wrong and frightening.
      const raw = String(err?.message ?? err ?? "");
      if (/not a sealed envelope/i.test(raw)) flash("That doesn't look like a Concord backup file.");
      else if (/wrong passphrase/i.test(raw))
        flash("That passphrase didn't open this file — or the file is damaged. From here the two look the same.");
      else flash(err);
    } finally {
      busy = "";
      e.target.value = "";
    }
  }
</script>

<RailShell title="Backup &amp; restore" {onClose} wide>
  <p class="intro muted">
    Your history lives only on the devices of the people in the conversation —
    there is no copy anywhere else. A backup is the copy you keep.
  </p>

  <section>
    <h3>Make a backup</h3>
    <p class="muted small">
      Every guild and direct message on this device, sealed with a passphrase of
      its own. It does <b>not</b> contain your identity — that is recovered with
      your recovery phrase — so restoring it puts your history back on a device
      you have already signed in to.
    </p>
    <label class="fld">
      <span>Passphrase for the file</span>
      <input type="password" bind:value={pass} placeholder="At least 8 characters" autocomplete="new-password" />
    </label>
    <label class="fld">
      <span>Repeat it</span>
      <input type="password" bind:value={confirmPass} autocomplete="new-password" />
    </label>
    {#if pass && pass.length < 8}
      <p class="warn small">Use at least 8 characters — this is all that protects the file.</p>
    {:else if confirmPass && pass !== confirmPass}
      <p class="warn small">The two passphrases do not match.</p>
    {/if}
    <label class="check">
      <input type="checkbox" bind:checked={withAttachments} />
      <span>
        Include images and files
        <span class="sub">Much larger, and only the ones still cached on this device — older
          attachments are evicted as space is needed, so this cannot promise every file.</span>
      </span>
    </label>
    <button class="primary" disabled={!canBackUp || busy} onclick={backUp}>
      {busy === "backup" ? "Preparing…" : "Download backup"}
    </button>
    <p class="warn small">
      <Icon name="lock" size={12} /> There is no way to recover this file's passphrase. Lose it and
      the backup is unreadable, by anyone, including you.
    </p>
  </section>

  <section>
    <h3>Restore a backup</h3>
    <p class="muted small">
      Adds anything missing. Nothing already here is overwritten or removed, so
      restoring an old backup cannot lose newer messages, and restoring the same
      file twice does nothing the second time.
    </p>
    <label class="fld">
      <span>The archive's passphrase</span>
      <input type="password" bind:value={restorePass} autocomplete="current-password" />
    </label>
    <input
      bind:this={fileInput}
      type="file"
      accept=".archive,application/octet-stream"
      style="display:none"
      onchange={restore}
    />
    <button disabled={!restorePass || busy} onclick={() => fileInput?.click()}>
      {busy === "restore" ? "Restoring…" : "Choose a backup file…"}
    </button>
    <p class="muted small">
      History is only restored into guilds this device is still a member of —
      rejoin first, then restore. Your Notes are the exception: there is nobody
      to rejoin, so they come back on their own.
    </p>
  </section>

  {#if result}
    <div class="result">
      {#if result.kind === "backup"}
        Backed up {plural(result.messages, "message")} across {plural(result.guilds, "guild")}{result.attachments
          ? `, with ${plural(result.attachments, "attachment")}`
          : ""}.
      {:else}
        Restored {plural(result.messages, "message")}{result.saved
          ? `, ${plural(result.saved, "bookmark")}`
          : ""}. {result.skipped} already present or belonging to a guild this device is not in.
      {/if}
    </div>
  {/if}
</RailShell>

<style>
  .intro {
    font-size: var(--fs-ui);
    line-height: 1.5;
    margin: 0 0 16px;
  }
  section {
    padding: 14px 0;
    border-top: 1px solid var(--border);
  }
  h3 {
    margin: 0 0 6px;
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .small {
    font-size: var(--fs-tiny);
    line-height: 1.55;
  }
  .muted {
    color: var(--text-muted);
  }
  .warn {
    color: var(--warn, var(--danger));
    margin: 6px 0 0;
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    margin: 10px 0;
  }
  .fld span {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .check {
    display: flex;
    align-items: flex-start;
    gap: 9px;
    margin: 12px 0;
    font-size: var(--fs-tiny);
  }
  /* The base input rule is width:100% with field padding, meant for text
     boxes; left alone it stretches the tick away from its own label. */
  .check input[type="checkbox"] {
    width: auto;
    flex: none;
    padding: 0;
    margin: 1px 0 0;
  }
  .check .sub {
    display: block;
    color: var(--text-muted);
    margin-top: 2px;
  }
  .result {
    margin-top: 14px;
    padding: 10px 12px;
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    font-size: var(--fs-tiny);
  }
</style>
