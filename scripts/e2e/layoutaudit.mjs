// layoutaudit.mjs — a spacing check run inside the page (spec
// layout-spacing.spec.mjs). It reads the laid-out boxes and reports four kinds
// of fault, each with a short path to the element and its rectangle:
//
//   edge      a visible text run, icon or control (a control with no fill,
//             border or shadow is measured by its icon or label) closer than `edge` px to the
//             inner edge of the panel it sits in (a panel is the nearest box
//             that reads as a surface: a scroll container, or a box at least
//             120 x 48 with its own background, border or shadow). A scroll
//             container is only checked across its scroll axis. On a phone,
//             also closer than `screenEdge` px to the left or right of the
//             screen, or than `edge` px plus the safe-area inset to its top or
//             bottom.
//   overlap   two in-flow siblings whose boxes cross.
//   overflow  the page scrolls sideways, or something runs off the screen
//             without an ancestor clipping it.
//   clipped   text cut by its own box without an ellipsis (or a line clamp).
//
// Media (img, video, canvas) is meant to bleed and is never a candidate; a
// row that spans its panel is transparent (its contents are checked instead);
// what sits under a dialog, a scrim or another layer is skipped (hit-tested
// at its centre). An element, or an ancestor, marked data-audit="bleed" is
// skipped on purpose; each such mark carries its reason beside it in the
// source.
//
// auditLayout is serialised into the page, so it closes over nothing.
export function auditLayout(opts) {
  const edge = opts.edge ?? 8;
  const screenEdge = opts.screenEdge ?? 12;
  const phone = !!opts.phone;
  const ignore = opts.ignore || [];
  const vw = document.documentElement.clientWidth;
  const vh = window.innerHeight;
  const px = (v) => parseFloat(v) || 0;
  const rootCS = getComputedStyle(document.documentElement);
  const safeTop = px(rootCS.getPropertyValue("--safe-top"));
  const safeBottom = px(rootCS.getPropertyValue("--safe-bottom"));
  const hits = [];
  const csCache = new Map();
  const cs = (el) => {
    let c = csCache.get(el);
    if (!c) csCache.set(el, (c = getComputedStyle(el)));
    return c;
  };
  const skipped = (el) => !!el.closest('[data-audit="bleed"], .sr-only, [aria-hidden="true"] .sr-only') || ignore.some((s) => el.closest(s));
  const path = (el) => {
    const parts = [];
    for (let a = el, i = 0; a && a !== document.body && i < 4; a = a.parentElement, i++) {
      const cls = [...a.classList].filter((c) => !/^svelte-/.test(c)).slice(0, 2);
      parts.unshift(a.tagName.toLowerCase() + (cls.length ? "." + cls.join(".") : ""));
    }
    return parts.join(" > ");
  };
  // The part of a box that is actually painted: cut by every clipping
  // ancestor and by the screen.
  const clipped = (el, r) => {
    let { left, top, right, bottom } = r;
    for (let a = el.parentElement; a && a !== document.documentElement; a = a.parentElement) {
      const c = cs(a);
      const cx = c.overflowX !== "visible";
      const cy = c.overflowY !== "visible";
      if (cx || cy) {
        const ar = a.getBoundingClientRect();
        if (cx) {
          left = Math.max(left, ar.left);
          right = Math.min(right, ar.right);
        }
        if (cy) {
          top = Math.max(top, ar.top);
          bottom = Math.min(bottom, ar.bottom);
        }
      }
      if (c.position === "fixed") break;
    }
    left = Math.max(left, 0);
    top = Math.max(top, 0);
    right = Math.min(right, vw);
    bottom = Math.min(bottom, vh);
    return right - left > 1 && bottom - top > 1 ? { left, top, right, bottom } : null;
  };
  // Painted on top at its centre (not under a dialog or another layer).
  const onTop = (el, r) => {
    const x = (r.left + r.right) / 2;
    const y = (r.top + r.bottom) / 2;
    const at = document.elementFromPoint(x, y);
    return !!at && (at === el || el.contains(at) || at.contains(el));
  };
  const scrolls = (c) => /(auto|scroll)/.test(c.overflowY) || /(auto|scroll)/.test(c.overflowX);
  const isPanel = (a) => {
    if (a === document.body) return true;
    const c = cs(a);
    if (scrolls(c)) return true;
    const r = a.getBoundingClientRect();
    if (r.width < 120 || r.height < 48) return false;
    const bg = c.backgroundColor;
    const hasBg = (bg && bg !== "transparent" && !/rgba\(.*,\s*0\)$/.test(bg)) || c.backgroundImage !== "none";
    const borders = ["Top", "Right", "Bottom", "Left"].filter((s) => px(c[`border${s}Width`]) > 0 && c[`border${s}Style`] !== "none").length;
    return hasBg || borders >= 2 || c.boxShadow !== "none";
  };
  const panelOf = (el) => {
    for (let a = el.parentElement; a; a = a.parentElement) if (isPanel(a)) return a;
    return document.body;
  };
  const inner = (p) => {
    const r = p.getBoundingClientRect();
    const c = cs(p);
    return {
      left: r.left + px(c.borderLeftWidth),
      right: r.right - px(c.borderRightWidth),
      top: r.top + px(c.borderTopWidth),
      bottom: r.bottom - px(c.borderBottomWidth),
    };
  };
  const hScroller = (el) => {
    for (let a = el.parentElement; a; a = a.parentElement) if (/(auto|scroll)/.test(cs(a).overflowX)) return true;
    return false;
  };
  const CONTROL = 'button, a[href], input, select, textarea, [role="button"], [role="tab"], [role="radio"], [role="menuitem"], [role="switch"], [role="checkbox"], [role="option"]';
  const MEDIA = "img, video, canvas, picture";
  const visible = (el) => (el.checkVisibility ? el.checkVisibility({ opacityProperty: true, visibilityProperty: true }) : true);

  // ---- candidates ----
  const cands = []; // { el, r }
  // A control drawn as nothing but its icon or label (no fill, border or
  // shadow at rest) is measured by what it draws, not by its hit box.
  const ghost = (el) => {
    const c = cs(el);
    const bg = c.backgroundColor;
    const hasBg = (bg && bg !== "transparent" && !/rgba\(.*,\s*0\)$/.test(bg)) || c.backgroundImage !== "none";
    const border = ["Top", "Right", "Bottom", "Left"].some((s) => px(c[`border${s}Width`]) > 0 && c[`border${s}Style`] !== "none");
    // A bare text field inside a drawn box is part of that box.
    return !hasBg && !border && c.boxShadow === "none";
  };
  const walk = (el) => {
    if (!visible(el) || skipped(el)) return;
    if (el.matches(MEDIA)) return;
    if (el.matches(CONTROL) && ghost(el)) {
      for (const ch of el.children) walk(ch);
      textOf(el);
      return;
    }
    if (el.matches(CONTROL)) {
      const r = el.getBoundingClientRect();
      const p = panelOf(el);
      const pr = p.getBoundingClientRect();
      // A row spanning its panel: its own padding holds its contents.
      if (r.width >= 0.8 * pr.width && p !== document.body) {
        for (const ch of el.children) walk(ch);
        textOf(el);
        return;
      }
      cands.push({ el, r });
      return;
    }
    if (el instanceof SVGElement) {
      if (el.tagName.toLowerCase() === "svg") cands.push({ el, r: el.getBoundingClientRect() });
      return;
    }
    textOf(el);
    for (const ch of el.children) walk(ch);
  };
  function textOf(el) {
    for (const n of el.childNodes) {
      if (n.nodeType !== 3 || !n.textContent.trim()) continue;
      const range = document.createRange();
      range.selectNodeContents(n);
      const r = range.getBoundingClientRect();
      if (r.width > 0 && r.height > 0) cands.push({ el, r, text: n.textContent.trim().slice(0, 40) });
    }
  }
  walk(document.body);

  const seen = new Set();
  for (const c of cands) {
    const cr = clipped(c.el, c.r);
    if (!cr || !onTop(c.el, cr)) continue;
    // Cut sideways, by the screen or by a box that hides its overflow. A
    // drawing may run past its frame; text and controls may not (except in
    // a sideways scroller, or as a line that ends in an ellipsis).
    if ((c.r.right > cr.right + 1 || c.r.left < cr.left - 1) && !(c.el instanceof SVGElement)) {
      const ell = [c.el, c.el.parentElement].some((x) => x && cs(x).textOverflow === "ellipsis");
      if (!hScroller(c.el) && !ell) hits.push({ kind: "overflow", path: path(c.el), text: c.text, rect: cr });
      continue;
    }
    const p = panelOf(c.el);
    const pi = inner(p);
    const pc = cs(p);
    const sx = /(auto|scroll)/.test(pc.overflowX);
    const sy = /(auto|scroll)/.test(pc.overflowY);
    const gaps = [];
    if (!sx) gaps.push(["left", c.r.left - pi.left], ["right", pi.right - c.r.right]);
    if (!sy) gaps.push(["top", c.r.top - pi.top], ["bottom", pi.bottom - c.r.bottom]);
    for (const [side, g] of gaps) {
      if (p === document.body) continue; // the screen rule below covers the page edge
      if (g < edge - 0.5 && g > -2) {
        const key = `${path(c.el)}|${side}`;
        if (seen.has(key)) continue;
        seen.add(key);
        hits.push({ kind: "edge", side, gap: Math.round(g * 10) / 10, path: path(c.el), panel: path(p), text: c.text, rect: cr });
      }
    }
    if (phone && !hScroller(c.el)) {
      const sg = [
        ["screen-left", c.r.left, screenEdge],
        ["screen-right", vw - c.r.right, screenEdge],
        ["screen-top", c.r.top - safeTop, edge],
        ["screen-bottom", vh - safeBottom - c.r.bottom, edge],
      ];
      for (const [side, g, min] of sg) {
        if (g < min - 0.5 && g > -2) {
          const key = `${path(c.el)}|${side}`;
          if (seen.has(key)) continue;
          seen.add(key);
          hits.push({ kind: "edge", side, gap: Math.round(g * 10) / 10, path: path(c.el), text: c.text, rect: cr });
        }
      }
    }
  }

  // ---- overlap between in-flow siblings ----
  for (const el of document.body.querySelectorAll("*")) {
    // A drawing's parts are meant to cross; only the drawing's box counts.
    if (el.children.length < 2 || el instanceof SVGElement || skipped(el) || !visible(el)) continue;
    const kids = [];
    for (const k of el.children) {
      if (kids.length > 60) break;
      const c = cs(k);
      if (c.position === "absolute" || c.position === "fixed" || c.display === "none" || c.display === "contents") continue;
      if (k.matches(MEDIA) || skipped(k) || !visible(k)) continue;
      const r = k.getBoundingClientRect();
      if (r.width < 2 || r.height < 2) continue;
      const cr = clipped(k, r);
      if (!cr || !onTop(k, cr)) continue;
      kids.push({ k, r });
    }
    for (let i = 0; i < kids.length; i++)
      for (let j = i + 1; j < kids.length; j++) {
        const a = kids[i].r;
        const b = kids[j].r;
        const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
        const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        if (w > 2 && h > 2) hits.push({ kind: "overlap", path: path(kids[i].k), other: path(kids[j].k), rect: { left: Math.max(a.left, b.left), top: Math.max(a.top, b.top), right: Math.min(a.right, b.right), bottom: Math.min(a.bottom, b.bottom) } });
      }
  }

  // ---- the page scrolling sideways ----
  const sw = document.scrollingElement.scrollWidth;
  if (sw > vw + 1) hits.push({ kind: "overflow", path: "document", text: `scrollWidth ${sw} > ${vw}`, rect: { left: vw - 4, top: 0, right: vw, bottom: vh } });

  // ---- text cut without an ellipsis ----
  for (const el of document.body.querySelectorAll("*")) {
    if (skipped(el) || !visible(el) || el.matches(MEDIA)) continue;
    if (![...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim())) continue;
    const c = cs(el);
    const r = el.getBoundingClientRect();
    if (r.width < 2 || r.height < 2) continue;
    const cr = clipped(el, r);
    if (!cr || !onTop(el, cr)) continue;
    if (c.overflowX !== "visible" && el.scrollWidth > el.clientWidth + 1 && c.textOverflow !== "ellipsis" && !/(auto|scroll)/.test(c.overflowX))
      hits.push({ kind: "clipped", side: "x", path: path(el), text: el.textContent.trim().slice(0, 40), rect: cr });
    else if (
      c.overflowY !== "visible" &&
      !/(auto|scroll)/.test(c.overflowY) &&
      el.scrollHeight > el.clientHeight + 2 &&
      c.webkitLineClamp === "none" &&
      c.textOverflow !== "ellipsis"
    )
      hits.push({ kind: "clipped", side: "y", path: path(el), text: el.textContent.trim().slice(0, 40), rect: cr });
  }
  return hits;
}

// Draws each hit as a red box with its kind, for the annotated capture.
export function markHits(hits) {
  const layer = document.createElement("div");
  layer.id = "__audit";
  layer.style.cssText = "position:fixed;inset:0;z-index:2147483647;pointer-events:none";
  for (const h of hits) {
    const b = document.createElement("div");
    const r = h.rect;
    b.style.cssText = `position:absolute;left:${r.left - 2}px;top:${r.top - 2}px;width:${r.right - r.left + 4}px;height:${r.bottom - r.top + 4}px;outline:2px solid #ff2d55;background:rgba(255,45,85,.12)`;
    const t = document.createElement("span");
    t.textContent = h.kind + (h.side ? `:${h.side}` : "");
    t.style.cssText = "position:absolute;left:0;top:-14px;font:10px/12px monospace;color:#fff;background:#ff2d55;padding:0 2px;white-space:nowrap";
    b.appendChild(t);
    layer.appendChild(b);
  }
  document.body.appendChild(layer);
}
export function clearHits() {
  document.getElementById("__audit")?.remove();
}
