// Presence options + custom-status helpers, shared by the self row and the
// status popover. Dot colors mirror Avatar.svelte's presence shading.
export const PRESENCE_OPTIONS = [
  { id: "online", label: "Online", desc: "Around and up for a chat", color: "var(--ok)" },
  { id: "idle", label: "Idle", desc: "Away from the keyboard for a bit", color: "#f0b232" },
  { id: "dnd", label: "Do Not Disturb", desc: "Here, but heads-down", color: "#f04747" },
  { id: "invisible", label: "Invisible", desc: "Appear offline to everyone else", color: "var(--text-faint)" },
];

export const presenceLabel = (id) => PRESENCE_OPTIONS.find((p) => p.id === id)?.label || "Online";

// A custom status is stored as one string ("🎮 Gaming"). Split a leading emoji
// (plus its optional variation selector) off so the UI can render and edit the
// two parts separately.
export function splitStatus(s) {
  const m = /^(\p{Extended_Pictographic}\uFE0F?)\s*(.*)$/u.exec(s || "");
  return m ? { emoji: m[1], text: m[2] } : { emoji: "", text: (s || "").trim() };
}

export const joinStatus = (emoji, text) => [emoji, text].filter(Boolean).join(" ").trim();
