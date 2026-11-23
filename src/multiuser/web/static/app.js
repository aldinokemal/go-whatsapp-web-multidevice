// WTF's own small script: passkey sign-in and <dialog> open/close helpers.
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
    var opts = JSON.parse(json);
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

  function setError(message) {
    var el = document.querySelector("[data-passkey-error]");
    if (el) el.textContent = message || "";
  }

  async function signInWithPasskey() {
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
        publicKey = PublicKeyCredential.parseRequestOptionsFromJSON(JSON.parse(optionsJSON));
      } else {
        publicKey = decodeOptions(optionsJSON);
      }

      var credential;
      try {
        credential = await navigator.credentials.get({ publicKey: publicKey });
      } catch (e) {
        setError("Passkey sign-in was cancelled.");
        return;
      }
      if (!credential) {
        setError("Passkey sign-in was cancelled.");
        return;
      }

      var payload = typeof credential.toJSON === "function" ? credential.toJSON() : encodeCredential(credential);

      var finishResp = await fetch("/auth/passkey/login/finish", {
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
      setError("Passkey sign-in was cancelled.");
    }
  }

  // The QR image polls itself every 20s with hx-swap="none" (the response is
  // a PNG, not markup to swap in), so nothing else makes the browser re-fetch
  // it once the real, rotating code behind it changes. On a successful poll,
  // re-point the <img> at the same URL plus a cache-buster so the browser
  // actually fetches a fresh frame instead of reusing the first one forever.
  document.body.addEventListener("htmx:afterRequest", function (event) {
    var el = event.detail && event.detail.elt;
    if (!el || typeof el.hasAttribute !== "function" || !el.hasAttribute("data-qr-refresh")) return;
    if (!event.detail.successful) return;
    var base = el.getAttribute("src").split("?")[0];
    el.src = base + "?t=" + Date.now();
  });

  document.addEventListener("click", function (event) {
    var loginBtn = event.target.closest("[data-passkey-login]");
    if (loginBtn) {
      event.preventDefault();
      signInWithPasskey();
      return;
    }
    var openBtn = event.target.closest("[data-open-dialog]");
    if (openBtn) {
      var id = openBtn.getAttribute("data-open-dialog");
      var dialog = document.getElementById(id);
      if (dialog && typeof dialog.showModal === "function") dialog.showModal();
      return;
    }
    var closeBtn = event.target.closest("[data-close-dialog]");
    if (closeBtn) {
      var host = closeBtn.closest("dialog");
      if (host) host.close();
    }
  });
})();
