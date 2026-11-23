// The splash's colours, before the first paint: see index.html.
(function () {
  try {
    var boot = document.getElementById("boot");
    var paint = JSON.parse(localStorage.getItem("concord.bootPaint") || "null");
    if (!paint) {
      var prefs = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
      var t = prefs.theme || "dark";
      var light = t === "light" || (t === "system" && matchMedia("(prefers-color-scheme: light)").matches);
      if (light) paint = { bg: "#fcfcfb", fg: "#0e7c70" };
    }
    if (paint && paint.bg) boot.style.background = paint.bg;
    if (paint && paint.fg) boot.querySelector("svg").style.color = paint.fg;
  } catch (e) {
    /* storage blocked: the dark splash it always was */
  }
})();
