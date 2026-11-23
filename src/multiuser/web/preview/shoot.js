#!/usr/bin/env node
// Screenshot sweep for the preview site written by TestPreviewSite.
//
//   node shoot.js <site-dir> <out-dir> [--viewports phone,tablet,desktop] [--schemes light,dark]
//
// Serves <site-dir> on a local port, takes every shot in its manifest.json at
// each viewport and colour scheme, writes <name>-<viewport>-<scheme>.png,
// report.json (horizontal overflow, console errors) and contact-sheet.png.
"use strict";
const http = require("http");
const fs = require("fs");
const path = require("path");
const { chromium } = require("playwright");

const args = process.argv.slice(2);
const site = path.resolve(args[0] || ".preview/site");
const out = path.resolve(args[1] || ".preview/shots");
function opt(name, def) {
  const i = args.indexOf("--" + name);
  return (i >= 0 ? args[i + 1] : def).split(",");
}
const VIEWPORTS = { phone: [390, 844], tablet: [820, 1180], desktop: [1440, 900] };
const viewports = opt("viewports", "phone,tablet,desktop");
const schemes = opt("schemes", "light,dark");
const only = opt("only", "").filter(Boolean);

const TYPES = { ".html": "text/html", ".css": "text/css", ".js": "text/javascript", ".png": "image/png",
  ".svg": "image/svg+xml", ".woff2": "font/woff2", ".json": "application/json", ".webmanifest": "application/manifest+json" };

function serve() {
  return new Promise((resolve) => {
    const server = http.createServer((req, res) => {
      const p = decodeURIComponent(req.url.split("?")[0]);
      const file = path.join(site, p);
      if (!file.startsWith(site) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
        res.writeHead(204); return res.end(); // htmx polls: "nothing yet"
      }
      res.writeHead(200, { "Content-Type": TYPES[path.extname(file)] || "application/octet-stream" });
      fs.createReadStream(file).pipe(res);
    });
    server.listen(0, "127.0.0.1", () => resolve(server));
  });
}

(async () => {
  const manifest = JSON.parse(fs.readFileSync(path.join(site, "manifest.json"), "utf8"))
    .filter((s) => !only.length || only.includes(s.name));
  fs.mkdirSync(out, { recursive: true });
  const server = await serve();
  const base = "http://127.0.0.1:" + server.address().port;
  const browser = await chromium.launch();
  const report = [];
  for (const scheme of schemes) {
    for (const vp of viewports) {
      const [width, height] = VIEWPORTS[vp];
      const context = await browser.newContext({ viewport: { width, height }, colorScheme: scheme,
        deviceScaleFactor: vp === "desktop" ? 1 : 2, hasTouch: vp !== "desktop", isMobile: vp === "phone" });
      for (const s of manifest) {
        const page = await context.newPage();
        if (s.storage) await page.addInitScript((st) => { for (const [k, v] of Object.entries(st)) localStorage.setItem(k, v); }, s.storage);
        const errors = [];
        page.on("pageerror", (e) => errors.push(String(e)));
        page.on("console", (m) => { if (m.type() === "error" && !/204|status of 4|MIME type/.test(m.text())) errors.push(m.text()); });
        await page.goto(base + "/pages/" + s.page + ".html", { waitUntil: "load" });
        await page.evaluate(() => document.fonts.ready);
        if (s.set) {
          await page.evaluate((set) => {
            for (const [sel, text] of Object.entries(set)) document.querySelectorAll(sel).forEach((el) => { el.textContent = text; });
          }, s.set);
        }
        if (s.attr) await page.evaluate((a) => document.documentElement.setAttribute(a, ""), s.attr);
        if (s.dialog) {
          await page.evaluate((id) => {
            // Open directly: clicking the real opener would fire its htmx
            // request, which this static site cannot answer.
            const d = document.getElementById(id);
            if (d && !d.open) d.showModal();
            // A tap does not draw a focus ring; a scripted open would.
            if (document.activeElement) document.activeElement.blur();
          }, s.dialog);
        }
        await page.waitForTimeout(450);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        const file = `${s.name}-${vp}-${scheme}.png`;
        await page.screenshot({ path: path.join(out, file), fullPage: !s.dialog });
        report.push({ file, overflow, errors });
        if (overflow > 0) console.log("OVERFLOW", file, overflow + "px");
        if (errors.length) console.log("ERRORS", file, errors.join(" | "));
        await page.close();
      }
      await context.close();
    }
  }
  fs.writeFileSync(path.join(out, "report.json"), JSON.stringify(report, null, 2));

  // Contact sheet: every shot, phone beside desktop, first scheme only.
  const scheme = schemes[0];
  const cells = manifest.map((s) => {
    const imgs = ["phone", "desktop"].filter((v) => viewports.includes(v)).map((v) => {
      const f = `${s.name}-${v}-${scheme}.png`;
      const data = fs.readFileSync(path.join(out, f)).toString("base64");
      return `<div class="shot ${v}"><img src="data:image/png;base64,${data}"></div>`;
    }).join("");
    return `<figure><figcaption>${s.name}</figcaption><div class="pair">${imgs}</div></figure>`;
  }).join("");
  const sheet = `<!doctype html><style>
    body{margin:0;padding:24px;background:#e5e7eb;font:600 15px system-ui;display:grid;grid-template-columns:repeat(3,1fr);gap:24px}
    figure{margin:0;background:#fff;border-radius:10px;padding:12px}
    figcaption{margin-bottom:8px;color:#111}
    .pair{display:flex;gap:10px;align-items:flex-start}
    .shot{height:420px;overflow:hidden;border:1px solid #d1d5db;border-radius:6px;background:#fff}
    .shot img{display:block;width:100%}
    .phone{flex:0 0 194px}.desktop{flex:1}
  </style>${cells}`;
  const ctx = await browser.newContext({ viewport: { width: 2400, height: 1000 } });
  const sp = await ctx.newPage();
  await sp.setContent(sheet, { waitUntil: "load" });
  await sp.screenshot({ path: path.join(out, "contact-sheet.png"), fullPage: true });
  await browser.close();
  server.close();
  console.log(`${report.length} shots in ${out}`);
})().catch((e) => { console.error(e); process.exit(1); });
