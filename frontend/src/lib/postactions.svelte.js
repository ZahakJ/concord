// The things you can do TO a forum post, in one place.
//
// They lived inside ForumView, which meant they were reachable only from the
// board: to mark a question answered you went back to the board, found the
// card and right-clicked it — at exactly the moment you were reading the
// answer. The post view now renders the same actions, off the same list, with
// the same gating.
import { S, flash, refreshGuilds } from "./state.svelte.js";
import { api } from "./api.js";
import { haptic } from "./touch.js";
import { PERM, has } from "./perms.js";

// canModeratePosts: pin, close, and curate anyone's post. Manage Messages,
// because closing silences other people.
export function canModeratePosts(guild) {
  return !!guild && (guild.isOwner || has(guild.myPerms || 0, PERM.MANAGE_MESSAGES));
}

// mayCurate also accepts the post's own author — a member may mark their own
// question answered or re-tag it without being handed a moderator bit. A post
// with no synced opening message has no provable author, so nobody but a
// moderator curates it (matches the backend).
export function mayCuratePost(guild, post) {
  return (
    canModeratePosts(guild) ||
    (!!post?.authorFingerprint && post.authorFingerprint === S.identity.fingerprint)
  );
}

export async function setPostPinned(guild, post, pinned, after) {
  try {
    await api.setPostPinned(guild.id, post.id, pinned);
    haptic("medium");
    flash(pinned ? "Post pinned" : "Post unpinned", "success");
    await after?.();
  } catch (err) {
    flash(err);
  }
}

export async function setPostSolved(guild, post, solved, after) {
  try {
    await api.setPostSolved(guild.id, post.id, solved);
    haptic("medium");
    flash(solved ? "Marked answered" : "Reopened", "success");
    await after?.();
  } catch (err) {
    flash(err);
  }
}

export async function setPostLocked(guild, post, locked, after) {
  try {
    await api.setPostLocked(guild.id, post.id, locked);
    haptic("medium");
    flash(locked ? "Post closed" : "Post reopened", "success");
    // The closed state lives on the CHANNEL record, which the guild view
    // carries, so the header the reader is looking at only updates if the
    // guild is refreshed — the board's own refresh reaches the board.
    await refreshGuilds();
    await after?.();
  } catch (err) {
    flash(err);
  }
}

// Deleting a post takes the whole thread with it, so it asks first. The
// confirm names the post: "Delete this post?" on a board of forty is a
// question about which one.
export function confirmDeletePost(guild, post, after) {
  S.modal = {
    kind: "confirm",
    title: `Delete "${post.title || post.name || "this post"}"?`,
    body: "The post and every reply in it are removed for everyone. This can't be undone.",
    confirmLabel: "Delete post",
    danger: true,
    onConfirm: async () => {
      S.modal = null;
      try {
        await api.deleteChannel(guild.id, post.id);
        haptic("heavy");
        flash("Post deleted", "success");
        await after?.();
      } catch (err) {
        flash(err);
      }
    },
  };
}
