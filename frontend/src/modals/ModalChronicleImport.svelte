<script>
  // The import wizard: turn a folder of exported channel JSON into this guild's
  // archive, and say what that will cost before it costs anything.
  //
  // Four steps, and the shape of them follows the backend's. Pick names the
  // folder. The bill is the scan — one pass over the directory, no writes, no
  // network — laid out to be READ: totals, then every channel with its own
  // count and span, then the size distribution of the media. Policy is where
  // that gets narrowed, with a live estimate pinned under it so the effect of
  // every change is visible in the same glance as the change. Run starts the
  // job and follows it.
  //
  // The wizard is closable while the import runs. It is a real job on the
  // backend with an id, and reopening this dialog picks the job back up rather
  // than offering to start a second one.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EmptyState from "../EmptyState.svelte";
  import InfoDot from "./InfoDot.svelte";
  import { S, activeGuild, flash, refreshGuilds, refreshChronicle } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { plural } from "../lib/plural.js";
  import { tooltip } from "../lib/tooltip.js";
  import {
    TIERS,
    CAPS,
    DEFAULT_UI,
    policyFor,
    estimateLine,
    leftBehind,
    histogramBars,
    channelRows,
    sortRows,
    rangeLabel,
    dateInput,
    nanoOfDate,
    fmtBytes,
    fmtCount,
    phaseLabel,
    progressPct,
    resultLines,
  } from "../lib/chronicle.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());

  let step = $state("pick"); // pick | bill | policy | run
  let dir = $state("");
  let canBrowse = $state(false);
  let scanning = $state(false);
  let scanError = $state("");
  let stats = $state(null);

  // The policy, as the wizard holds it. policyFor() turns this into the object
  // the two RPCs take; nothing else in this file constructs one.
  let ui = $state({ ...DEFAULT_UI, exclude: [] });
  let est = $state(null);
  let estimating = $state(false);

  let sortKey = $state("messages");
  let sortDir = $state(-1);

  let starting = $state(false);

  // ---- picking a folder ----

  // A native folder chooser exists on the desktop shell and nowhere else. Where
  // there is one, offer it first; where there isn't, the typed path is the only
  // road and the help text below says where to point it.
  $effect(() => {
    api
      .canChooseFolder()
      .then((v) => (canBrowse = !!v))
      .catch(() => (canBrowse = false));
  });

  // Pick the running job back up. Someone who closed the wizard mid-import and
  // reopened it must land on the progress bar, not on a second Start button —
  // the backend runs one import at a time and would refuse the second anyway,
  // with an error that reads like a bug.
  $effect(() => {
    api
      .chronicleImportStatus("")
      .then((st) => {
        if (st?.running && st.guildId === S.activeGuildId) {
          S.chronImport = st;
          dir = st.dir || dir;
          step = "run";
        }
      })
      .catch(() => {
        /* no import has ever run here */
      });
  });

  async function browse() {
    try {
      const chosen = await api.chooseFolder("Choose the exported chat folder");
      if (chosen) {
        dir = chosen;
        scanError = "";
      }
    } catch (err) {
      flash(err);
    }
  }

  async function scan() {
    const path = dir.trim();
    if (!path || scanning) return;
    scanning = true;
    scanError = "";
    try {
      const res = await api.scanChatExport(path);
      stats = res;
      ui = { ...DEFAULT_UI, exclude: [], source: res?.guild || "" };
      est = null;
      step = "bill";
    } catch (err) {
      // Inline, beside the field it is about. A toast for this would vanish
      // while the reader was still looking at the path they typed.
      scanError = String(err?.message || err).replace(/^app:\s*/, "");
    } finally {
      scanning = false;
    }
  }

  // ---- the bill ----

  const rows = $derived(sortRows(channelRows(stats, ui.exclude), sortKey, sortDir));
  const bars = $derived(histogramBars(stats));
  const includedCount = $derived(rows.filter((r) => r.included).length);
  const notExported = $derived(stats ? stats.remoteAttachmentBytes || 0 : 0);

  function toggleSort(key) {
    if (sortKey === key) sortDir = -sortDir;
    else {
      sortKey = key;
      // Names read best A→Z; every count reads best biggest-first.
      sortDir = key === "name" ? 1 : -1;
    }
  }

  function toggleChannel(id) {
    ui.exclude = ui.exclude.includes(id)
      ? ui.exclude.filter((x) => x !== id)
      : [...ui.exclude, id];
  }

  const chanIcon = (type) =>
    /voice/i.test(type || "") ? "speaker" : /forum|thread/i.test(type || "") ? "forum" : "hash";

  // ---- the live estimate ----

  // Debounced, because this fires on every keystroke in a date field and every
  // click of a radio. The call itself is pure arithmetic over the cached scan —
  // no file is touched — so 150ms is about hiding the flicker, not about load.
  let estTimer = 0;
  $effect(() => {
    if (step !== "policy" || !stats) return;
    // Read everything the policy depends on, so the effect re-runs when any of
    // it changes.
    const policy = policyFor($state.snapshot(ui));
    const path = dir;
    clearTimeout(estTimer);
    estTimer = setTimeout(async () => {
      estimating = true;
      try {
        est = await api.estimateChatImport(path, policy);
      } catch (err) {
        est = null;
        flash(err);
      } finally {
        estimating = false;
      }
    }, 150);
    return () => clearTimeout(estTimer);
  });

  const line = $derived(estimateLine(est));
  const behind = $derived(leftBehind(est, stats));

  // ---- running it ----

  async function start() {
    if (starting) return;
    starting = true;
    try {
      S.chronImportDone = null;
      S.chronImport = null;
      await api.importChatExport(S.activeGuildId, dir.trim(), policyFor($state.snapshot(ui)));
      step = "run";
    } catch (err) {
      flash(err);
    } finally {
      starting = false;
    }
  }

  const prog = $derived(S.chronImport);
  const done = $derived(S.chronImportDone);
  const pct = $derived(progressPct(prog));

  async function finish() {
    await refreshGuilds();
    refreshChronicle(S.activeGuildId);
    onClose();
  }
</script>

<Modal title="Import a chat archive" wide {onClose}>
  {#if !g?.isOwner}
    <!-- The backend refuses this for anyone but the owner, twice over. Saying so
         here is kinder than letting somebody fill in three screens first. -->
    <EmptyState
      icon="lock"
      headline="Only the guild's owner can import an archive"
      sub="The archive is signed by the owner's key — that signature is what lets every other member trust it."
    />
    <div class="actions"><button class="ghost" onclick={onClose}>Close</button></div>
  {:else if step === "pick"}
    <p class="lead">
      Point this at a folder of exported channel JSON. Concord reads it once, tells you
      exactly how big it is, and lets you decide what to keep — nothing is written and
      nothing is fetched until you say so.
    </p>

    <label class="fld" for="chron-dir">Folder</label>
    <div class="pathrow">
      <!-- svelte-ignore a11y_autofocus -->
      <input
        id="chron-dir"
        type="text"
        placeholder="/home/you/exports/old-community"
        bind:value={dir}
        autofocus={!S.isMobile}
        onkeydown={(e) => e.key === "Enter" && scan()}
      />
      {#if canBrowse}
        <button class="ghost browse" onclick={browse} use:tooltip aria-label="Choose a folder">
          <Icon name="folder" size={14} /> Browse…
        </button>
      {/if}
    </div>
    <p class="hint">
      The folder should contain one JSON file per channel — several per channel is fine,
      big channels are usually split by year. An <code>assets</code> folder beside them,
      if the export made one, carries the pictures.
    </p>
    {#if scanError}
      <p class="err" role="alert"><Icon name="alert" size={13} /> {scanError}</p>
    {/if}

    <div class="actions">
      <button class="ghost" onclick={onClose}>Cancel</button>
      <button disabled={!dir.trim() || scanning} onclick={scan}>
        {#if scanning}<span class="spin"></span> Reading…{:else}Read the folder{/if}
      </button>
    </div>
  {:else if step === "bill"}
    {#if !stats?.channels?.length}
      <EmptyState
        icon="folder"
        headline="No channels in that folder"
        sub="Nothing there parsed as a channel export. Check that you pointed at the folder holding the JSON files rather than at one of them."
      />
      <div class="actions">
        <button class="ghost" onclick={() => (step = "pick")}>Back</button>
      </div>
    {:else}
      <!-- The transparency moment. Everything the scan found, before a single
           byte has been written. -->
      <div class="totals">
        <div class="tot">
          <span class="tv">{fmtCount(stats.messages)}</span>
          <span class="tl">messages</span>
        </div>
        <div class="tot">
          <span class="tv">{rangeLabel(stats.firstNano, stats.lastNano) || "—"}</span>
          <span class="tl">covering</span>
        </div>
        <div class="tot">
          <span class="tv">{fmtCount(stats.authors)}</span>
          <span class="tl">{stats.authors === 1 ? "author" : "authors"}</span>
        </div>
        <div class="tot">
          <span class="tv">{fmtBytes(stats.localAttachmentBytes)}</span>
          <span class="tl">media in the folder</span>
        </div>
        <div class="tot">
          <span class="tv">{fmtBytes(notExported)}</span>
          <span class="tl">
            named but not exported
            <InfoDot
              label="Why are some files missing?"
              text="The export linked to these files instead of saving them. Concord never fetches from the network, so each one becomes a line naming the file and its size."
            />
          </span>
        </div>
      </div>

      {#if stats.notices || stats.malformed || stats.fileErrors?.length}
        <p class="note">
          {#if stats.notices}{fmtCount(stats.notices)} join/pin notices will be left out.{/if}
          {#if stats.malformed}
            {fmtCount(stats.malformed)} entries could not be read.{/if}
          {#if stats.fileErrors?.length}
            {plural(stats.fileErrors.length, "file")} could not be opened at all
            ({stats.fileErrors
              .slice(0, 3)
              .map((f) => f.file)
              .join(", ")}{stats.fileErrors.length > 3 ? "…" : ""}).
          {/if}
        </p>
      {/if}

      <div class="sec-head">
        <h4>Channels</h4>
        <span class="muted small">{includedCount} of {plural(rows.length, "channel")} selected</span>
      </div>
      <div class="tbl">
        <div class="thead">
          <span class="c-inc"><span class="sr-only">Include</span></span>
          <button class="c-name th" onclick={() => toggleSort("name")}>
            Channel {#if sortKey === "name"}<span class="caret">{sortDir < 0 ? "▾" : "▴"}</span>{/if}
          </button>
          <button class="c-num th" onclick={() => toggleSort("messages")}>
            Messages {#if sortKey === "messages"}<span class="caret">{sortDir < 0 ? "▾" : "▴"}</span
              >{/if}
          </button>
          <span class="c-range th">Span</span>
          <button
            class="c-num th"
           
            onclick={() => toggleSort("attachmentBytes")}
          >
            Media {#if sortKey === "attachmentBytes"}<span class="caret"
                >{sortDir < 0 ? "▾" : "▴"}</span
              >{/if}
          </button>
        </div>
        {#each rows as r (r.id)}
          <label class="trow" class:off={!r.included}>
            <span class="c-inc">
              <input
                type="checkbox"
                checked={r.included}
                onchange={() => toggleChannel(r.id)}
                aria-label="Include {r.name}"
              />
            </span>
            <span class="c-name">
              <Icon name={chanIcon(r.type)} size={12} />
              <span class="cn">{r.name}</span>
            </span>
            <span class="c-num">{fmtCount(r.messages)}</span>
            <span class="c-range">{rangeLabel(r.firstNano, r.lastNano) || "—"}</span>
            <span class="c-num">{r.attachmentBytes ? fmtBytes(r.attachmentBytes) : "—"}</span>
          </label>
        {/each}
      </div>

      {#if stats.attachments > 0}
        <div class="sec-head">
          <h4>Attachment sizes</h4>
          <span class="muted small">{plural(stats.attachments, "file")}</span>
        </div>
        <div class="hist">
          {#each bars as b (b.label)}
            <div class="hrow">
              <span class="hl">{b.label}</span>
              <!-- A bucket with nothing in it draws nothing. min-width on the
                   fill (which keeps a 0.2% bar visible) would otherwise give
                   every empty size class a permanent 2px stub. -->
              <span class="hbar">
                {#if b.count > 0}<span class="hfill" style="width:{b.pct}%"></span>{/if}
              </span>
              <span class="hn">{fmtCount(b.count)}</span>
            </div>
          {/each}
        </div>
        <p class="hint">
          The size limit on the next screen cuts across these buckets — that is what makes
          "images only, under 5 MB" a different number from "everything".
        </p>
      {/if}

      <div class="actions">
        <button class="ghost" onclick={() => (step = "pick")}>Back</button>
        <button disabled={!includedCount} onclick={() => (step = "policy")}>
          Choose what to keep
        </button>
      </div>
    {/if}
  {:else if step === "policy"}
    <div class="pol">
      <div class="sec-head"><h4>Dates</h4></div>
      <div class="dates">
        <label class="dfld">
          <span>From</span>
          <input
            type="date"
            value={dateInput(ui.fromNano)}
            onchange={(e) => (ui.fromNano = nanoOfDate(e.currentTarget.value))}
          />
        </label>
        <label class="dfld">
          <span>To</span>
          <input
            type="date"
            value={dateInput(ui.toNano ? ui.toNano - 1 : 0)}
            onchange={(e) => (ui.toNano = nanoOfDate(e.currentTarget.value, true))}
          />
        </label>
        {#if ui.fromNano || ui.toNano}
          <button class="ghost tiny" onclick={() => ((ui.fromNano = 0), (ui.toNano = 0))}>
            Clear
          </button>
        {/if}
      </div>
      <p class="hint">Leave both empty to take the whole history the folder holds.</p>

      <div class="sec-head"><h4>Attachments</h4></div>
      {#each TIERS as t (t.id)}
        <label class="radio">
          <input type="radio" name="tier" value={t.id} bind:group={ui.tier} />
          <span class="rtext">
            <span class="rt">{t.label}</span>
            <span class="rs">{t.hint}</span>
          </span>
        </label>
      {/each}
      {#if ui.tier !== "none"}
        <label class="capfld">
          <span>Skip anything larger than</span>
          <select bind:value={ui.cap}>
            {#each CAPS as c (c.bytes)}
              <option value={c.bytes}>{c.label}</option>
            {/each}
          </select>
        </label>
      {/if}

      <div class="sec-head"><h4>Extras</h4></div>
      <label class="chk">
        <input type="checkbox" bind:checked={ui.reactions} />
        <span class="rtext">
          <span class="rt">Keep reactions</span>
          <span class="rs">Shown as counts — archived messages can't be reacted to.</span>
        </span>
      </label>
      <label class="chk">
        <input type="checkbox" bind:checked={ui.emoji} />
        <span class="rtext">
          <span class="rt">Import custom emoji</span>
          <span class="rs">
            {#if stats?.emoji?.length}
              {plural(stats.emoji.filter((e) => e.local && e.sanitized).length, "emoji")} can come
              across; the rest were links rather than files.
            {:else}
              This export brought none.
            {/if}
          </span>
        </span>
      </label>

      <div class="sec-head"><h4>Label</h4></div>
      <input
        class="lbl"
        type="text"
        placeholder="Where this history came from"
        bind:value={ui.source}
        aria-label="Archive label"
      />
      <p class="hint">Every member sees this beside the archive. The export's own name is filled in.</p>
    </div>

    <!-- The estimate, pinned. Fixed height on purpose: it recomputes on every
         change, and a line that grows from one row to two would shove the
         buttons under the reader's cursor mid-click. -->
    <div class="est" class:stale={estimating} aria-live="polite">
      <span class="est-main">{line || " "}</span>
      <span class="est-sub">{behind.text || " "}</span>
    </div>

    <div class="actions">
      <button class="ghost" onclick={() => (step = "bill")}>Back</button>
      <button disabled={starting || !est?.messages} onclick={start}>
        {#if starting}<span class="spin"></span> Starting…{:else}Import{/if}
      </button>
    </div>
  {:else}
    <!-- Run. -->
    {#if prog?.phase === "failed" || done?.error}
      <EmptyState
        icon="alert"
        headline="The import stopped"
        sub={done?.error || "Nothing was attached to this guild."}
      />
      <div class="actions">
        <button class="ghost" onclick={() => (step = "pick")}>Try again</button>
        <button onclick={onClose}>Close</button>
      </div>
    {:else if prog?.phase === "done" && done?.result}
      <div class="sec-head"><h4>Imported</h4></div>
      <ul class="reslist">
        {#each resultLines(done.result) as l (l)}
          <li><Icon name="check" size={12} /> {l}</li>
        {/each}
      </ul>
      <p class="hint">
        Scroll past the top of any of those channels and the archive continues below the
        divider. Other members get the index automatically; the pages themselves come from
        whoever has them, only when somebody reads that far.
      </p>
      <div class="actions">
        <button onclick={finish}>Done</button>
      </div>
    {:else}
      <p class="lead">
        {phaseLabel(prog?.phase)}{prog?.channel ? ` · #${prog.channel}` : ""}
      </p>
      <div
        class="bar"
        class:indeterminate={pct < 0}
        role="progressbar"
        aria-valuemin="0"
        aria-valuemax="100"
        aria-valuenow={pct < 0 ? undefined : Math.round(pct)}
        aria-label="Import progress"
      >
        <span class="fill" style={pct < 0 ? "" : `width:${pct}%`}></span>
      </div>
      <p class="muted small counts">
        {#if prog?.total > 0}
          {fmtCount(prog.done)} of {fmtCount(prog.total)}
        {:else}
          Working…
        {/if}
      </p>
      <p class="hint">
        This runs in the background. You can close this and keep using Concord — you'll be
        told when it lands.
      </p>
      <div class="actions">
        <button class="ghost" onclick={onClose}>Close</button>
      </div>
    {/if}
  {/if}
</Modal>

<style>
  .lead {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .hint {
    margin: 0;
    font-size: var(--fs-small);
    line-height: 1.5;
    color: var(--text-faint);
  }
  .hint code {
    font-family: var(--mono-font);
    background: var(--bg-2);
    border-radius: var(--radius-sm);
    padding: 0 4px;
  }
  .note {
    margin: 0;
    font-size: var(--fs-small);
    line-height: 1.5;
    color: var(--warn-text);
  }
  .err {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    margin: 0;
    font-size: var(--fs-small);
    color: var(--danger-text);
  }
  .fld {
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .pathrow {
    display: flex;
    gap: var(--sp-2);
  }
  .pathrow input {
    flex: 1;
    min-width: 0;
    font-family: var(--mono-font);
    font-size: var(--fs-compact);
  }
  .browse {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .sec-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--sp-2);
    margin-top: var(--sp-2);
  }
  .sec-head h4 {
    margin: 0;
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .small {
    font-size: var(--fs-small);
  }

  /* ---- the bill ---- */
  .totals {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: var(--sp-2);
  }
  .tot {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-2);
    min-width: 0;
  }
  .tv {
    font-size: var(--fs-title);
    font-weight: 700;
    line-height: 1.15;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tl {
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .tbl {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    /* Clips the rounded corner off the first and last row's fill. */
    overflow: hidden;
    /* …and `overflow: hidden` is also what makes this shrinkable: it zeroes the
       automatic minimum size, so as a child of the dialog's own column flexbox
       the table collapsed to a 8px sliver the moment the content was taller
       than a phone sheet. The dialog is the scroller; nothing inside it should
       be squeezed to make the content fit. */
    flex: none;
    background: var(--bg-2);
  }
  .thead,
  .trow {
    display: grid;
    /* Tight on purpose: the dialog is 460px and the channel NAME is the column
       a reader scans, so every other column is cut to what its widest value
       actually needs rather than to a round number. */
    grid-template-columns: 22px minmax(0, 1fr) 68px 96px 58px;
    align-items: center;
    gap: var(--sp-1);
    padding: 6px var(--sp-2);
  }
  .thead {
    background: var(--bg-3);
    border-bottom: 1px solid var(--border);
  }
  .th {
    white-space: nowrap;
    background: transparent;
    border: none;
    padding: 0;
    color: var(--text-faint);
    font-size: var(--fs-tiny);
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    text-align: left;
    cursor: pointer;
  }
  button.th:hover {
    color: var(--text);
  }
  .caret {
    font-size: var(--fs-micro);
  }
  .trow {
    font-size: var(--fs-compact);
    border-bottom: 1px solid var(--hairline, var(--border));
    cursor: pointer;
  }
  .trow:last-child {
    border-bottom: none;
  }
  @media (pointer: fine) {
    .trow:hover {
      background: var(--bg-3);
    }
  }
  .trow.off {
    opacity: 0.45;
  }
  .c-name {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    color: var(--text);
  }
  .cn {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .c-num {
    text-align: right;
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
  }
  button.c-num {
    text-align: right;
  }
  .c-range {
    color: var(--text-faint);
    font-size: var(--fs-tiny);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Under a phone's width the span and the media size drop to their own line
     rather than crushing the channel name to three letters. */
  @media (max-width: 620px) {
    .thead {
      grid-template-columns: 22px minmax(0, 1fr) 74px;
    }
    .thead .c-range,
    .thead .c-num:last-child {
      display: none;
    }
    .trow {
      grid-template-columns: 22px minmax(0, 1fr) 74px;
    }
    .trow .c-range,
    .trow .c-num:last-child {
      grid-column: 2 / -1;
      text-align: left;
    }
  }

  .hist {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .hrow {
    display: grid;
    grid-template-columns: 68px 1fr 52px;
    align-items: center;
    gap: var(--sp-2);
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .hl {
    font-variant-numeric: tabular-nums;
  }
  .hbar {
    height: 8px;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  .hfill {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: linear-gradient(90deg, var(--accent), var(--accent-hover));
    min-width: 2px;
  }
  .hn {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  /* ---- policy ---- */
  /* The policy controls scroll inside their own pane rather than scrolling the
     dialog, so the estimate below them and the Import button below that are on
     screen the whole time somebody is changing the policy. Without it the one
     line whose entire job is to react to these controls sat two screens down on
     a phone. min-height:0 is what lets a flex child be smaller than its content
     at all; the pane only shrinks when the dialog has hit its own ceiling. */
  .pol {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    /* The scrollbar would otherwise sit on top of the radio cards' right edge. */
    padding-right: 2px;
  }
  .dates {
    display: flex;
    align-items: flex-end;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .dfld {
    display: flex;
    flex-direction: column;
    gap: 3px;
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .tiny {
    padding: 4px 10px;
    font-size: var(--fs-small);
  }
  .radio,
  .chk {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: var(--sp-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-2);
    cursor: pointer;
  }
  .radio:has(input:checked) {
    border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
    background: var(--accent-soft);
  }
  .radio input,
  .chk input {
    margin-top: 2px;
    flex-shrink: 0;
  }
  .rtext {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .rt {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .rs {
    font-size: var(--fs-small);
    color: var(--text-faint);
    line-height: 1.45;
  }
  .capfld {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    font-size: var(--fs-ui);
    color: var(--text-muted);
  }
  .lbl {
    width: 100%;
  }

  /* The estimate line. Two rows, always — the second is empty when a policy
     leaves nothing behind, and reserving it is what stops the buttons moving. */
  .est {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 2px;
    flex: none;
    min-height: 52px;
    margin-top: var(--sp-2);
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--border));
    border-radius: var(--radius-md);
    background: var(--accent-soft);
    transition: opacity var(--dur-standard) ease;
  }
  .est.stale {
    opacity: 0.55;
  }
  .est-main {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .est-sub {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }

  /* ---- run ---- */
  .bar {
    height: 8px;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: linear-gradient(90deg, var(--accent), var(--accent-hover));
    transition: width 0.3s ease;
  }
  /* No total to divide by (signing, attaching): a sweep, rather than a
     percentage nobody computed. */
  .bar.indeterminate .fill {
    width: 35%;
    animation: sweep 1.2s ease-in-out infinite;
  }
  @keyframes sweep {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(300%);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bar.indeterminate .fill {
      animation: none;
      width: 100%;
      opacity: 0.5;
    }
  }
  .counts {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
  .reslist {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .reslist li {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    font-size: var(--fs-ui);
    color: var(--text-muted);
  }
  .reslist :global(svg) {
    color: var(--ok-text, var(--accent));
    flex-shrink: 0;
  }
  .spin {
    display: inline-block;
    width: 12px;
    height: 12px;
    border: 2px solid color-mix(in srgb, currentColor 35%, transparent);
    border-top-color: currentColor;
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    vertical-align: -1px;
    margin-right: var(--sp-1);
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .spin {
      animation: none;
    }
  }
</style>
