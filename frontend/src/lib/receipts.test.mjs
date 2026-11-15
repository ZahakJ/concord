import assert from "node:assert/strict";
import { test } from "node:test";
import { tickOf, tickLabel, partialLabel } from "./receipts.js";

test("no view, no tick", () => {
  assert.equal(tickOf(undefined, true), null);
  assert.equal(tickOf({ delivered: false, read: false }, true), null);
});

test("delivered shows regardless of the read setting", () => {
  const v = { delivered: true, read: false, of: 1 };
  assert.equal(tickOf(v, false), "delivered");
  assert.equal(tickOf(v, true), "delivered");
});

test("read only shows while our own setting is on", () => {
  const v = { delivered: true, read: true, of: 1 };
  assert.equal(tickOf(v, true), "read");
  assert.equal(tickOf(v, false), "delivered");
});

test("labels", () => {
  assert.equal(tickLabel({ of: 1 }, "delivered"), "Delivered");
  assert.equal(tickLabel({ of: 1 }, "read"), "Read");
  assert.equal(tickLabel({ of: 3 }, "delivered"), "Delivered to all 3");
  assert.equal(tickLabel({ of: 3 }, "read"), "Read by all 3");
  assert.equal(tickLabel({ of: 3 }, null), "");
});

test("group partial progress", () => {
  const v = { of: 3, deliveredBy: 2, readBy: 1, delivered: false, read: false };
  assert.equal(partialLabel(v, false), "Delivered to 2 of 3");
  assert.equal(partialLabel(v, true), "Read by 1 of 3");
  assert.equal(partialLabel({ of: 1, deliveredBy: 0 }, true), "");
  assert.equal(partialLabel({ of: 3, deliveredBy: 3, delivered: true }, true), "");
});
