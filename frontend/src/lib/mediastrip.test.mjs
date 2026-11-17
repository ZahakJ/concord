// node src/lib/mediastrip.test.mjs
import { asIsKind, asIsLine } from "./mediastrip.js";

let failed = 0;
function check(name, ok) {
  if (!ok) {
    failed++;
    console.error("FAIL", name);
  }
}

// The core's words (internal/app/attach.go, MediaAsIsMarker), as the transport
// hands them over.
const core = (k) => new Error(`rpc SendFile: app: the details can't be removed from this ${k}, so it would be sent as it is`);
check("video", asIsKind(core("video")) === "video");
check("photo", asIsKind(core("photo")) === "photo");
check("file", asIsKind(core("file")) === "file");
check("a plain string", asIsKind("app: the details can't be removed from this photo, so it would be sent as it is") === "photo");
check("other errors are not it", asIsKind(new Error("file must be 1 byte – 25 MB")) === "");
check("nothing", asIsKind(undefined) === "");
check("the line names the file and the location", asIsLine("video") === "Concord couldn't remove the details stored in this video. If you send it, it goes as it is, including any location stored in it.");
check("unknown kinds read as file", asIsLine("weird").includes("this file."));

if (failed) process.exit(1);
console.log("mediastrip: ok");
