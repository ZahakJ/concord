// evicted.js — what the removal band says, by why this device is out of a guild.
//
// Three different situations used to share one sentence. A ban was announced as
// a kick ("an admin can let you back in"), a kick promised that the moderation
// log's "Allow back" would bring the person back (it reopens the door; coming
// through it still takes an invite), and a newcomer welcomed by someone who
// could not admit them had no words at all — their client sat on a catching-up
// bar for a guild it had never been in.
export function evictedCopy(why) {
  switch (why) {
    case "banned":
      return {
        lead: "You were banned from",
        next: "An invite won't let you back in unless an admin lifts the ban.",
      };
    case "unseated":
      return {
        lead: "You never got into",
        next: "The invite you used came from someone who can't add members. Ask a member of the guild for a new one.",
      };
    default:
      return {
        lead: "You were removed from",
        next: "To come back, an admin has to allow it and you need a new invite.",
      };
  }
}
