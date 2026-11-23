// node scripts/check-no-backup.test.mjs — the check refuses what lets data out.
import assert from "node:assert/strict";
import fs from "node:fs";
import { manifestProblems, rulesProblems, DOMAINS } from "./check-no-backup.mjs";

const app = (attrs) => `<manifest><application ${attrs}></application></manifest>`;
const good = 'android:allowBackup="false" android:fullBackupContent="false" android:dataExtractionRules="@xml/data_extraction_rules"';
assert.deepEqual(manifestProblems(app(good)), []);
assert.equal(manifestProblems(app('android:allowBackup="false" android:dataExtractionRules="@xml/r"')).length, 1);
assert.equal(manifestProblems(app('android:allowBackup="false" android:fullBackupContent="false"')).length, 1);
assert.equal(manifestProblems(app('android:allowBackup="true" android:fullBackupContent="false" android:dataExtractionRules="@xml/r"')).length, 1);
assert.equal(manifestProblems(`<manifest><!-- <application ${good}> --><application android:allowBackup="false"></application></manifest>`).length, 2);

const ex = DOMAINS.map((d) => `<exclude domain="${d}" path="." />`).join("");
const rules = (cb, dt) => `<data-extraction-rules><cloud-backup>${cb}</cloud-backup><device-transfer>${dt}</device-transfer></data-extraction-rules>`;
assert.deepEqual(rulesProblems(rules(ex, ex)), []);
assert.ok(rulesProblems(`<data-extraction-rules><cloud-backup>${ex}</cloud-backup></data-extraction-rules>`).some((p) => p.includes("device-transfer")));
assert.equal(rulesProblems(rules(ex, ex.replace('domain="database" path="."', 'domain="database" path="keep"'))).length, 1);
assert.ok(rulesProblems(rules(ex + '<include domain="file" path="x" />', ex)).length > 0);
assert.equal(rulesProblems(rules(ex, `<!-- ${ex} -->`)).length, DOMAINS.length);

// The shipped manifest and rules pass.
const m = "apps/mobile/android/app/src/main/AndroidManifest.xml";
assert.deepEqual(manifestProblems(fs.readFileSync(m, "utf8")), []);
assert.deepEqual(rulesProblems(fs.readFileSync("apps/mobile/android/app/src/main/res/xml/data_extraction_rules.xml", "utf8")), []);
console.log("check-no-backup.test: ok");
