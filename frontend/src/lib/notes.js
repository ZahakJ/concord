// notes.js — which conversations are Notes.
//
// An account can hold more than one Notes group (two devices each started
// one). The core picks one as the Notes (`dmNotes`); any other is a copy
// (`dmNotesCopy`), shown under its own name, and `dmNotesCopyEmpty` when it
// holds nothing. A copy is still yours alone: never a conversation with a
// person, so never a call, an unread badge, a "You: " preview or a Close. Every
// place that tells a person's DM from Notes asks here, so a copy cannot pass
// for a DM at one of them.

// isNotesLike: Notes, or another copy of it.
export const isNotesLike = (g) => !!(g && g.kind === "dm" && (g.dmNotes || g.dmNotesCopy));

// isPersonDM: a conversation with somebody (one-to-one or a group).
export const isPersonDM = (g) => !!(g && g.kind === "dm" && !isNotesLike(g));

// listedNotesCopy: another copy that holds notes, and so is listed.
export const listedNotesCopy = (g) => !!(g && g.dmNotesCopy && !g.dmNotesCopyEmpty);
