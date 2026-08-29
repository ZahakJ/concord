<script>
  // Reporting a message, in an app with nobody to report it to.
  //
  // Every chat app has this dialog and in every other one it means "send this
  // to our moderation team". Concord has no moderation team, because it has no
  // server: the message you are looking at travelled from that person's device
  // to yours and touched nothing in between. There is no copy of it we could
  // fetch, no account we could suspend, and no queue this could join.
  //
  // Pretending otherwise would be the worst version of this feature — a button
  // that files a complaint into nothing and leaves someone waiting for a reply
  // that is never coming. So the dialog says what is true, and then does the
  // three things that are actually within reach on this device: stop showing
  // this message, stop showing this person, and write down what happened in a
  // form you can hand to someone who *can* act.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, flash, blockUser, nameFor, hideMessage } from "../lib/state.svelte.js";
  import { saveText } from "../lib/savefile.js";
  import { buildReport, reportFilename } from "../lib/report.js";
  import { haptic } from "../lib/touch.js";

  let { message, onClose } = $props();

  const who = $derived(message?.senderName || nameFor(message?.sender) || "this member");
  let busy = $state(false);

  function hide() {
    haptic("light");
    hideMessage(message.id);
    flash("Message hidden on this device", "success");
    onClose?.();
  }

  async function block() {
    busy = true;
    try {
      await blockUser(message.sender, who);
      onClose?.();
    } catch (err) {
      flash(err);
    }
    busy = false;
  }

  // The evidence file. Everything here is already on this device; the export
  // just puts it in one place with the timestamps and identifiers intact, so
  // it means something to whoever reads it next. The sender fingerprint is the
  // safety number — the only identifier in Concord that is actually pinned to
  // a person's keys, and therefore the only one worth writing down.
  async function exportEvidence() {
    const now = new Date();
    const record = buildReport({
      message,
      reporter: S.identity.fingerprint,
      guildId: S.activeGuildId,
      guildName: S.guilds.find((g) => g.id === S.activeGuildId)?.name || "",
      now,
    });
    const how = await saveText(reportFilename(now), JSON.stringify(record, null, 2));
    if (how === "file") flash("Evidence saved to your downloads", "success");
    else if (how === "clipboard") flash("Evidence copied — paste it somewhere safe", "success");
    else flash("Couldn't write the evidence file");
  }
</script>

<Modal title="Report this message" {onClose}>
  <p class="lede">
    Nothing you do here leaves this device. Concord has no server holding your
    conversations and no moderation team reading them, so there is no report to
    file and nobody to receive it — this message reached you directly from
    {who}, encrypted, and no copy exists anywhere else.
  </p>
  <p>Here is what you can actually do about it.</p>

  <div class="opts">
    <button class="opt" onclick={hide} disabled={busy}>
      <span class="oi"><Icon name="eyeOff" size={17} /></span>
      <span class="ot">
        <strong>Hide this message</strong>
        <small>Stops it rendering for you. Reversible, and it stays in the record.</small>
      </span>
    </button>

    <button class="opt" onclick={block} disabled={busy}>
      <span class="oi"><Icon name="close" size={17} /></span>
      <span class="ot">
        <strong>Block {who}</strong>
        <small>
          Hides everything they have said and will say, and stops them starting
          a DM or adding you anywhere. Unblocking brings it all back.
        </small>
      </span>
    </button>

    <button class="opt" onclick={exportEvidence} disabled={busy}>
      <span class="oi"><Icon name="download" size={17} /></span>
      <span class="ot">
        <strong>Export evidence</strong>
        <small>
          Writes the message, their safety number and the timestamps to a file
          you keep — for handing to someone who can act, if it comes to that.
        </small>
      </span>
    </button>
  </div>

  <p class="foot">
    If this guild has admins, they can delete the message and remove the member
    for everyone — ask them. And if the problem is Concord itself rather than a
    person, that <em>is</em> something the maintainers can fix: see SECURITY.md
    in the repository for how to reach them privately.
  </p>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Close</button>
  </div>
</Modal>

<style>
  p {
    font-size: var(--fs-ui);
    color: var(--text-muted);
    line-height: 1.55;
    margin: 0 0 10px;
  }
  .lede {
    color: var(--text);
  }
  .foot {
    font-size: var(--fs-compact);
    margin: 14px 0 0;
  }
  .opts {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    margin: 14px 0 0;
  }
  .opt {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-3);
    width: 100%;
    text-align: start;
    padding: var(--sp-3);
    min-height: var(--tap-min);
    background: var(--bg-2);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .opt:hover:not(:disabled) {
    background: var(--bg-3);
    border-color: var(--accent);
  }
  .opt:disabled {
    opacity: 0.55;
  }
  .oi {
    display: grid;
    place-items: center;
    flex-shrink: 0;
    width: 30px;
    height: 30px;
    border-radius: 50%;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .ot {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }
  .ot strong {
    font-size: var(--fs-ui);
    color: var(--text);
  }
  .ot small {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    line-height: 1.45;
  }
</style>
