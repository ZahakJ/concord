// Which conversations are Notes (`npm test` runs it).
import { isNotesLike, isPersonDM, listedNotesCopy } from "./notes.js";

let failures = 0;
const check = (what, ok) => {
  if (!ok) {
    console.error(`  FAIL ${what}`);
    failures++;
  }
};

const notes = { kind: "dm", dmNotes: true, dmMembers: 1 };
const copy = { kind: "dm", name: "Notes (other copy)", dmNotesCopy: true, dmMembers: 1 };
const emptyCopy = { ...copy, dmNotesCopyEmpty: true };
const dm = { kind: "dm", name: "Bashir", dmMembers: 2 };
const guild = { kind: "", name: "Keyboarders" };

check("Notes is Notes", isNotesLike(notes) && !isPersonDM(notes));
check("an other copy is Notes, not a person", isNotesLike(copy) && !isPersonDM(copy));
check("an empty other copy too", isNotesLike(emptyCopy) && !isPersonDM(emptyCopy));
check("a DM is with a person", !isNotesLike(dm) && isPersonDM(dm));
check("a guild is neither", !isNotesLike(guild) && !isPersonDM(guild));
check("nothing is neither", !isNotesLike(null) && !isPersonDM(undefined));
check("an other copy holding notes is listed", listedNotesCopy(copy));
check("an empty other copy is not", !listedNotesCopy(emptyCopy));
check("Notes itself is not a copy", !listedNotesCopy(notes));

console.log(failures === 0 ? "notes.test.mjs: OK" : `notes.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
