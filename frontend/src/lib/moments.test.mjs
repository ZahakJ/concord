// Moments play grouped by author, oldest-first within an author, from the
// first unseen one — the order the member-panel tray used before Moments moved
// to the guild menu.
import { momentsPlaylist } from "./moments.js";

const fail = (msg) => {
  console.error("  FAIL", msg);
  process.exitCode = 1;
};

const stories = [
  { id: "a3", author: "A", seen: false },
  { id: "b2", author: "B", seen: true },
  { id: "a2", author: "A", seen: true },
  { id: "b1", author: "B", seen: false },
  { id: "a3", author: "A", seen: false }, // a duplicate from a racing re-fetch
  { id: "a1", author: "A", seen: true },
];
const { list, start } = momentsPlaylist(stories);
const ids = list.map((s) => s.id).join(",");
if (ids !== "a1,a2,a3,b1,b2") fail(`order: ${ids}`);
if (list[start].id !== "a3") fail(`starts on the first unseen, got ${list[start]?.id}`);
{
  const p = momentsPlaylist(stories, "B");
  if (p.list[p.start].id !== "b1") fail("a named author starts on their first unseen");
}
{
  const p = momentsPlaylist([{ id: "x", author: "A", seen: true }]);
  if (p.start !== 0) fail("all seen starts at the beginning");
}
if (momentsPlaylist([]).list.length !== 0) fail("nothing to play");

if (!process.exitCode) console.log("moments.test.mjs: ok");
