// Maps keyed by a guild, channel or category id answer only for their own
// entries, whatever the id. One block per kind of map. (`npm test`.)
import { own, listFor } from "./lookup.js";
import { mergeUnreadChunk } from "./readstate.js";
import { resolve as notifLevel, setChannel, migrateMutes, normalize } from "./notifs.js";

let failures = 0;
const ok = (c, m) => {
  if (!c) {
    console.error("  FAIL " + m);
    failures++;
  }
};
const quietly = (fn) => {
  try {
    return { v: fn() };
  } catch (e) {
    return { err: e };
  }
};
const IDS = ["constructor", "__proto__", "toString", "valueOf", "hasOwnProperty"];
const good = "0123456789abcdef0123456789abcdef";

// ---- the unread recount: rows from the core, keyed by channel id ------------
{
  const tally = (msgs, id) => (msgs.length ? { count: msgs.length, mentions: 0, id } : null);
  for (const id of IDS) {
    const r = quietly(() => mergeUnreadChunk({}, [id, good], { [good]: [{}, {}] }, tally));
    ok(!r.err, `the unread recount throws for channel "${id}": ${r.err?.message}`);
    ok(r.v && r.v[good]?.count === 2, `a real channel is still counted beside "${id}"`);
    ok(r.v && !Object.hasOwn(r.v, id), `channel "${id}" the core did not answer for is left alone`);
  }
  // A row the core answers under "__proto__" must not change the map's prototype.
  const odd = mergeUnreadChunk({}, ["__proto__", good], JSON.parse(`{"__proto__": [{}], "${good}": [{}]}`), tally);
  ok(Object.getPrototypeOf(odd) === Object.prototype, "an answered __proto__ row changed the unread map's prototype");
  ok(odd[good]?.count === 1, "the real channel beside it is still counted");
  const kept = mergeUnreadChunk({ [good]: { count: 5 } }, [good], {}, tally);
  ok(kept[good].count === 5, "a channel the core left out keeps its badge");
  const cleared = mergeUnreadChunk({ [good]: { count: 5 } }, [good], { [good]: [] }, tally);
  ok(!(good in cleared), "a channel the core says is read is cleared");
}

// ---- per-guild lists (events, rosters): listFor ------------------------------
{
  const byGuild = { [good]: [{ id: "e" }] };
  for (const id of IDS) {
    const r = quietly(() => [...listFor(byGuild, id)].length);
    ok(!r.err && r.v === 0, `the events of guild "${id}" must be an empty list`);
  }
  ok(listFor(byGuild, good).length === 1, "a real guild's events");
  ok(listFor({ [good]: "x" }, good).length === 0, "a non-list entry reads as empty");
}

// ---- notification levels, keyed by channel and guild id -----------------------
{
  const st = normalize({ channels: { [good]: "mentions" }, guilds: {} });
  for (const id of IDS) {
    ok(notifLevel(st, id, id) === "all", `the level of channel/guild "${id}" must be the default`);
    const m = quietly(() => migrateMutes(st, { [id]: true }));
    ok(!m.err && Object.getPrototypeOf(m.v.channels) === Object.prototype, `muting channel "${id}" must not change the map's prototype`);
    if (id !== "__proto__") ok(own(m.v.channels, id) === "none", `muting channel "${id}" records it`);
    const s = quietly(() => setChannel(st, id, "none"));
    ok(!s.err, `setting channel "${id}" throws`);
  }
  ok(notifLevel(st, good, "") === "mentions", "a real channel's level");
}

// ---- generic: own() on a map that holds the key as data -------------------------
{
  const m = JSON.parse(`{"constructor": 3, "${good}": 1}`);
  ok(own(m, "constructor") === 3 && own(m, good) === 1 && own(m, "toString") === undefined, "own() reads data, not members");
}

if (failures) {
  console.error(`idmaps: ${failures} failure(s)`);
  process.exit(1);
}
console.log("idmaps: ok");
