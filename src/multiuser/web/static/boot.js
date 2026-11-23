// Runs in <head> before the body paints: restores the folded desktop menu
// and the chosen theme, so a reload never snaps from one to the other.
(function () {
  "use strict";
  var root = document.documentElement;
  root.classList.add("js");
  try {
    if (window.localStorage.getItem("wtf-nav") === "rail") root.setAttribute("data-rail", "");
  } catch (e) { /* storage refused: full menu */ }

  // Theme: "light" or "dark" pins it (data-theme sets color-scheme, which
  // light-dark() follows); no stored value follows the system. The browser's
  // theme-color follows too.
  var COLORS = { light: "#ffffff", dark: "#111a2b" };
  function applyTheme(t) {
    if (t === "light" || t === "dark") root.setAttribute("data-theme", t);
    else root.removeAttribute("data-theme");
    var metas = document.querySelectorAll('meta[name="theme-color"][data-theme-color]');
    for (var i = 0; i < metas.length; i++) {
      var own = metas[i].getAttribute("data-theme-color");
      metas[i].setAttribute("content", COLORS[t === "light" || t === "dark" ? t : own]);
    }
  }
  window.wtfApplyTheme = applyTheme;
  var stored = null;
  try { stored = window.localStorage.getItem("wtf-theme"); } catch (e) { /* follow the system */ }
  applyTheme(stored);
})();
