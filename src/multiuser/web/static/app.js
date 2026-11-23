// WTF's own small script: passkey sign-in, <dialog> open/close helpers and
// the navigation drawer / rail.
// Never a browser-native popup; failures are written into the page instead.
(function () {
  "use strict";

  function base64urlToBuffer(value) {
    var padded = value.replace(/-/g, "+").replace(/_/g, "/");
    var pad = padded.length % 4;
    if (pad) padded += "=".repeat(4 - pad);
    var raw = window.atob(padded);
    var buffer = new ArrayBuffer(raw.length);
    var view = new Uint8Array(buffer);
    for (var i = 0; i < raw.length; i++) view[i] = raw.charCodeAt(i);
    return buffer;
  }

  function bufferToBase64url(buffer) {
    var bytes = new Uint8Array(buffer);
    var str = "";
    for (var i = 0; i < bytes.length; i++) str += String.fromCharCode(bytes[i]);
    return window.btoa(str).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function decodeOptions(json) {
    var opts = JSON.parse(json).publicKey;
    opts.challenge = base64urlToBuffer(opts.challenge);
    if (Array.isArray(opts.allowCredentials)) {
      opts.allowCredentials = opts.allowCredentials.map(function (c) {
        return Object.assign({}, c, { id: base64urlToBuffer(c.id) });
      });
    }
    return opts;
  }

  function encodeCredential(credential) {
    var response = credential.response;
    var out = {
      id: credential.id,
      rawId: bufferToBase64url(credential.rawId),
      type: credential.type,
      response: {
        clientDataJSON: bufferToBase64url(response.clientDataJSON),
        authenticatorData: bufferToBase64url(response.authenticatorData),
        signature: bufferToBase64url(response.signature),
      },
    };
    if (response.userHandle) {
      out.response.userHandle = bufferToBase64url(response.userHandle);
    }
    return out;
  }

  // A rejected WebAuthn ceremony is the person backing out only when the
  // browser says so (NotAllowedError, AbortError); anything else is a plain
  // failure. Raw browser error text is never shown.
  function wasCancelled(e) {
    return !!e && (e.name === "NotAllowedError" || e.name === "AbortError");
  }

  function setError(message) {
    var el = document.querySelector("[data-passkey-error]");
    if (el) el.textContent = message || "";
  }

  function setRegisterError(message) {
    var el = document.querySelector("[data-passkey-register-error]");
    if (el) el.textContent = message || "";
  }

  async function signInWithPasskey(button) {
    setError("");
    if (!window.PublicKeyCredential || !navigator.credentials) {
      setError("This browser doesn't support passkeys.");
      return;
    }
    try {
      var beginResp = await fetch("/auth/passkey/login/begin", { method: "POST" });
      if (!beginResp.ok) {
        setError("Couldn't start passkey sign-in. Try again.");
        return;
      }
      var optionsJSON = await beginResp.text();
      var publicKey;
      if (typeof PublicKeyCredential.parseRequestOptionsFromJSON === "function") {
        publicKey = PublicKeyCredential.parseRequestOptionsFromJSON(JSON.parse(optionsJSON).publicKey);
      } else {
        publicKey = decodeOptions(optionsJSON);
      }

      var credential;
      try {
        credential = await navigator.credentials.get({ publicKey: publicKey });
      } catch (e) {
        setError(wasCancelled(e) ? "Passkey sign-in was cancelled." : "Couldn't sign you in with a passkey. Try again.");
        return;
      }
      if (!credential) {
        setError("Passkey sign-in was cancelled.");
        return;
      }

      var payload = typeof credential.toJSON === "function" ? credential.toJSON() : encodeCredential(credential);

      // The server validates next (same-origin relative paths only).
      var next = (button && button.getAttribute("data-next")) || "/";
      var finishResp = await fetch("/auth/passkey/login/finish?next=" + encodeURIComponent(next), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      var result = await finishResp.json().catch(function () { return {}; });
      if (!finishResp.ok || !result.ok) {
        setError(result.message || "That passkey didn't work. Try again or continue with WhatsApp.");
        return;
      }
      window.location.assign(result.next || "/");
    } catch (e) {
      setError(wasCancelled(e) ? "Passkey sign-in was cancelled." : "Couldn't sign you in with a passkey. Try again.");
    }
  }

  // addPasskey drives the registration ceremony from the account page's
  // "Add a passkey" button. The CSRF token travels on the button itself
  // (data-csrf) rather than from a form, since this is a plain button, not
  // a submit.
  async function addPasskey(button) {
    setRegisterError("");
    if (!window.PublicKeyCredential || !navigator.credentials) {
      setRegisterError("This browser doesn't support passkeys.");
      return;
    }
    var csrf = button.getAttribute("data-csrf") || "";
    try {
      var beginResp = await fetch("/auth/passkey/register/begin", {
        method: "POST",
        headers: { "X-CSRF-Token": csrf },
      });
      if (!beginResp.ok) {
        var beginResult = await beginResp.json().catch(function () { return {}; });
        setRegisterError(beginResult.message || "Couldn't start adding a passkey. Try again.");
        return;
      }
      var optionsJSON = await beginResp.text();
      var publicKey;
      if (typeof PublicKeyCredential.parseCreationOptionsFromJSON === "function") {
        publicKey = PublicKeyCredential.parseCreationOptionsFromJSON(JSON.parse(optionsJSON).publicKey);
      } else {
        var opts = JSON.parse(optionsJSON).publicKey;
        opts.challenge = base64urlToBuffer(opts.challenge);
        opts.user.id = base64urlToBuffer(opts.user.id);
        if (Array.isArray(opts.excludeCredentials)) {
          opts.excludeCredentials = opts.excludeCredentials.map(function (c) {
            return Object.assign({}, c, { id: base64urlToBuffer(c.id) });
          });
        }
        publicKey = opts;
      }

      var credential;
      try {
        credential = await navigator.credentials.create({ publicKey: publicKey });
      } catch (e) {
        setRegisterError(wasCancelled(e) ? "Adding the passkey was cancelled." : "Couldn't add the passkey. Try again.");
        return;
      }
      if (!credential) {
        setRegisterError("Adding the passkey was cancelled.");
        return;
      }

      var payload;
      if (typeof credential.toJSON === "function") {
        payload = credential.toJSON();
      } else {
        var response = credential.response;
        payload = {
          id: credential.id,
          rawId: bufferToBase64url(credential.rawId),
          type: credential.type,
          response: {
            clientDataJSON: bufferToBase64url(response.clientDataJSON),
            attestationObject: bufferToBase64url(response.attestationObject),
          },
        };
      }

      var finishResp = await fetch("/auth/passkey/register/finish", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
        body: JSON.stringify(payload),
      });
      var result = await finishResp.json().catch(function () { return {}; });
      if (!finishResp.ok || !result.ok) {
        setRegisterError(result.message || "That didn't work. Try again.");
        return;
      }
      window.location.reload();
    } catch (e) {
      setRegisterError(wasCancelled(e) ? "Adding the passkey was cancelled." : "Couldn't add the passkey. Try again.");
    }
  }

  // copyToClipboard backs the "Copy" button beside a read-only sign-in link
  // (data-copy names the id of the <input> to copy). It reports success or
  // failure in the nearest [data-copy-feedback], never a native popup.
  function copyToClipboard(button) {
    var targetId = button.getAttribute("data-copy");
    var input = targetId && document.getElementById(targetId);
    var feedback = button.parentElement && button.parentElement.parentElement &&
      button.parentElement.parentElement.querySelector("[data-copy-feedback]");
    if (!feedback) feedback = document.querySelector("[data-copy-feedback]");
    if (!input || !navigator.clipboard || !navigator.clipboard.writeText) {
      if (feedback) feedback.textContent = "Couldn't copy. Select and copy the link by hand.";
      return;
    }
    navigator.clipboard.writeText(input.value).then(function () {
      if (feedback) feedback.textContent = "Copied.";
      button.classList.add("is-copied");
      window.setTimeout(function () { button.classList.remove("is-copied"); }, 2000);
    }).catch(function () {
      if (feedback) feedback.textContent = "Couldn't copy. Select and copy the link by hand.";
    });
  }

  // The real QR code rotates, so while a QR <img> (data-qr-refresh) is
  // showing, re-point it at the same URL plus a cache-buster (Date.now())
  // every 20 seconds: one fetch per refresh, and none while it is hidden or
  // its dialog is closed.
  function refreshQR(img) {
    var base = img.getAttribute("src").split("?")[0];
    img.src = base + "?t=" + Date.now();
  }
  function qrShowing(img) {
    return document.contains(img) && img.closest("[open], .auth") && !img.closest("[data-ended]") && img.getClientRects().length > 0;
  }
  window.setInterval(function () {
    document.querySelectorAll("img[data-qr-refresh]").forEach(function (img) {
      if (qrShowing(img)) refreshQR(img);
    });
  }, 20000);

  // openDialog remembers what opened a dialog so focus goes back there on
  // close (Esc, Cancel, the close button or a backdrop click). A dialog whose
  // body an htmx request is about to refill is emptied first, so it shows the
  // loading spinner instead of the previous content.
  function openDialog(dialog, opener) {
    if (!dialog || typeof dialog.showModal !== "function") return;
    var target = opener && opener.getAttribute("hx-target");
    if (target) {
      var body = document.querySelector(target);
      if (body && dialog.contains(body)) body.innerHTML = "";
    }
    dialog._opener = opener || document.activeElement;
    if (!dialog.open) dialog.showModal();
  }

  document.addEventListener("close", function (event) {
    var dialog = event.target;
    // A finished add (data-reload-on-close) refreshes the list behind it.
    if (dialog && dialog.querySelector && dialog.querySelector("[data-reload-on-close]")) {
      window.location.reload();
      return;
    }
    // Emptying the htmx-filled body stops its QR refresh and status polling.
    var body = dialog && dialog.querySelector && dialog.querySelector("#link-dialog-body");
    if (body) body.innerHTML = "";
    if (dialog && dialog._opener && typeof dialog._opener.focus === "function" && document.contains(dialog._opener)) {
      dialog._opener.focus();
    }
  }, true);

  // A press on the backdrop (the <dialog> element itself, outside its panel)
  // closes it; Esc and the buttons inside still work as well.
  document.addEventListener("mousedown", function (event) {
    var d = event.target;
    if (d && d.tagName === "DIALOG" && d.open) {
      var r = d.getBoundingClientRect();
      var inside = event.clientX >= r.left && event.clientX <= r.right && event.clientY >= r.top && event.clientY <= r.bottom;
      if (!inside) d.close();
    }
  });

  // Navigation: a slide-over drawer at 960px and below, a sidebar that folds
  // to an icon rail above it (remembered in localStorage; boot.js restores it).
  var root = document.documentElement;
  var navOpener = document.querySelector("[data-nav-open]");
  var sidebar = document.getElementById("sidebar");

  // While the drawer is open, everything behind it is inert, so Tab stays
  // inside the menu.
  function setDrawer(open) {
    if (open) root.setAttribute("data-nav-open", "");
    else root.removeAttribute("data-nav-open");
    document.querySelectorAll(".topbar, #main").forEach(function (el) {
      if (open) el.setAttribute("inert", "");
      else el.removeAttribute("inert");
    });
    if (navOpener) navOpener.setAttribute("aria-expanded", open ? "true" : "false");
    if (open && sidebar) {
      var first = sidebar.querySelector("[aria-current='page']") || sidebar.querySelector("a, button");
      window.setTimeout(function () { if (first) first.focus(); }, 30);
    } else if (navOpener && sidebar && sidebar.contains(document.activeElement)) {
      navOpener.focus();
    }
  }

  function syncRailButton() {
    var btn = document.querySelector("[data-rail-toggle]");
    if (btn) btn.setAttribute("aria-label", root.hasAttribute("data-rail") ? "Expand menu" : "Collapse menu");
  }
  syncRailButton();

  function toggleRail() {
    var rail = !root.hasAttribute("data-rail");
    if (rail) root.setAttribute("data-rail", "");
    else root.removeAttribute("data-rail");
    try { window.localStorage.setItem("wtf-nav", rail ? "rail" : "full"); } catch (e) { /* storage refused */ }
    syncRailButton();
  }

  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape" && root.hasAttribute("data-nav-open")) setDrawer(false);
  });

  // Theme: System / Light / Dark, stored per browser. boot.js applies the
  // stored choice before paint; this keeps every switcher on the page and
  // the theme-color metas in step when it changes.
  function currentTheme() {
    var t = root.getAttribute("data-theme");
    return t === "light" || t === "dark" ? t : "system";
  }
  function syncThemeControls() {
    var t = currentTheme();
    document.querySelectorAll("[data-theme-choice]").forEach(function (b) {
      b.setAttribute("aria-pressed", b.getAttribute("data-theme-choice") === t ? "true" : "false");
    });
    document.querySelectorAll("[data-theme-select]").forEach(function (s) { s.value = t; });
  }
  function setTheme(t) {
    try {
      if (t === "system") window.localStorage.removeItem("wtf-theme");
      else window.localStorage.setItem("wtf-theme", t);
    } catch (e) { /* storage refused: applies to this page only */ }
    if (window.wtfApplyTheme) window.wtfApplyTheme(t);
    syncThemeControls();
  }
  syncThemeControls();
  document.addEventListener("change", function (event) {
    var sel = event.target.closest && event.target.closest("[data-theme-select]");
    if (sel) setTheme(sel.value);
  });

  // Instant feedback: a submitted form's button shows busy at once, and is
  // disabled after the browser has taken the submission so it cannot double-post.
  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!form || form.hasAttribute("hx-post")) return;
    var btn = event.submitter || form.querySelector("[type=submit]");
    if (!btn) return;
    btn.setAttribute("aria-busy", "true");
    window.setTimeout(function () { btn.disabled = true; }, 0);
  });
  window.addEventListener("pageshow", function () {
    document.querySelectorAll("[aria-busy='true']").forEach(function (b) {
      b.removeAttribute("aria-busy");
      b.disabled = false;
    });
  });

  // A linking session (a QR or a pairing code) ends after two minutes, or
  // when its QR image keeps failing: show "Get a new code" and stop the
  // status and QR polling by removing them.
  function endLinkSession(el, why) {
    if (!el || el.hasAttribute("data-ended")) return;
    el.setAttribute("data-ended", why);
    var body = el.closest(".link-body");
    if (body) body.querySelectorAll("[hx-trigger^='every']").forEach(function (p) { p.remove(); });
  }
  function armLinkSessions(root) {
    (root || document).querySelectorAll("[data-link-session]:not([data-armed])").forEach(function (el) {
      el.setAttribute("data-armed", "");
      var secs = parseInt(el.getAttribute("data-expires-in"), 10) || 120;
      window.setTimeout(function () { if (document.contains(el)) endLinkSession(el, "expired"); }, secs * 1000);
    });
  }
  armLinkSessions();
  document.addEventListener("htmx:afterSettle", function (event) { armLinkSessions(event.target); });

  // Until WhatsApp has produced a code, the QR image fails to load: show a
  // spinner in its frame instead of a broken image and retry; after five
  // failures in a row, say so instead.
  document.addEventListener("error", function (event) {
    var img = event.target;
    if (img && img.hasAttribute && img.hasAttribute("data-qr-refresh") && img.parentElement) {
      img.parentElement.classList.add("is-loading");
      var fails = (parseInt(img.getAttribute("data-fails"), 10) || 0) + 1;
      img.setAttribute("data-fails", String(fails));
      if (fails >= 5) {
        endLinkSession(img.closest("[data-link-session]"), "failed");
        return;
      }
      // Until the code exists, try again sooner than the 20-second refresh.
      window.setTimeout(function () { if (qrShowing(img)) refreshQR(img); }, 3000);
    }
  }, true);
  // An image that already failed before this script ran (deferred) gets the
  // same treatment.
  document.querySelectorAll("img[data-qr-refresh]").forEach(function (img) {
    if (img.complete && img.naturalWidth === 0 && img.parentElement) img.parentElement.classList.add("is-loading");
  });
  document.addEventListener("load", function (event) {
    var img = event.target;
    if (img && img.hasAttribute && img.hasAttribute("data-qr-refresh") && img.parentElement) {
      img.parentElement.classList.remove("is-loading");
      img.removeAttribute("data-fails");
    }
  }, true);

  document.addEventListener("click", function (event) {
    var loginBtn = event.target.closest("[data-passkey-login]");
    if (loginBtn) {
      event.preventDefault();
      signInWithPasskey(loginBtn);
      return;
    }
    var registerBtn = event.target.closest("[data-passkey-register]");
    if (registerBtn) {
      event.preventDefault();
      addPasskey(registerBtn);
      return;
    }
    var openBtn = event.target.closest("[data-open-dialog]");
    if (openBtn) {
      var id = openBtn.getAttribute("data-open-dialog");
      openDialog(document.getElementById(id), openBtn);
      return;
    }
    var closeBtn = event.target.closest("[data-close-dialog]");
    if (closeBtn) {
      var host = closeBtn.closest("dialog");
      if (host) host.close();
      return;
    }
    var copyBtn = event.target.closest("[data-copy]");
    if (copyBtn) {
      copyToClipboard(copyBtn);
      return;
    }
    var dismiss = event.target.closest("[data-dismiss]");
    if (dismiss) {
      var key = dismiss.getAttribute("data-dismiss");
      try { window.localStorage.setItem("wtf-dismiss-" + key, "1"); } catch (e) { /* hides for this page only */ }
      var card = dismiss.closest("[data-nudge]");
      if (card) card.remove();
      return;
    }
    var themeBtn = event.target.closest("[data-theme-choice]");
    if (themeBtn) {
      setTheme(themeBtn.getAttribute("data-theme-choice"));
      return;
    }
    var modeBtn = event.target.closest("[data-link-mode]");
    if (modeBtn) {
      var modes = modeBtn.closest("[data-link-modes]");
      if (modes) {
        modes.setAttribute("data-mode", modeBtn.getAttribute("data-link-mode"));
        var first = modes.querySelector(modeBtn.getAttribute("data-link-mode") === "pair" ? ".mode-pair input" : ".to-pair");
        if (first) first.focus();
      }
      return;
    }
    if (event.target.closest("[data-nav-open]")) {
      setDrawer(!root.hasAttribute("data-nav-open"));
      return;
    }
    if (event.target.closest("[data-nav-close]")) {
      setDrawer(false);
      return;
    }
    if (event.target.closest("[data-rail-toggle]")) {
      toggleRail();
      return;
    }
    if (root.hasAttribute("data-nav-open") && event.target.closest(".sidebar a")) {
      setDrawer(false);
    }
  });
})();
