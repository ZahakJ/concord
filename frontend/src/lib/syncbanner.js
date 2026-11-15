// syncbanner.js — which actions the "catching up" banner offers.
//
// "Continue from here" drops this device's evidence that its members are ahead
// and carries on from its own epoch. That is the honest exit when nobody is
// left who could bring the missing updates. It is the wrong one when this
// device is the guild's only possible committer and its members are ahead of
// it (`cannotRepair`): nobody can re-admit it, and continuing builds a private
// copy of the guild that looks healthy. There the banner says what is true,
// at once, and the core refuses the call anyway.
export function cannotRepairText(kind) {
  const what = kind === "dm" ? "conversation" : "guild";
  return `This copy fell behind and cannot be repaired from here. Start a new ${what} with the same people.`;
}

export function syncBannerActions({ alone, cannotRepair, retriedInVain }) {
  if (alone) return { retry: false, continueHere: true, cannotRepair: false };
  return {
    retry: true,
    continueHere: !cannotRepair && !!retriedInVain,
    cannotRepair: !!cannotRepair,
  };
}
