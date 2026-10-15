// A typing line announces a message that has not arrived yet. When it does
// arrive, the line is over: leaving it to its 4-second timer kept "X is
// typing…" on screen under the very message it announced. Only a message's
// first arrival ends it; an edit, a reaction or a backfilled row re-emitted
// under the same id says nothing about what the person is doing now.
export function typistsAfter(list, m, activeChannelId, firstSight) {
  if (!firstSight || m.deleted || m.backfill) return { keep: list, gone: [] };
  if (m.channelId !== activeChannelId || !m.sender) return { keep: list, gone: [] };
  const gone = list.filter((t) => t.from === m.sender);
  if (!gone.length) return { keep: list, gone };
  return { keep: list.filter((t) => t.from !== m.sender), gone };
}
