// The channel and member lists rove: arrows step and stop at the ends, Home
// and End jump, and a typed letter finds the next row by name.
import { stepIndex, typeahead } from "./listnav.js";

const fail = (msg) => {
  console.error("  FAIL", msg);
  process.exitCode = 1;
};
const eq = (a, b, msg) => a !== b && fail(`${msg}: got ${a}, want ${b}`);

eq(stepIndex(5, 2, "ArrowDown"), 3, "down steps");
eq(stepIndex(5, 4, "ArrowDown"), 4, "down stops at the last row");
eq(stepIndex(5, 0, "ArrowUp"), 0, "up stops at the first row");
eq(stepIndex(5, 3, "Home"), 0, "Home");
eq(stepIndex(5, 1, "End"), 4, "End");
eq(stepIndex(5, 1, "x"), -1, "not a navigation key");
eq(stepIndex(0, 0, "ArrowDown"), -1, "an empty list");
// A sideways row (the guild strip) steps on ←/→ and leaves ↑/↓ alone.
eq(stepIndex(5, 2, "ArrowRight", "x"), 3, "right steps in a row");
eq(stepIndex(5, 2, "ArrowLeft", "x"), 1, "left steps back in a row");
eq(stepIndex(5, 2, "ArrowDown", "x"), -1, "down is not a row's key");
eq(stepIndex(5, 2, "ArrowRight"), -1, "right is not a column's key");

const names = ["workshop", "notices", "showcase", "supplies", "brainstorm", "# patterns"];
eq(typeahead(names, 0, "s"), 2, "a letter finds the next row starting with it");
eq(typeahead(names, 2, "s"), 3, "the same letter again moves on");
eq(typeahead(names, 3, "ss"), 2, "a repeated letter cycles, wrapping round");
eq(typeahead(names, 2, "su"), 3, "two letters narrow the match");
eq(typeahead(names, 3, "su"), 3, "a longer query may stay on the row it already matches");
eq(typeahead(names, 0, "P"), 5, "case and a leading # are ignored");
eq(typeahead(names, 0, "z"), -1, "no match");
eq(typeahead([], 0, "a"), -1, "an empty list");

if (!process.exitCode) console.log("listnav.test.mjs: ok");
