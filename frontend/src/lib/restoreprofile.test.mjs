import { foundName, keepTypedArgs, restoreStep } from "./restoreprofile.js";

let failures = 0;
const check = (what, ok) => {
  if (!ok) {
    console.error(`  FAIL ${what}`);
    failures++;
  }
};

const fp = "JIQD ETYB XQOK JWGT UYZ2 5ZF5 AEEO A6JZ";
check("the fingerprint stub is not a found name", foundName({ displayName: "JIQD", fingerprint: fp }) === "");
check("nothing is not a found name", foundName({ displayName: "", fingerprint: fp }) === "");
check("a real name is found", foundName({ displayName: "Avicenna", fingerprint: fp }) === "Avicenna");

check("nothing back yet: wait", restoreStep({ found: "", typed: "" }) === "waiting");
check("nothing back, something typed: still wait", restoreStep({ found: "", typed: "Avi" }) === "waiting");
check("back, nothing typed: found", restoreStep({ found: "Avicenna", typed: "" }) === "found");
check("back, same name typed: found", restoreStep({ found: "Avicenna", typed: " Avicenna " }) === "found");
check("back, different name typed: ask", restoreStep({ found: "Avicenna", typed: "Avi" }) === "conflict");
// The name this screen saved for now is not a profile that came back.
check("own provisional name is not found", restoreStep({ found: "Avi", typed: "Avi", provisional: "Avi" }) === "waiting");

const id = {
  displayName: "Avicenna", status: "reading", emoji: "🦉", color: "#3366ff", avatar: "data:image/png;base64,AA",
  banner: "preset:dusk", presence: "idle", bio: "physician", color2: "#112233", frame: "gold", effect: "",
  style: { speed: "slow" },
};
const args = keepTypedArgs(id, "Avi");
check("keeping the typed name changes only the name", args[0] === "Avi" && args[1] === "reading" && args[4] === id.avatar && args[7] === "physician");
check("the style rides through", args[11] === JSON.stringify({ speed: "slow" }));
check("twelve positional arguments (birthday untouched)", args.length === 12);

if (failures) {
  console.error(`restoreprofile: ${failures} failure(s)`);
  process.exit(1);
}
console.log("restoreprofile: ok");
