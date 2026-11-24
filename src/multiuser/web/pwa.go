package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"io/fs"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const (
	brandCache = "public, max-age=604800"
	// themeColor matches the light theme-color in the page <head> (the top
	// bar's surface), so an installed app's title bar matches the page.
	themeColor  = "#ffffff"
	splashColor = "#0B1220"
)

// contentTypes covers every extension the static dir serves; the bare
// extension lookup in fiber's c.Type is not reliable for webmanifest/woff2/ico.
var contentTypes = map[string]string{
	".css":         "text/css; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".html":        "text/html; charset=utf-8",
	".json":        "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".webp":        "image/webp",
	".ico":         "image/x-icon",
	".woff2":       "font/woff2",
	".woff":        "font/woff",
	".txt":         "text/plain; charset=utf-8",
}

// staticType returns the Content-Type for a static file name.
func staticType(name string) string {
	if t, ok := contentTypes[strings.ToLower(extOf(name))]; ok {
		return t
	}
	return "application/octet-stream"
}

// shellAssets lists every embedded static file the service worker precaches
// (the social card is not part of the app shell).
func shellAssets() []string {
	var out []string
	_ = fs.WalkDir(staticFS, "static", func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() || strings.HasSuffix(p, "og-image.png") {
			return nil
		}
		out = append(out, "/ui/"+p)
		return nil
	})
	sort.Strings(out)
	return out
}

// cacheVersion changes whenever any embedded static file changes, so a new
// build gets a fresh cache and the old one is deleted on activate.
func cacheVersion() string {
	h := sha256.New()
	_ = fs.WalkDir(staticFS, "static", func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		b, _ := fs.ReadFile(staticFS, p)
		h.Write([]byte(p))
		h.Write(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:10]
}

const swSource = `/* Service worker: precache the app shell, never cache signed-in pages. */
const CACHE = "wtf-shell-%VERSION%";
const SHELL = %SHELL%;
const NEVER = ["/auth/", "/signin", "/signout", "/ui/devices/", "/app/", "/send/", "/user/", "/message/",
  "/chat/", "/chats", "/group/", "/newsletter/", "/devices", "/call/", "/schedule", "/mcp", "/health"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL.map((u) => new Request(u, { cache: "reload" })))).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith("wtf-shell-") && k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (e) => {
  const req = e.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (NEVER.some((p) => url.pathname.startsWith(p))) return;

  // Static shell: cache first, fill from the network.
  if (url.pathname.startsWith("/ui/static/")) {
    e.respondWith(
      caches.match(req).then((hit) => hit || fetch(req).then((res) => {
        if (res.ok) { const copy = res.clone(); caches.open(CACHE).then((c) => c.put(req, copy)); }
        return res;
      }))
    );
    return;
  }

  // Pages: network first, never stored; offline falls back to the offline page.
  if (req.mode === "navigate") {
    e.respondWith(fetch(req).catch(() => caches.match("/offline")));
  }
});
`

var offlineTmpl = template.Must(template.New("offline").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="light dark">
<title>Offline &middot; {{.Product}}</title>
<meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="#111a2b" media="(prefers-color-scheme: dark)">
<link rel="icon" href="/ui/static/brand/favicon.svg" type="image/svg+xml">
<link rel="icon" href="/favicon.ico" sizes="48x48">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
<link rel="stylesheet" href="/ui/static/app.css">
<script src="/ui/static/boot.js"></script>
</head>
<body>
<main id="main" class="auth">
  <div class="auth-inner">
    <div class="auth-brand"><a class="brand" href="/"><img class="brand-mark" src="/ui/static/brand/mark.svg" alt="" width="32" height="32"><span class="brand-name">{{.Product}}</span></a></div>
    <div class="card auth-card">
      <span class="badge-icon badge-icon-muted" aria-hidden="true"><svg class="icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h.01"/><path d="M8.5 16.429a5 5 0 0 1 7 0"/><path d="M5 12.859a10 10 0 0 1 5.17-2.69"/><path d="M19 12.859a10 10 0 0 0-2.007-1.523"/><path d="M2 8.82a15 15 0 0 1 4.177-2.643"/><path d="M22 8.82a15 15 0 0 0-11.288-3.764"/><path d="m2 2 20 20"/></svg></span>
      <h1 class="auth-title">You're offline</h1>
      <p class="auth-sub">Check your connection, then try again.</p>
      <div class="auth-actions">
        <a class="btn btn-primary btn-lg btn-block" href="/">Try again</a>
      </div>
    </div>
  </div>
</main>
</body>
</html>
`))

func manifestJSON(product string) ([]byte, error) {
	icon := func(file, size, purpose string) map[string]string {
		m := map[string]string{"src": "/ui/static/brand/" + file, "sizes": size + "x" + size, "type": "image/png"}
		if purpose != "" {
			m["purpose"] = purpose
		}
		return m
	}
	return json.Marshal(map[string]any{
		"id":               "/",
		"name":             product,
		"short_name":       product,
		"description":      "Link your WhatsApp and use it from any device.",
		"start_url":        "/",
		"scope":            "/",
		"display":          "standalone",
		"orientation":      "any",
		"background_color": splashColor,
		"theme_color":      themeColor,
		"categories":       []string{"productivity", "utilities"},
		"icons": []map[string]string{
			icon("icon-192.png", "192", "any"),
			icon("icon-512.png", "512", "any"),
			icon("icon-maskable-512.png", "512", "maskable"),
		},
	})
}

func mountPWA(app *fiber.App, d Deps) {
	manifest, _ := manifestJSON(d.ProductName)
	sw := strings.NewReplacer("%VERSION%", cacheVersion())
	shell, _ := json.Marshal(append([]string{"/offline"}, shellAssets()...))
	swBody := strings.Replace(sw.Replace(swSource), "%SHELL%", string(shell), 1)

	var offline bytes.Buffer
	_ = offlineTmpl.Execute(&offline, map[string]string{"Product": d.ProductName})

	app.Get("/manifest.webmanifest", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "application/manifest+json")
		c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
		return c.Send(manifest)
	})
	app.Get("/sw.js", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/javascript; charset=utf-8")
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Set("X-Content-Type-Options", "nosniff")
		return c.SendString(swBody)
	})
	app.Get("/offline", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentSecurityPolicy, csp)
		c.Set(fiber.HeaderContentType, "text/html; charset=utf-8")
		c.Set(fiber.HeaderCacheControl, "no-cache")
		return c.Send(offline.Bytes())
	})
	app.Get("/robots.txt", func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/plain; charset=utf-8")
		c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
		return c.SendString("User-agent: *\nDisallow: /\n")
	})
	brand := func(route, file string) {
		app.Get(route, func(c fiber.Ctx) error {
			b, err := fs.ReadFile(staticFS, "static/brand/"+file)
			if err != nil {
				return fiber.ErrNotFound
			}
			c.Set(fiber.HeaderContentType, staticType(file))
			c.Set(fiber.HeaderCacheControl, brandCache)
			return c.Send(b)
		})
	}
	brand("/favicon.ico", "favicon.ico")
	brand("/apple-touch-icon.png", "apple-touch-icon.png")
}
