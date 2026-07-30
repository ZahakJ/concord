<script>
  // Define guild roles: name, color, and a set of permissions. Requires the
  // "Manage roles" permission (the backend re-checks and enforces rank rules).
  import Modal from "./Modal.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import Icon from "../Icon.svelte";
  import { S, refreshRightPanel, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { PERM_LIST, has } from "../lib/perms.js";

  let { onClose } = $props();

  let roles = $state([]);
  let loading = $state(true);
  let editing = $state(null); // { id, name, color, perms, position }
  let busy = $state(false);

  async function load() {
    loading = true;
    try {
      roles = (await api.roles(S.activeGuildId)) || [];
    } catch (err) {
      flash(err);
    } finally {
      loading = false;
    }
  }

  function newRole() {
    const topPos = roles.reduce((m, r) => Math.max(m, r.position), 0);
    editing = { id: "", name: "", color: "#5865f2", perms: 0, position: topPos + 1 };
  }
  function edit(r) {
    editing = { ...r };
  }
  function togglePerm(bit) {
    editing.perms = has(editing.perms, bit) ? editing.perms & ~bit : editing.perms | bit;
  }

  async function save() {
    if (!editing.name.trim() || busy) return;
    busy = true;
    try {
      await api.upsertRole(
        S.activeGuildId,
        editing.id,
        editing.name.trim(),
        editing.color,
        editing.perms,
        editing.position,
      );
      await load();
      await refreshRightPanel();
      editing = null;
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  // Deleting a role strips its permissions from everyone who holds it, and
  // there is no undo. It used to fire on the first tap of a control that was
  // invisible on a phone (see .del below) — the two halves of one accident.
  // Rendered locally rather than through S.modal, which would REPLACE this
  // panel and drop the settings back-stack on cancel.
  let confirmDel = $state(null);

  async function del(r) {
    confirmDel = null;
    busy = true;
    try {
      await api.deleteRole(S.activeGuildId, r.id);
      await load();
      await refreshRightPanel();
      if (editing?.id === r.id) editing = null;
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  load();
</script>

<Modal title="Roles" {onClose}>
  {#if editing}
    <div class="editor">
      <label class="field">
        <span class="muted">Role name</span>
        <div class="name-row">
          <input type="color" bind:value={editing.color} title="Role color" />
          <input bind:value={editing.name} maxlength="32" placeholder="e.g. Moderator" />
        </div>
      </label>
      <div class="field">
        <span class="muted">Permissions</span>
        <div class="perms">
          {#each PERM_LIST as p (p.bit)}
            <button type="button" class="perm" class:on={has(editing.perms, p.bit)} onclick={() => togglePerm(p.bit)}>
              <span class="check">{has(editing.perms, p.bit) ? "✓" : ""}</span>
              <span class="perm-text">
                <strong>{p.label}</strong>
                <span class="muted tiny">{p.hint}</span>
              </span>
            </button>
          {/each}
        </div>
      </div>
      <div class="actions">
        <button class="ghost" onclick={() => (editing = null)}>Back</button>
        <button onclick={save} disabled={!editing.name.trim() || busy}>Save role</button>
      </div>
    </div>
  {:else}
    {#if loading}
      <p class="muted empty">Loading…</p>
    {:else}
      <div class="list">
        {#each roles as r (r.id)}
          <div class="role-row">
            <button class="role" onclick={() => edit(r)}>
              <span class="swatch" style="background:{r.color || 'var(--text-faint)'}"></span>
              <span class="role-name" style={r.color ? `color:${r.color}` : ""}>{r.name}</span>
            </button>
            <button class="del" title="Delete role" aria-label="Delete role" onclick={() => (confirmDel = r)}>
              <Icon name="trash" size={13} />
            </button>
          </div>
        {:else}
          <p class="muted empty">No roles yet. Create one to grant specific permissions.</p>
        {/each}
      </div>
      <button class="new" onclick={newRole}><Icon name="plus" size={13} /> New role</button>
    {/if}
  {/if}
</Modal>

{#if confirmDel}
  <ConfirmDialog
    title="Delete role?"
    body="“{confirmDel.name}” is removed from everyone who has it, along with the permissions it granted. This can't be undone."
    confirmLabel="Delete role"
    onConfirm={() => del(confirmDel)}
    onClose={() => (confirmDel = null)}
  />
{/if}

<style>
  .empty {
    font-size: var(--fs-ui);
    padding: 6px 2px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 320px;
    overflow-y: auto;
  }
  /* A scroller nested inside a sheet that already scrolls is a thumb trap: a
     flick starting inside it moves the list, the same flick 4px outside moves
     the sheet. The sheet's own scroll is enough. */
  @media (pointer: coarse), (max-width: 768px) {
    .list {
      max-height: none;
      overflow-y: visible;
    }
  }
  .role-row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
  }
  .role-row:hover,
  .role-row:active {
    background: var(--bg-3);
  }
  .role {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 9px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 8px;
  }
  .swatch {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .role-name {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .del {
    background: transparent;
    color: var(--danger-text);
    padding: 6px 8px;
  }
  /* Hidden-until-hover is a MOUSE affordance. Scoped to (pointer: fine) it
     stays out of the way on desktop; anywhere else it is permanently visible,
     because opacity:0 does not remove hit-testing — the old rule left a live,
     unlabelled delete target floating at the right edge of every role row on a
     phone, invisible to the person tapping it. */
  @media (pointer: fine) {
    .del {
      opacity: 0;
    }
    .role-row:hover .del,
    .role-row:focus-within .del {
      opacity: 1;
    }
  }
  .del:active {
    background: color-mix(in srgb, var(--danger) 18%, transparent);
    border-radius: var(--radius-sm);
  }
  .new {
    margin-top: 10px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-ui);
    align-self: flex-start;
  }
  .editor {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    text-align: left;
  }
  .name-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .name-row input[type="color"] {
    width: 38px;
    height: 34px;
    padding: 2px;
    flex-shrink: 0;
  }
  .name-row input:not([type="color"]) {
    flex: 1;
  }
  .perms {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .perm {
    display: flex;
    align-items: center;
    gap: 10px;
    background: var(--bg-3);
    border: 1px solid transparent;
    color: var(--text);
    text-align: left;
    padding: 8px 10px;
    border-radius: var(--radius-sm);
  }
  .perm.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .check {
    width: 16px;
    color: var(--accent-hover);
    font-weight: 700;
  }
  .perm-text {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .tiny {
    font-size: var(--fs-small);
  }
</style>
