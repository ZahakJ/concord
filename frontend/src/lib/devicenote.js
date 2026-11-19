// devicenote.js — what the new-device line in a 1:1 says and offers.
//
// The core writes the line when a contact's account gains a device
// (internal/app/devicewatch.go): "Ann added a new device (Pixel, 11-14). If
// this wasn't Ann, verify." Verifying someone compares their ACCOUNT
// fingerprint, and linking a device does not change it. So "Verified" is only
// ever about the person, never about the device, and nobody has checked the
// device itself. A verified contact's line therefore shows both: that the
// person is verified, and that this device is new and not checked.
//
//   contact not verified (or unknown here)  stored text   + button "Verify"
//   contact verified                         first sentence + tag "New device, not checked"
//                                                           + mark "Verified"
//
// For a verified contact the stored "If this wasn't Ann, verify." is dropped:
// scanning the same account's code again cannot tell whether Ann linked this
// device herself, and the line must not ask for a check it already shows done.

export const DEVICE_VERIFY = "Verify";
export const DEVICE_VERIFIED = "Verified";
export const DEVICE_UNCHECKED = "New device, not checked";

const VERIFY_TAIL = / If this wasn't .+, verify\.$/s;

// deviceNote({ content, verified, name }) -> { text, action, tag, markTitle }
//   action "verify":   draw the Verify button
//   action "verified": draw the Verified mark and the not-checked tag
export function deviceNote({ content = "", verified = false, name = "" } = {}) {
  const text = String(content);
  if (!verified) return { text, action: "verify", tag: "", markTitle: "" };
  const who = name || "this person";
  return {
    text: text.replace(VERIFY_TAIL, ""),
    action: "verified",
    tag: DEVICE_UNCHECKED,
    markTitle: `You verified ${who}. That checked their account, not this device.`,
  };
}
