import { encodeSafetyCode, parseSafetyCode, checkSafetyCode } from "./safetycode.js";

let failures = 0;
const check = (what, ok) => {
  if (!ok) {
    console.error(`  FAIL ${what}`);
    failures++;
  }
};

const ME = "AAAA BBBB CCCC DDDD EEEE FFFF GGGG HHHH";
const ANN = "2222 3333 4444 5555 6666 7777 AAAA ZZZZ";
const BOB = "CCCC DDDD EEEE FFFF GGGG HHHH 2222 3333";

const mine = encodeSafetyCode(ME, ANN);
const theirs = encodeSafetyCode(ANN, ME);
check("both sides render the same code", mine && mine === theirs);
check("version-tagged", mine.startsWith("concord-verify/1/"));
check("sorted", mine.split("/")[2] < mine.split("/")[3]);
check("nothing but the tag and two fingerprints", mine.split("/").length === 4 && !/\s/.test(mine));
check("a case/spacing difference is the same identity", encodeSafetyCode(ME.toLowerCase(), ANN.replace(/ /g, "")) === mine);
check("no code for yourself or an empty side", encodeSafetyCode(ME, ME) === "" && encodeSafetyCode(ME, "") === "");

check("scan of their code matches", checkSafetyCode(theirs, ME, ANN) === "match");
check("scan of my own code on my own screen matches", checkSafetyCode(mine, ME, ANN) === "match");
check("a code for someone else is a mismatch", checkSafetyCode(encodeSafetyCode(ME, BOB), ME, ANN) === "mismatch");
check("a code for two strangers is a mismatch", checkSafetyCode(encodeSafetyCode(ANN, BOB), ME, ANN) === "mismatch");
check("a code with a changed character is a mismatch", checkSafetyCode(mine.slice(0, -1) + "Q", ME, ANN) === "mismatch");
check("random text is invalid", checkSafetyCode("https://example.com", ME, ANN) === "invalid");
check("empty is invalid", checkSafetyCode("", ME, ANN) === "invalid");
check("an unknown version is invalid, never a match", checkSafetyCode(mine.replace("/1/", "/2/"), ME, ANN) === "invalid");
check("unsorted pair is invalid", parseSafetyCode("concord-verify/1/ZZZZ/AAAA") === null);
check("extra fields are invalid", parseSafetyCode(mine + "/Ann") === null);

console.log(failures === 0 ? "safetycode.test.mjs: OK" : `safetycode.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
