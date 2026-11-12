import { bioRoom, hasVisible, utf8Bytes, BIO_MAX_BYTES } from "./profilefields.js";

let failures = 0;
const check = (what, ok) => {
  if (!ok) {
    console.error(`  FAIL ${what}`);
    failures++;
  }
};

// The bio is counted the way the core counts it: bytes. 600 Arabic letters are
// 1200 bytes — over, and SAID to be over, not cut to 300 behind a success toast.
check("ascii bytes", utf8Bytes("abc") === 3);
check("arabic is two bytes a letter", utf8Bytes("ع") === 2);
const arabic = "ع".repeat(600);
check("600 Arabic letters are over", bioRoom(arabic).over && bioRoom(arabic).used === 1200);
check("300 Arabic letters fit exactly", !bioRoom("ع".repeat(300)).over && bioRoom("ع".repeat(300)).left === 0);
check("600 Latin letters fit", !bioRoom("a".repeat(600)).over);
check("the budget is the core's", BIO_MAX_BYTES === 600);
check("outer whitespace is not counted (the core trims)", bioRoom("  hi  ").used === 2);

// A name needs something a person can see.
check("zero-width only", !hasVisible("​​​"));
check("bidi override only", !hasVisible("‮‬"));
check("hangul filler", !hasVisible("ㅤ"));
check("braille blank", !hasVisible("⠀ "));
check("spaces", !hasVisible("   "));
check("plain name", hasVisible("Ada"));
check("Arabic name", hasVisible("أمين"));
check("emoji name", hasVisible("🦊"));
check("Persian name with ZWNJ", hasVisible("می‌خواهم"));

console.log(failures === 0 ? "profilefields.test.mjs: OK" : `profilefields.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
