// compose.test.mjs — per-conversation staged trays.
import assert from "node:assert/strict";
import { stashStaged, takeStaged, addStaged, stagedCount } from "./compose.js";

// A tray parked in one conversation is not handed to another.
stashStaged("dm-1", [{ id: "a" }]);
assert.deepEqual(takeStaged("guild-chan"), [], "another conversation starts empty");
assert.equal(stagedCount("dm-1"), 1);
assert.deepEqual(takeStaged("dm-1"), [{ id: "a" }], "coming back restores it");
assert.deepEqual(takeStaged("dm-1"), [], "taken trays are not handed out twice");

// Stashing an empty tray forgets the conversation.
stashStaged("c1", [{ id: "x" }]);
stashStaged("c1", []);
assert.equal(stagedCount("c1"), 0);

// A file that finishes reading after the switch lands where it was picked.
stashStaged("c2", [{ id: "1" }]);
addStaged("c2", { id: "2" });
assert.deepEqual(takeStaged("c2").map((x) => x.id), ["1", "2"]);
addStaged("c3", { id: "late" });
assert.deepEqual(takeStaged("c3").map((x) => x.id), ["late"]);
stashStaged("", [{ id: "nowhere" }]);
assert.deepEqual(takeStaged(""), []);

console.log("compose: ok");
