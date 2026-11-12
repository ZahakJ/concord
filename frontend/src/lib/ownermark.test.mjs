// The owner's-crown rule, held in place (`npm test` runs it).
import { ownsPlace, ownerLabel } from "./ownermark.js";

let failures = 0;
const check = (name, ok) => {
  if (!ok) {
    failures++;
    console.error("FAIL:", name);
  }
};

check("a guild you own is marked", ownsPlace({ kind: "guild", isOwner: true }));
check("a guild with no kind field (older core) you own is marked", ownsPlace({ isOwner: true }));
check("a guild you are a member of is not", !ownsPlace({ kind: "guild", isOwner: false }));
check("an instant meeting you started is marked", ownsPlace({ kind: "meeting", isOwner: true }));
check("someone else's meeting is not", !ownsPlace({ kind: "meeting", isOwner: false }));
check("a group you started is marked", ownsPlace({ kind: "dm", isOwner: true, dmMembers: 3 }));
check("a group someone else started is not", !ownsPlace({ kind: "dm", isOwner: false, dmMembers: 3 }));
check("a one-to-one conversation has no owner", !ownsPlace({ kind: "dm", isOwner: true, dmMembers: 2 }));
check("Notes is not marked", !ownsPlace({ kind: "dm", isOwner: true, dmNotes: true, dmMembers: 1 }));
check("a guild you were removed from is not", !ownsPlace({ kind: "guild", isOwner: true, evicted: "removed" }));
check("nothing is not", !ownsPlace(null));
check("a guild says guild", ownerLabel({ kind: "meeting" }) === "You own this guild");
check("a group says group", ownerLabel({ kind: "dm" }) === "You started this group");

console.log(failures === 0 ? "ownermark.test.mjs: OK" : `ownermark.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
