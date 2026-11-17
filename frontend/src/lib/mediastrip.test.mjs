// node src/lib/mediastrip.test.mjs
import { asIsKind, asIsLine, hasTextTracks, heldRow, fileSendOp, TEXT_LINE } from "./mediastrip.js";
import { humanError } from "./errors.js";

let failed = 0;
function check(name, ok) {
  if (!ok) {
    failed++;
    console.error("FAIL", name);
  }
}

// The core's words (internal/app/attach.go, MediaAsIsMarker and
// MediaTextMarker), as the transport hands them over.
const core = (k) => new Error(`rpc SendFile: app: the details can't be removed from this ${k}, so it would be sent as it is`);
const text = new Error("rpc SendFile: app: this video has subtitles or other text tracks, which can hold location");
check("video", asIsKind(core("video")) === "video");
check("photo", asIsKind(core("photo")) === "photo");
check("file", asIsKind(core("file")) === "file");
check("a plain string", asIsKind("app: the details can't be removed from this photo, so it would be sent as it is") === "photo");
check("other errors are not it", asIsKind(new Error("file must be 1 byte – 25 MB")) === "");
check("nothing", asIsKind(undefined) === "");
check("the line names the file and the location", asIsLine("video") === "Concord couldn't remove the details stored in this video. If you send it, it goes as it is, including any location stored in it.");
check("unknown kinds read as file", asIsLine("weird").includes("this file."));

// A video with text tracks: its own question, with both choices.
check("text tracks are recognised", hasTextTracks(text) && !hasTextTracks(core("video")));
const t = heldRow(text);
check("text row", t && t.textTracks === true && t.asIs === "video" && t.error === TEXT_LINE && t.state === "failed");
check("text line", TEXT_LINE === "This video has subtitles or other text tracks, which can hold location. Concord can remove them along with the other details, or send the video as it is.");
const a = heldRow(core("photo"));
check("as-is row", a && a.textTracks === false && a.asIs === "photo" && a.error === asIsLine("photo"));
check("other failures are not held", heldRow(new Error("HTTP 500")) === null);

// The person's choice picks the core call.
check("no choice", fileSendOp({}) === "sendFile");
check("send as it is", fileSendOp({ asIsChosen: true }) === "sendFileAsIs");
check("remove and send", fileSendOp({ dropTextChosen: true }) === "sendFileWithoutText");

// A dialog without the row's buttons says where to go.
{
  check("dialog: text tracks", /message box/.test(humanError(text)));
  check("dialog: as is", /message box/.test(humanError(core("video"))));
}

if (failed) process.exit(1);
console.log("mediastrip: ok");
