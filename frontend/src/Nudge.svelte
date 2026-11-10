<script>
  // The one nudge: a single quiet row asking for one thing — write down the
  // recovery phrase, finish setting up a guild, turn on notifications. One
  // icon, one sentence, one action, and a way to say "later".
  //
  //   <Nudge icon="key" action="Show me" onAction={…} onDismiss={…} dismissLabel="Later">
  //     <strong>Write down your recovery phrase.</strong> It is the only way back in.
  //   </Nudge>
  //
  // Only one is ever on screen (the caller's queue decides which, by priority:
  // connection, then security, then setup, then tips), and it never covers the
  // feed or a dialog. In the flow it is a 40px row on --bg-3; `floating` is for
  // the rare caller that has to lay it over the app, and gives it the lifted
  // surface and the float shadow instead. `tone="warn"` tints the icon for a
  // nudge whose consequence is losing something (the recovery phrase), and
  // nothing else changes: it is still a nudge, not an alarm.
  import Icon from "./Icon.svelte";

  let {
    icon = "info",
    action = "",
    onAction,
    onDismiss,
    dismissLabel = "Dismiss",
    tone = "",
    floating = false,
    children,
  } = $props();
</script>

<div class="nudge" class:floating class:warn={tone === "warn"} role="note">
  <span class="ic" aria-hidden="true"><Icon name={icon} size={16} /></span>
  <span class="txt">{@render children?.()}</span>
  {#if action}
    <button type="button" class="primary sm act" onclick={onAction}>{action}</button>
  {/if}
  {#if onDismiss}
    <button type="button" class="iconbtn x" aria-label={dismissLabel} title={dismissLabel} onclick={onDismiss}>
      <Icon name="close" size={14} />
    </button>
  {/if}
</div>

<style>
  .nudge {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 40px;
    padding: var(--sp-1) var(--sp-1) var(--sp-1) var(--sp-3);
    border-radius: var(--radius-md);
    background: var(--bg-3);
    color: var(--text);
    font-size: var(--fs-compact);
  }
  .nudge.floating {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    box-shadow: var(--shadow-pop);
  }
  .ic {
    display: grid;
    place-items: center;
    flex: none;
    color: var(--text-muted);
  }
  .warn .ic {
    color: var(--warn-text);
  }
  .txt {
    flex: 1;
    min-width: 0;
    line-height: 1.35;
  }
  .act {
    flex: none;
  }
  .x {
    flex: none;
    width: 28px;
    height: 28px;
  }
</style>
