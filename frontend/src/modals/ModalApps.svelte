<script>
  // App traffic: what the other local apps (sentinel, trove, …) have pushed
  // into THIS channel over the app-bus.
  //
  // These payloads deliberately never render in the feed — they are data, not
  // conversation, and they used to spam #general. But "hidden" must not mean
  // "gone": when a connector misbehaves you need to see exactly what it sent,
  // and when it works you want proof it arrived. This is that window, and it is
  // no more than that — a debug/observability surface, not a dashboard. It
  // shows the raw payload as it came in rather than pretty-printing it into
  // something that might not match what was actually delivered.
  //
  // Scope is the channel you're in (S.appMessages, loaded and pruned alongside
  // the feed) — there is no cross-channel app history to browse here.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, clockOpts } from "../lib/state.svelte.js";
  import { parseAppMessage } from "../lib/appbus.js";

  let { channelName, onClose } = $props();

  // Newest first: when you open this you are almost always asking "what just
  // came in?", not reading forwards through the day.
  const rows = $derived(
    [...S.appMessages].reverse().map((m) => ({ m, ...parseAppMessage(m) })),
  );

  function fmtTime(iso) {
    try {
      return new Date(iso).toLocaleString([], {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        ...clockOpts(),
      });
    } catch {
      return iso || "";
    }
  }
</script>

<Modal title={`App traffic — ${channelName || "channel"}`} {onClose} wide>
  <p class="muted intro">
    <Icon name="code" size={11} />
    Machine payloads other apps posted here. They're kept out of the conversation
    on purpose — this is the only place they show up.
  </p>

  {#if rows.length === 0}
    <div class="ap-empty">
      <Icon name="code" size={18} />
      <strong>Nothing from any app</strong>
      <span>
        No app-bus payloads in the history loaded for this channel. Scroll further
        back in the conversation to load more of it.
      </span>
    </div>
  {:else}
    <div class="ap-list">
      {#each rows as r (r.m.id)}
        <div class="ap-row">
          <div class="ap-head">
            <!-- No parsed name means a sender that set kind:"app" without the
                 APPBUS header line — say so rather than invent an app. -->
            <span class="ap-app" class:unknown={!r.app}>{r.app || "unidentified app"}</span>
            {#if r.version}<span class="ap-ver">v{r.version}</span>{/if}
            <span class="ap-time">{fmtTime(r.m.sent)}</span>
          </div>
          <pre class="ap-payload">{r.payload || r.m.content}</pre>
        </div>
      {/each}
    </div>
  {/if}

  <div class="actions">
    <button class="ghost" onclick={onClose}>Close</button>
  </div>
</Modal>

<style>
  p.intro {
    margin: 0;
    font-size: 12.5px;
    display: flex;
    align-items: flex-start;
    gap: 6px;
    line-height: 1.5;
  }
  .ap-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    /* Long histories scroll inside the dialog; the Modal's own max-height
       already caps the sheet. */
    max-height: 52vh;
    overflow-y: auto;
  }
  .ap-row {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-1);
    overflow: hidden;
  }
  .ap-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 7px 10px;
    background: var(--bg-2);
    border-bottom: 1px solid var(--border);
  }
  .ap-app {
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.02em;
  }
  .ap-app.unknown {
    font-weight: 600;
    font-style: italic;
    color: var(--text-muted);
  }
  .ap-ver {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    font-weight: 700;
  }
  /* Timestamp takes the slack so it always sits at the right edge. */
  .ap-time {
    margin-left: auto;
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }
  .ap-payload {
    margin: 0;
    padding: 9px 10px;
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 11.5px;
    line-height: 1.5;
    color: var(--text-muted);
    /* Wrap rather than scroll horizontally: a one-line JSON blob is the common
       case and a sideways scrollbar per row would be unusable. */
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 180px;
    overflow-y: auto;
  }
  .ap-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
    padding: 26px 16px;
    text-align: center;
    color: var(--text-muted);
  }
  .ap-empty strong {
    font-size: 13.5px;
    color: var(--text);
  }
  .ap-empty span {
    font-size: 12px;
    max-width: 34ch;
    line-height: 1.5;
  }
</style>
