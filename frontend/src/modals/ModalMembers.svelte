<script>
  // People → Members. The only way to act on a person used to be finding their
  // avatar in the right-hand sidebar. With three members that is fine; with
  // three hundred there was no search, no filter by role, no bulk assignment,
  // and no answer to "who holds Manage Messages?" short of opening every card.
  //
  // The table is the answer to both questions an organizer actually asks: WHO
  // IS HERE, and WHO CAN DO WHAT. Everything it does is a call the app already
  // made from somewhere else — the row overflow is the same shared moderation
  // menu the card's ⋯ and the member list's right-click render.
  import RailShell from "./RailShell.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import Select from "../Select.svelte";
  import {
    S,
    activeGuild,
    flash,
    refreshRightPanel,
    openContextMenu,
    openProfilePopover,
  } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { PERM, PERM_LIST, has } from "../lib/perms.js";
  import { moderationItems } from "../lib/moderation.svelte.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const canAssign = $derived(!!g && (g.isOwner || has(g.myPerms || 0, PERM.MANAGE_ROLES)));

  let q = $state("");
  let sortKey = $state("name");
  let sortDir = $state(1);
  let roleFilter = $state(""); // "" all · a role id · "none" · a permission bit
  let picked = $state(new Set());
  let bulkRoleID = $state("");
  let busy = $state(false);

  const roles = $derived(S.roles || []);
  const roleName = (id) => roles.find((r) => r.id === id)?.name || "";

  // The fingerprint arrives space-grouped; this is the short form a moderator
  // reads aloud, and the whole string is what Copy puts on the clipboard.
  const shortFpr = (f) => (f || "").replace(/\s+/g, "").slice(0, 12).replace(/(.{4})/g, "$1 ").trim();

  const rows = $derived.by(() => {
    const needle = q.trim().toLowerCase();
    let out = (S.members || []).filter((m) => {
      if (needle) {
        const hay = `${m.name || ""} ${m.username || ""} ${m.fingerprint || ""}`.toLowerCase();
        if (!hay.includes(needle)) return false;
      }
      if (roleFilter === "none") return !(m.roleIds || []).length && !m.isOwner;
      if (roleFilter.startsWith("perm:")) {
        const bit = Number(roleFilter.slice(5));
        return m.isOwner || has(m.perms || 0, bit);
      }
      if (roleFilter) return (m.roleIds || []).includes(roleFilter);
      return true;
    });
    const rank = (m) => (m.isOwner ? 2 : m.isHeir ? 1 : 0);
    out = [...out].sort((a, b) => {
      let d = 0;
      if (sortKey === "name") d = (a.name || "").localeCompare(b.name || "");
      else if (sortKey === "roles") d = (a.roleIds || []).length - (b.roleIds || []).length;
      else if (sortKey === "rank") d = rank(a) - rank(b);
      else if (sortKey === "fpr") d = (a.fingerprint || "").localeCompare(b.fingerprint || "");
      return d * sortDir || (a.name || "").localeCompare(b.name || "");
    });
    return out;
  });

  function sortBy(k) {
    if (sortKey === k) sortDir = -sortDir;
    else ((sortKey = k), (sortDir = 1));
  }

  function toggle(fpr) {
    const next = new Set(picked);
    if (next.has(fpr)) next.delete(fpr);
    else next.add(fpr);
    picked = next;
  }
  function toggleAll() {
    picked = picked.size === rows.length ? new Set() : new Set(rows.map((m) => m.fingerprint));
  }

  // Bulk grant/revoke walks the same AssignRole the profile card calls, one op
  // per member. Sequential on purpose: each is a signed governance op with a
  // sequence number, and firing forty in parallel would make forty ops racing
  // for the same next Seq.
  async function bulkRole(roleID, add) {
    if (!roleID || !picked.size || busy) return;
    busy = true;
    let ok = 0;
    const failed = [];
    for (const fpr of picked) {
      try {
        await api.assignRole(S.activeGuildId, fpr, roleID, add);
        ok++;
      } catch (err) {
        failed.push(String(err?.message || err));
      }
    }
    await refreshRightPanel();
    busy = false;
    picked = new Set();
    if (failed.length) flash(failed[0]);
    else flash(`${add ? "Gave" : "Took"} ${roleName(roleID)} ${add ? "to" : "from"} ${ok} member${ok === 1 ? "" : "s"}`, "success");
  }

  function rowMenu(el, m) {
    const items = moderationItems(m);
    const extra = [
      { label: "View profile", icon: "members", onClick: () => openProfilePopover(m.fingerprint, el) },
      {
        label: "Copy fingerprint",
        icon: "copy",
        onClick: () => {
          navigator.clipboard?.writeText(m.fingerprint);
          flash("Fingerprint copied", "success");
        },
      },
    ];
    const all = items.length
      ? [{ header: true, label: m.name || "Member" }, ...extra, { sep: true }, ...items]
      : [{ header: true, label: m.name || "Member" }, ...extra];
    // Anchor to the button's box, not a point on its corner: a zero-size
    // point at the right edge hung the menu into the dim and made the ⋯
    // look like it had opened the wrong thing.
    openContextMenu({ preventDefault() {}, stopPropagation() {} }, all, {
      title: m.name,
      anchorEl: el,
      align: "end",
      rowEl: el,
    });
  }
</script>

<RailShell title="Members" wide {onClose}>
  <div class="bar">
    <label class="search">
      <Icon name="search" size={14} />
      <input bind:value={q} placeholder="Search name or fingerprint" aria-label="Search members" />
    </label>
    <div class="filt">
      <Select
        label="Filter members"
        value={roleFilter}
        onPick={(v) => (roleFilter = v)}
        options={[
          { value: "", label: `Everyone (${S.members.length})` },
          { value: "none", label: "No role" },
          ...roles.map((r) => ({ value: r.id, label: r.name })),
          ...PERM_LIST.map((p) => ({ value: `perm:${p.bit}`, label: `Can ${p.label.toLowerCase()}` })),
        ]}
      />
    </div>
  </div>

  {#if canAssign && picked.size > 0}
    <!-- The bulk bar only exists while a selection does; a permanently parked
         row of disabled controls reads as broken. -->
    <div class="bulk" role="group" aria-label="Bulk role actions">
      <strong>{picked.size} selected</strong>
      <Select
        label="Role to grant or revoke"
        placeholder="Choose a role…"
        value={bulkRoleID}
        onPick={(v) => (bulkRoleID = v)}
        options={[{ value: "", label: "Choose a role…" }, ...roles.map((r) => ({ value: r.id, label: r.name }))]}
      />
      <button disabled={busy || !bulkRoleID} onclick={() => bulkRole(bulkRoleID, true)}>
        Grant
      </button>
      <button class="ghost" disabled={busy || !bulkRoleID} onclick={() => bulkRole(bulkRoleID, false)}>
        Revoke
      </button>
      <button class="ghost" onclick={() => (picked = new Set())}>Clear</button>
    </div>
  {/if}

  {#if !rows.length}
    <p class="muted empty">
      {q.trim() || roleFilter ? "Nobody here matches that." : "Nobody else is in this guild yet."}
    </p>
  {:else}
    <div class="tbl-wrap">
      <table class="tbl">
        <thead>
          <tr>
            {#if canAssign}
              <th class="pick">
                <input
                  type="checkbox"
                  checked={picked.size === rows.length && rows.length > 0}
                  onchange={toggleAll}
                  aria-label="Select every listed member"
                />
              </th>
            {/if}
            <th><button class="sortbtn" onclick={() => sortBy("name")}>Name{sortKey === "name" ? (sortDir > 0 ? " ↑" : " ↓") : ""}</button></th>
            <th><button class="sortbtn" onclick={() => sortBy("roles")}>Roles{sortKey === "roles" ? (sortDir > 0 ? " ↑" : " ↓") : ""}</button></th>
            <th><button class="sortbtn" onclick={() => sortBy("rank")}>Standing{sortKey === "rank" ? (sortDir > 0 ? " ↑" : " ↓") : ""}</button></th>
            <th class="fpr"><button class="sortbtn" onclick={() => sortBy("fpr")}>Fingerprint{sortKey === "fpr" ? (sortDir > 0 ? " ↑" : " ↓") : ""}</button></th>
            <th class="act">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as m (m.fingerprint)}
            <tr class:sel={picked.has(m.fingerprint)} data-menu-row>
              {#if canAssign}
                <td class="pick">
                  <input
                    type="checkbox"
                    checked={picked.has(m.fingerprint)}
                    onchange={() => toggle(m.fingerprint)}
                    aria-label={`Select ${m.name || "member"}`}
                  />
                </td>
              {/if}
              <td class="who">
                <Avatar name={m.name || m.fingerprint} image={m.avatar} emoji={m.emoji} color={m.color} size={24} />
                <span class="nm">
                  <span class="n1">{m.name || "Unknown"}</span>
                  {#if m.username && m.username !== m.name}
                    <span class="n2">{m.username}</span>
                  {/if}
                </span>
              </td>
              <td class="roles">
                {#if m.roleIds?.length}
                  {#each m.roleIds as rid (rid)}
                    {@const r = roles.find((x) => x.id === rid)}
                    {#if r}
                      <span class="pill" style="--rc:{r.color || 'var(--accent)'}">{r.name}</span>
                    {/if}
                  {/each}
                {:else}
                  <span class="none">—</span>
                {/if}
              </td>
              <td class="rank">
                {#if m.isOwner}<span class="badge owner">Owner</span>{/if}
                {#if m.isHeir}<span class="badge heir">Heir</span>{/if}
                {#if m.pending}<span class="badge">Invited</span>{/if}
                {#if m.mutedUntil > Date.now() / 1000}<span class="badge muted-b">Muted</span>{/if}
              </td>
              <td class="fpr">
                <button
                  class="fprbtn"
                  title="Copy the full fingerprint"
                  onclick={() => {
                    navigator.clipboard?.writeText(m.fingerprint);
                    flash("Fingerprint copied", "success");
                  }}
                >
                  {shortFpr(m.fingerprint)}… <Icon name="copy" size={11} />
                </button>
              </td>
              <td class="act">
                <button
                  class="dots"
                  aria-label={`Actions for ${m.name || "member"}`}
                  onclick={(e) => rowMenu(e.currentTarget, m)}
                >
                  <Icon name="dots" size={15} />
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <!-- Stated rather than faked: nothing in MLS records WHEN a member joined,
         so a "Joined" column would have to be invented. -->
    <p class="muted foot">
      {rows.length} of {S.members.length} shown. Concord keeps no join dates — the group's
      membership is a live fact, not a ledger of arrivals.
    </p>
  {/if}
</RailShell>

<style>
  .bar {
    display: flex;
    gap: var(--sp-2);
    align-items: center;
  }
  /* Fixed, not content-sized: a picker that resizes the search box beside it
     every time you change the filter is a moving target. */
  .filt {
    flex: none;
    width: 172px;
  }
  .search {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 10px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-faint);
  }
  .search input {
    flex: 1;
    border: 0;
    background: transparent;
    padding: 7px 0;
  }
  .search input:focus {
    outline: none;
  }
  .bulk {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
    padding: 8px 10px;
    background: var(--accent-soft);
    border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
    border-radius: var(--radius-sm);
    font-size: var(--fs-small);
  }
  /* The role picker in the bulk bar is one control among four in a tight row;
     it takes the width it needs and no more. */
  .bulk :global(.menu-root) {
    min-width: 160px;
  }
  .bulk button {
    padding: 5px 11px;
    font-size: var(--fs-small);
  }
  /* The scroller is the WRAPPER, not the table: `overflow` on a flex child
     zeroes its automatic minimum size, which is how a table inside a sheet
     collapsed to an 8px sliver once before.
     It is also the query CONTAINER. Six columns wanted 489px and the box gave
     them 416, so 73px was off the right edge with no scrollbar drawn to say so
     — and what was off the edge was the ⋯ carrying Make admin / Mute / Kick /
     Ban, which is the entire reason a moderator opens this screen. A viewport
     media query is the wrong instrument (it cannot see the rail beside it, or
     the app's own zoom); the wrapper measures itself. */
  .tbl-wrap {
    flex: none;
    overflow-x: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    container-type: inline-size;
    container-name: memtbl;
  }
  /* Fingerprint gives way first, and it is the right one to lose: it is key
     material a moderator READS, and this table exists to ACT. It is still one
     click away on the row's own menu (Copy fingerprint) and on the profile
     card. Actions never gives way — it pins to the right edge instead. */
  @container memtbl (max-width: 560px) {
    .fpr {
      display: none;
    }
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-small);
  }
  th {
    text-align: left;
    font-weight: 600;
    color: var(--text-muted);
    background: var(--bg-2);
    white-space: nowrap;
  }
  th,
  td {
    padding: 7px 10px;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
    vertical-align: middle;
  }
  tbody tr:last-child td {
    border-bottom: 0;
  }
  tbody tr:hover {
    background: var(--bg-2);
  }
  tbody tr.sel {
    background: var(--accent-soft);
  }
  .sortbtn {
    background: none;
    border: 0;
    padding: 0;
    font: inherit;
    color: inherit;
    font-weight: 600;
  }
  .sortbtn:hover {
    color: var(--text);
  }
  .pick {
    width: 30px;
  }
  .who {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
  }
  .nm {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .n1 {
    font-size: var(--fs-ui);
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .n2 {
    color: var(--text-faint);
  }
  .roles {
    max-width: 260px;
  }
  .pill {
    display: inline-block;
    margin: 1px 3px 1px 0;
    padding: 2px 7px;
    border-radius: 999px;
    font-size: var(--fs-small);
    color: var(--rc);
    border: 1px solid color-mix(in srgb, var(--rc) 55%, transparent);
    background: color-mix(in srgb, var(--rc) 14%, transparent);
  }
  .none {
    color: var(--text-faint);
  }
  .badge {
    display: inline-block;
    margin-right: var(--sp-1);
    padding: 1px 6px;
    border-radius: var(--radius-sm);
    font-size: var(--fs-small);
    background: var(--bg-3);
    color: var(--text-muted);
    white-space: nowrap;
  }
  .badge.owner {
    background: color-mix(in srgb, var(--accent) 20%, transparent);
    color: var(--accent-hover);
  }
  .badge.heir {
    background: color-mix(in srgb, var(--warn) 20%, transparent);
    color: var(--warn-text);
  }
  .badge.muted-b {
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    color: var(--danger-text);
  }
  td.fpr {
    white-space: nowrap;
  }
  .fprbtn {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    white-space: nowrap;
    background: none;
    border: 0;
    padding: 0;
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  .fprbtn:hover {
    color: var(--text);
  }
  /* Pinned to the right edge so that whatever else scrolls, the thing you came
     to press is on screen. The row's own tint is translucent (accent-soft is a
     color-mix into transparent), so it is painted as a layer OVER the dialog's
     opaque ground rather than as a background colour — otherwise a scrolled
     name would read straight through the pinned cell. */
  .act {
    width: 52px;
    text-align: right;
    position: sticky;
    right: 0;
    background-color: var(--bg-elevated);
    background-image: linear-gradient(var(--rowbg, transparent), var(--rowbg, transparent));
    border-left: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  th.act {
    background-color: var(--bg-2);
    background-image: none;
    text-align: right;
  }
  tbody tr {
    --rowbg: transparent;
  }
  tbody tr:hover {
    --rowbg: var(--bg-2);
  }
  tbody tr.sel {
    --rowbg: var(--accent-soft);
  }
  /* A visible affordance, not a hover reveal: the row's only way to act on a
     person must not be invisible until a pointer finds it (and a finger never
     hovers). */
  .dots {
    width: 32px;
    height: 32px;
    display: inline-grid;
    place-items: center;
    background: transparent;
    border: none;
    color: var(--text-muted);
    border-radius: 50%;
  }
  .dots:hover,
  :global(tr[data-menu-target]) .dots {
    background: var(--bg-3);
    color: var(--text);
  }
  .foot {
    font-size: var(--fs-small);
    line-height: 1.5;
    margin: 0;
  }
  .empty {
    text-align: center;
    padding: var(--sp-5) 0;
  }
</style>
