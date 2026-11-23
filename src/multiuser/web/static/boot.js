// Runs in <head> before the body paints: restores the folded desktop menu
// so a reload never snaps from the full sidebar to the rail.
(function () {
  "use strict";
  var root = document.documentElement;
  root.classList.add("js");
  try {
    if (window.localStorage.getItem("wtf-nav") === "rail") root.setAttribute("data-rail", "");
  } catch (e) { /* storage refused: full menu */ }
})();
