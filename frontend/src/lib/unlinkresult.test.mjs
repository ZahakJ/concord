import { unlinkReport } from "./unlinkresult.js";

let failures = 0;
const check = (what, ok) => {
  if (!ok) {
    console.error(`  FAIL ${what}`);
    failures++;
  }
};
const complete = (r) => r.success.startsWith("Phone unlinked") && r.pending.length === 0 && !r.pendingTitle && !r.pendingLine && !r.error;

check("complete: success, nothing else", complete(unlinkReport("Phone", { pending: [], failed: [] })));
check("an older core's empty answer is a complete unlink", complete(unlinkReport("Phone", null)) && complete(unlinkReport("Phone", undefined)));

const r = unlinkReport("Phone", {
  pending: [
    { kind: "guild", id: "g1", name: "Book club" },
    { kind: "dm", id: "d1", name: "Carol", removableBy: "Carol" },
  ],
  failed: [],
});
check("pending is still a success, not an error", r.success.startsWith("Phone unlinked") && !r.error);
check("the list says which device", r.pendingTitle === "Phone is still listed in:");
check("one line for a toast", r.pendingLine === "Phone is still listed in: Book club, Direct message with Carol");
check("a guild by name, with who can remove it", r.pending[0].where === "Book club" && /owner or moderator/.test(r.pending[0].who));
check("a DM named as one, with who can remove it", r.pending[1].where === "Direct message with Carol" && r.pending[1].who === "Carol can remove it");
check("never calls a DM a guild", !/guild/i.test(r.pending[1].where + r.pending[1].who));
check("rows are keyed by conversation", r.pending[0].key === "g1" && r.pending[1].key === "d1");

const f = unlinkReport("Phone", { pending: [], failed: [{ kind: "dm", id: "d2", name: "Dave", removableBy: "Dave" }] });
check("a failed removal is an error with a retry", f.error === "Removing it from Direct message with Dave failed. Unlink it again to retry." && f.pending.length === 0);

console.log(failures === 0 ? "unlinkresult.test.mjs: OK" : `unlinkresult.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
