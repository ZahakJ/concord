// listnav.js — a list of rows is ONE stop in the Tab order.
//
// The sidebar's channel list and the inspector's member list were a Tab stop
// per control: each channel row was three (the row, its "Channel options"
// chevron, its bell) and each member row two (the row, its "More"), all of
// them hover-only controls that a keyboard user could not see until they had
// tabbed onto them. From the inspector to the composer took about forty
// presses, and the arrows did nothing in either list.
//
// So, the same contract the message feed (feedtabs.js) and the toolbars
// (roving.js) keep: Tab reaches the list once — on the current row, or the row
// you were last on — ↑/↓/Home/End move between rows, a letter jumps to the
// next row whose name starts with what you typed, and Tab leaves. A row's
// secondary controls are never in the Tab order; everything they do is in the
// row's own menu, which the menu key or Shift+F10 opens from the row exactly
// as a right-click does.
//
//   <div use:listNav={{ rows: ".channel", current: ".active .channel", park: ".mute-btn", name: ".ch-name" }}>
//
// rows    the focusable row elements, in reading order
// current the row that holds the stop when you arrive (the open channel);
//         after that the row you last left, then the first row
// park    controls inside the list taken out of the Tab order
// name    where in a row its typeahead name lives (default: the row's text)
//
// The rows are read out of the DOM on every keystroke and every mutation,
// because these lists change under you: a category folds, a voice channel
// grows a participant, a member comes online and re-sorts.

// The next row index for a navigation key, or -1 when the key is not one.
export function stepIndex(len, at, key) {
  if (!len) return -1;
  if (key === "ArrowDown") return Math.min(len - 1, at + 1);
  if (key === "ArrowUp") return Math.max(0, at - 1);
  if (key === "Home") return 0;
  if (key === "End") return len - 1;
  return -1;
}

// typeahead — the index of the first name after `at` (wrapping round) that
// starts with `query`, case-insensitively; -1 when none does. A query of one
// repeated letter cycles through the rows starting with that letter, the way
// a file list answers "s s s".
export function typeahead(names, at, query) {
  const q = String(query || "").toLowerCase();
  if (!q || !names.length) return -1;
  const same = q.length > 1 && [...q].every((c) => c === q[0]);
  const want = same ? q[0] : q;
  // A fresh multi-letter query may match the row you are on; a cycling one
  // must move on from it.
  const from = same || q.length === 1 ? 1 : 0;
  for (let k = from; k < names.length + from; k++) {
    const i = (at + k) % names.length;
    if (String(names[i] || "").trim().toLowerCase().replace(/^#\s*/, "").startsWith(want)) return i;
  }
  return -1;
}

const visible = (el) => el.offsetParent !== null || el === document.activeElement;

export function listNav(node, opts) {
  let o = opts || {};
  let last = null;
  let lastCur = null;
  let frame = 0;
  let typed = "";
  let typedAt = 0;

  const rows = () => [...node.querySelectorAll(o.rows)].filter(visible);
  const nameOf = (row) => (o.name && row.querySelector(o.name)?.textContent) || row.textContent || "";

  function setTab(el, v) {
    if (el.getAttribute("tabindex") !== v) el.setAttribute("tabindex", v);
  }

  function sync() {
    frame = 0;
    const rs = rows();
    if (o.park) for (const el of node.querySelectorAll(o.park)) setTab(el, "-1");
    if (!rs.length) return;
    const act = document.activeElement;
    const cur = o.current ? node.querySelector(o.current) : null;
    // The row you were last on keeps the stop — Tab out and Shift+Tab back
    // returns you there — until the current row itself changes (you opened
    // another channel by some other road), which is where you then are.
    if (cur !== lastCur) {
      lastCur = cur;
      if (!node.contains(act)) last = null;
    }
    let stop = rs.includes(act) ? act : null;
    if (!stop && last?.isConnected && rs.includes(last)) stop = last;
    if (!stop && cur && rs.includes(cur)) stop = cur;
    stop ||= rs[0];
    for (const r of rs) setTab(r, r === stop ? "0" : "-1");
  }
  const later = () => {
    if (!frame) frame = requestAnimationFrame(sync);
  };

  // What a right-click on the row does, from the keyboard: the row's own
  // contextmenu handler, given a point under the row's start so the menu
  // opens where the row is rather than at 0,0.
  function openMenu(row) {
    const b = row.getBoundingClientRect();
    row.dispatchEvent(
      new MouseEvent("contextmenu", { bubbles: true, cancelable: true, view: window, clientX: b.left + 24, clientY: b.bottom }),
    );
  }

  function go(row) {
    last = row;
    row.focus();
    row.scrollIntoView?.({ block: "nearest" });
    sync();
  }

  function onKey(e) {
    const rs = rows();
    const at = rs.indexOf(e.target);
    if (at < 0 || e.defaultPrevented) return;
    if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) {
      e.preventDefault();
      openMenu(rs[at]);
      return;
    }
    // Alt+↑/↓ and friends are the app's channel steppers; leave every chord
    // with a modifier to them.
    if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return;
    const to = stepIndex(rs.length, at, e.key);
    if (to >= 0) {
      e.preventDefault();
      go(rs[to]);
      return;
    }
    if (e.key.length === 1 && e.key !== " " && !e.isComposing) {
      const now = Date.now();
      typed = now - typedAt > 700 ? e.key : typed + e.key;
      typedAt = now;
      const hit = typeahead(rs.map(nameOf), at, typed);
      // Only a key that found a row is the list's; any other letter carries on
      // to the composer, which is where typing goes everywhere else.
      if (hit >= 0) {
        e.preventDefault();
        go(rs[hit]);
      }
    }
  }

  function onIn(e) {
    if (rows().includes(e.target)) last = e.target;
    cancelAnimationFrame(frame);
    sync();
  }

  node.addEventListener("keydown", onKey);
  node.addEventListener("focusin", onIn);
  node.addEventListener("focusout", later);
  const mo = new MutationObserver(later);
  mo.observe(node, { childList: true, subtree: true, attributes: true, attributeFilter: ["class", "tabindex"] });
  sync();
  return {
    update(next) {
      o = next || {};
      later();
    },
    destroy() {
      cancelAnimationFrame(frame);
      node.removeEventListener("keydown", onKey);
      node.removeEventListener("focusin", onIn);
      node.removeEventListener("focusout", later);
      mo.disconnect();
    },
  };
}
