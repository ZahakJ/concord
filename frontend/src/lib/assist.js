// assist.js — shared plumbing for the local assistant now that more than one
// engine can answer.
//
// Two engines exist: "local" (the on-device Ollama model, the default and the
// only one that keeps message content on this machine) and "brain" (a Claude
// Code session running as a local process on the user's own subscription,
// which DOES see the message content). Which one answered is not cosmetic —
// it's the difference between two different privacy stories — so every answer
// carries its `engine` and a `note` the backend writes, and the UI shows what
// came back rather than what it expected.

import { api } from "./api.js";

// engineLabel: what to print on the badge. Unknown engines are reported as
// unknown instead of being guessed into "local", because claiming a local
// answer for something that wasn't one is exactly the failure to avoid.
export function engineLabel(engine) {
  if (engine === "local") return "local";
  if (engine === "brain") return "shared brain";
  return engine || "unknown engine";
}

// awaitBrainJob polls a queued brain job until it answers.
//
// The brain isn't a request/response service: a session may be busy or not yet
// connected, so the backend queues the work and hands back a jobId. We poll
// rather than hold a socket open, and we give up rather than wait forever — a
// spinner with no end is a lie about what's happening.
//
// `cancelled` is called before every poll; when it returns true we stop and
// resolve null (the caller walked away). Throws on timeout, so the caller can
// show the same honest error path it uses for a failed request.
export async function awaitBrainJob(
  jobId,
  { intervalMs = 2000, timeoutMs = 120000, cancelled = () => false } = {},
) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (cancelled()) return null;
    await new Promise((r) => setTimeout(r, intervalMs));
    if (cancelled()) return null;
    const out = await api.assistBrainJob(jobId);
    if (out && !out.pending) return out;
  }
  throw new Error("The shared brain didn't answer in time — no session picked the job up.");
}
