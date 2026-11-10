<script>
  // Who is in a group DM, and — for whoever can change that — adding and
  // removing people. A group used to show only its two-face avatar and its
  // name, so there was no way to see who was in it and no way to add anyone.
  //
  // Adding goes through the same DM invite the group was created with, so the
  // person added gets the group by themselves when they are next reachable.
  // Only verified contacts can be added, for the same reason only verified
  // contacts can start a group: the group is end-to-end encrypted to whoever
  // is in it, and a key nobody checked is a key somebody could have swapped.
  import Modal from "./Modal.svelte";
  import Avatar from "../Avatar.svelte";
  import { api } from "../lib/api.js";
  import { S, refreshGuilds, flash } from "../lib/state.svelte.js";

  let { guildId, onClose } = $props();

  let members = $state([]);
  let contacts = $state([]);
  let busy = $state("");

  const g = $derived(S.guilds.find((x) => x.id === guildId));
  const canManage = $derived(!!g?.canManage);
  const inGroup = $derived(new Set(members.map((m) => m.fingerprint)));
  const addable = $derived(contacts.filter((c) => c.verified && !inGroup.has(c.fingerprint)));

  async function load() {
    try {
      members = (await api.members(guildId)) || [];
    } catch (err) {
      flash(err);
    }
    try {
      contacts = (await api.contacts()) || [];
    } catch {
      contacts = [];
    }
  }
  load();

  async function add(c) {
    busy = c.fingerprint;
    try {
      await api.addMember(guildId, c.fingerprint);
      flash(`Invited ${c.name || "them"} — they join when they're next online`, "success");
      contacts = contacts.filter((x) => x.fingerprint !== c.fingerprint);
      await refreshGuilds();
    } catch (err) {
      flash(err);
    }
    busy = "";
  }

  async function remove(m) {
    busy = m.fingerprint;
    try {
      await api.removeMember(guildId, m.fingerprint, "");
      members = members.filter((x) => x.fingerprint !== m.fingerprint);
      await refreshGuilds();
    } catch (err) {
      flash(err);
    }
    busy = "";
  }
</script>

<Modal title="People in {g?.name || 'this group'}" {onClose}>
  <ul class="people" aria-label="Members">
    {#each members as m (m.fingerprint)}
      <li>
        <Avatar name={m.name} image={m.avatar} color={m.color} online={m.online} presence={m.presence} size={28} />
        <span class="who">{m.name || m.fingerprint.slice(0, 9)}{m.isSelf ? " (you)" : ""}</span>
        {#if !m.isSelf && !m.verified}<span class="muted tag">not verified</span>{/if}
        {#if canManage && !m.isSelf}
          <button class="ghost" disabled={!!busy} onclick={() => remove(m)}>Remove</button>
        {/if}
      </li>
    {/each}
  </ul>
  {#if canManage}
    <h4>Add people</h4>
    {#if addable.length}
      <ul class="people" aria-label="Verified contacts you can add">
        {#each addable as c (c.fingerprint)}
          <li>
            <Avatar name={c.name} image={c.avatar} color={c.color} size={28} />
            <span class="who">{c.name || c.fingerprint.slice(0, 9)}</span>
            <button class="primary" disabled={!!busy} onclick={() => add(c)}>Add</button>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted">Everyone you have verified is already here. Verify someone from their profile card to add them.</p>
    {/if}
  {:else}
    <p class="muted">Only the person who started this group can add or remove people.</p>
  {/if}
</Modal>

<style>
  .people {
    list-style: none;
    margin: 0 0 var(--sp-3);
    padding: 0;
    display: grid;
    gap: var(--sp-2);
  }
  li {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .who {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tag {
    font-size: var(--fs-small);
  }
  h4 {
    margin: var(--sp-3) 0 var(--sp-2);
    font-size: var(--fs-ui);
  }
  p {
    margin: 0;
    font-size: var(--fs-ui);
  }
</style>
