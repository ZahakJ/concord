// joinerror.js — what the Join dialog says when a code does not get you in.
//
// It used to say one thing for every failure ("That code didn't open the door.
// Ask your friend for a fresh one.") and print the core's own string under it.
// Most failures are not the code's fault: the person who made it is offline (a
// fresh code from them is just as offline), you are banned (no code helps), or
// they are no longer able to add anyone. Each gets its own sentence, and the
// raw string never reaches the screen.
//
// Returns { title, detail, field }: title replaces the dialog's subtitle,
// detail sits under the field, and field says whether the fault is in what was
// typed (the field is marked invalid) or somewhere else.
export function joinFailure(err) {
  const text = String(err?.message ?? err ?? "").toLowerCase();
  const has = (...words) => words.some((w) => text.includes(w));
  if (has("banned")) {
    return {
      title: "You're banned from this guild.",
      detail: "A new code won't help unless an admin lifts the ban.",
      field: false,
    };
  }
  if (has("were removed")) {
    return {
      title: "You were removed from this guild.",
      detail: "An admin has to let you back in from the moderation log before an invite will work.",
      field: false,
    };
  }
  if (has("bad invite code", "invite code", "decode", "illegal base64", "unexpected end", "invalid character")) {
    return {
      title: "That isn't a whole invite code.",
      detail: "Check you copied all of it — codes are long and easy to clip.",
      field: true,
    };
  }
  if (has("unknown guild")) {
    return {
      title: "Whoever made this code isn't in the guild any more.",
      detail: "Ask someone who is still a member for a new code.",
      field: false,
    };
  }
  if (has("not authorized to admit", "permission to invite")) {
    return {
      title: "Whoever made this code can't add members any more.",
      detail: "Ask an admin of the guild for a new code.",
      field: false,
    };
  }
  if (has("catching up")) {
    return {
      title: "The person who made this code is catching up.",
      detail: "The code is fine — try again in a moment.",
      field: false,
    };
  }
  if (has("offline", "connection failed", "dial", "timeout", "deadline", "no addresses", "context canceled", "unreachable", "couldn't reach")) {
    return {
      title: "The code is fine, but its owner isn't reachable.",
      detail: "Whoever made it needs to be online for you to join. Try again when they are.",
      field: false,
    };
  }
  return {
    title: "That code didn't get you in.",
    detail: "Try again, or ask for a new code.",
    field: false,
  };
}
