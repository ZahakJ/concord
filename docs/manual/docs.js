// The Concord manual's behavior: theme, the sidebar on phones, the search
// palette, the outline's current section and copy buttons. Written by hand;
// scripts/build-docs.mjs writes the pages that load it.
(() => {
  const base = (window.__docs && window.__docs.base) || "";
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];
  const store = {
    get: (k) => { try { return localStorage.getItem(k); } catch { return null; } },
    set: (k, v) => { try { localStorage.setItem(k, v); } catch { /* private window */ } },
  };

  // Theme: follow the system until the reader picks, then remember the pick.
  const root = document.documentElement;
  $$("[data-theme]").forEach((b) => b.addEventListener("click", () => {
    const dark = root.dataset.theme ? root.dataset.theme === "dark" : !matchMedia("(prefers-color-scheme: light)").matches;
    root.dataset.theme = dark ? "light" : "dark";
    store.set("concord-docs-theme", root.dataset.theme);
  }));

  // The sidebar is a drawer on narrow screens.
  const nav = $("[data-nav]");
  const scrim = $("[data-scrim]");
  const menu = $("[data-menu]");
  const setNav = (open) => {
    nav.classList.toggle("is-open", open);
    scrim.hidden = !open;
    menu.setAttribute("aria-expanded", String(open));
  };
  menu.addEventListener("click", () => setNav(!nav.classList.contains("is-open")));
  scrim.addEventListener("click", () => setNav(false));
  const current = $(".nav__link.is-current", nav);
  if (current) current.scrollIntoView({ block: "center" });

  // Copy buttons on code blocks.
  $$(".doc pre").forEach((pre) => {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "copy";
    b.textContent = "Copy";
    b.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(pre.querySelector("code").innerText);
        b.textContent = "Copied";
        b.classList.add("is-done");
        setTimeout(() => { b.textContent = "Copy"; b.classList.remove("is-done"); }, 1400);
      } catch { /* clipboard refused; the text is still selectable */ }
    });
    pre.append(b);
  });

  // The outline marks the section being read.
  const tocLinks = $$(".toc .toc__link");
  if (tocLinks.length && "IntersectionObserver" in window) {
    const byId = new Map(tocLinks.map((a) => [decodeURIComponent(a.hash.slice(1)), a]));
    const heads = [...byId.keys()].map((id) => document.getElementById(id)).filter(Boolean);
    const mark = () => {
      let at = heads[0];
      for (const h of heads) if (h.getBoundingClientRect().top < 120) at = h;
      tocLinks.forEach((a) => a.classList.toggle("is-active", byId.get(at.id) === a));
    };
    new IntersectionObserver(mark, { rootMargin: "-80px 0px -60% 0px" }).observe(document.body);
    heads.forEach((h) => new IntersectionObserver(mark, { rootMargin: "-80px 0px -60% 0px" }).observe(h));
    mark();
  }
  $$(".toc-inline .toc__link").forEach((a) => a.addEventListener("click", () => a.closest("details").removeAttribute("open")));

  // Search: a palette over an index of every section of every page.
  const palette = $("[data-palette]");
  const input = $("[data-search]", palette);
  const results = $("[data-results]", palette);
  let index = null;
  let active = 0;
  let opener = null;
  const load = () => index || (index = fetch(`${base}search.json`).then((r) => r.json()).catch(() => []));
  const open = () => {
    opener = document.activeElement;
    palette.hidden = false;
    document.body.style.overflow = "hidden";
    input.focus();
    input.select();
    load().then(() => draw());
  };
  const close = () => {
    palette.hidden = true;
    document.body.style.overflow = "";
    if (opener && opener.focus) opener.focus();
  };
  const escHtml = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const fold = (s) => s.toLowerCase().normalize("NFKD").replace(/[̀-ͯ]/g, "");
  const highlight = (text, terms) => {
    let html = escHtml(text);
    for (const t of terms) html = html.replace(new RegExp(`(${t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")})`, "gi"), "<mark>$1</mark>");
    return html;
  };
  const excerpt = (text, terms) => {
    const at = terms.length ? fold(text).indexOf(terms[0]) : -1;
    if (at < 60) return text.slice(0, 180);
    return `…${text.slice(at - 50, at + 130)}`;
  };
  async function draw() {
    const data = await load();
    const q = fold(input.value.trim());
    const terms = q.split(/\s+/).filter(Boolean);
    if (!terms.length) {
      results.innerHTML = "";
      return;
    }
    const scored = [];
    for (const e of data) {
      const h = fold(e.h), p = fold(e.p), t = fold(e.t);
      let score = 0;
      let ok = true;
      for (const term of terms) {
        // A page's title counts most on its own opening entry, so the page
        // about a thing outranks every section that mentions it.
        const s = (h.includes(term) ? 10 : 0) + (p.includes(term) ? (e.h ? 3 : 14) : 0) + (t.includes(term) ? 1 : 0);
        if (!s) { ok = false; break; }
        score += s;
      }
      if (!ok) continue;
      if (h.startsWith(terms[0]) || p.startsWith(terms[0])) score += 4;
      if (!e.h) score += 2;
      scored.push({ e, score });
    }
    scored.sort((a, b) => b.score - a.score);
    const top = scored.slice(0, 14);
    if (!top.length) {
      results.innerHTML = `<div class="palette__empty">Nothing in the manual matches “${escHtml(input.value.trim())}”.</div>`;
      return;
    }
    // Group by page, keeping the order of each page's best hit.
    const groups = new Map();
    for (const { e } of top) {
      if (!groups.has(e.p)) groups.set(e.p, []);
      groups.get(e.p).push(e);
    }
    let html = "";
    let k = 0;
    for (const [page, hits] of groups) {
      html += `<div class="palette__group">${escHtml(page)}</div>`;
      for (const e of hits) {
        html += `<a class="hit" role="option" id="hit-${k}" data-k="${k++}" href="${base}${e.u}"><span class="hit__title">${highlight(e.h || e.p, terms)}</span><span class="hit__text">${highlight(excerpt(e.t, terms), terms)}</span></a>`;
      }
    }
    results.innerHTML = html;
    select(0);
  }
  const select = (k) => {
    const hits = $$(".hit", results);
    if (!hits.length) return;
    active = (k + hits.length) % hits.length;
    hits.forEach((h, i) => h.classList.toggle("is-active", i === active));
    hits[active].scrollIntoView({ block: "nearest" });
    input.setAttribute("aria-activedescendant", hits[active].id);
  };
  input.addEventListener("input", draw);
  input.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown") { e.preventDefault(); select(active + 1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); select(active - 1); }
    else if (e.key === "Enter") {
      const hit = $$(".hit", results)[active];
      if (hit) { e.preventDefault(); close(); location.href = hit.href; }
    }
  });
  results.addEventListener("mousemove", (e) => {
    const hit = e.target.closest(".hit");
    if (hit && Number(hit.dataset.k) !== active) select(Number(hit.dataset.k));
  });
  results.addEventListener("click", (e) => { if (e.target.closest(".hit")) close(); });
  palette.addEventListener("mousedown", (e) => { if (e.target === palette) close(); });
  $$("[data-open-search]").forEach((b) => b.addEventListener("click", open));
  document.addEventListener("keydown", (e) => {
    const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName) || document.activeElement.isContentEditable;
    if ((e.key === "k" && (e.ctrlKey || e.metaKey)) || (e.key === "/" && !typing)) {
      e.preventDefault();
      if (palette.hidden) open(); else close();
    } else if (e.key === "Escape") {
      if (!palette.hidden) close();
      else if (nav.classList.contains("is-open")) setNav(false);
    }
  });
  const kbd = $(".top__search kbd");
  if (kbd && /Mac|iPhone|iPad/.test(navigator.platform)) kbd.textContent = "⌘ K";
})();
