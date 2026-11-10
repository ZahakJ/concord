// The day divider names the year for any day outside the current one.
import assert from "node:assert/strict";
import { dayLabel } from "./daylabel.js";

const now = new Date(2026, 10, 10, 15, 0); // Tue 10 Nov 2026
const L = "en-GB";
assert.equal(dayLabel(new Date(2026, 10, 10, 1).toDateString(), now, L), "Today");
assert.equal(dayLabel(new Date(2026, 10, 9, 23).toDateString(), now, L), "Yesterday");
assert.equal(dayLabel(new Date(2026, 10, 6).toDateString(), now, L), "Friday");
assert.equal(dayLabel(new Date(2026, 9, 3).toDateString(), now, L), "3 October");
// Last year's day must not pass for this year's.
assert.equal(dayLabel(new Date(2025, 10, 3).toDateString(), now, L), "3 November 2025");
assert.equal(dayLabel(new Date(2025, 11, 31).toDateString(), new Date(2026, 0, 2), L), "Wednesday");
assert.equal(dayLabel(new Date(2024, 1, 29).toDateString(), now, L), "29 February 2024");
assert.equal(dayLabel("not a date", now, L), "");
console.log("daylabel ok");
