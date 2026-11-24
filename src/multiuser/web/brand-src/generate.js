// Regenerates every file in ../static/brand from glyph.js.
//   NODE_PATH=$(npm root -g) node generate.js      (needs playwright + chromium)
const fs = require('fs'), path = require('path');
const { chromium } = require('playwright');
const { markSVG } = require('./glyph');
const out = path.join(__dirname, '..', 'static', 'brand');

async function png(browser, html, w, h, file) {
  const p = await browser.newPage({ viewport: { width: w, height: h } });
  await p.setContent(`<body style="margin:0;background:transparent">${html}</body>`);
  const buf = await p.screenshot({ omitBackground: true, type: 'png' });
  await p.close(); if (file) fs.writeFileSync(path.join(out, file), buf); return buf;
}
const img = (svg, s) => `<img width="${s}" height="${s}" style="display:block" src="data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}">`;

function ico(pngs) { // PNG-in-ICO
  const head = Buffer.alloc(6); head.writeUInt16LE(1, 2); head.writeUInt16LE(pngs.length, 4);
  let off = 6 + 16 * pngs.length; const dirs = [];
  for (const [size, b] of pngs) {
    const d = Buffer.alloc(16); d[0] = size; d[1] = size; d.writeUInt16LE(1, 4); d.writeUInt16LE(32, 6);
    d.writeUInt32LE(b.length, 8); d.writeUInt32LE(off, 12); off += b.length; dirs.push(d);
  }
  return Buffer.concat([head, ...dirs, ...pngs.map(p => p[1])]);
}

(async () => {
  fs.mkdirSync(out, { recursive: true });
  const mark = markSVG({ id: 'g' });
  fs.writeFileSync(path.join(out, 'mark.svg'), mark + '\n');
  fs.writeFileSync(path.join(out, 'favicon.svg'), mark + '\n');
  const b = await chromium.launch();
  const sizes = {};
  for (const s of [16, 32, 48]) sizes[s] = await png(b, img(mark, s), s, s, s === 32 ? 'favicon-32.png' : null);
  fs.writeFileSync(path.join(out, 'favicon.ico'), ico([[16, sizes[16]], [32, sizes[32]], [48, sizes[48]]]));
  // opaque, full-bleed square for iOS (it rounds the corners itself)
  const square = markSVG({ rounded: false, id: 'a' });
  await png(b, `<div style="background:#047857">${img(square, 180)}</div>`, 180, 180, 'apple-touch-icon.png');
  await png(b, img(mark, 192), 192, 192, 'icon-192.png');
  await png(b, img(mark, 512), 512, 512, 'icon-512.png');
  // maskable: full-bleed, glyph inside the 80% safe zone
  await png(b, img(markSVG({ rounded: false, glyphScale: 0.7, id: 'm' }), 512), 512, 512, 'icon-maskable-512.png');
  // social card
  const card = `<div style="width:1200px;height:630px;background:linear-gradient(135deg,#0B1220,#064E3B);display:flex;align-items:center;justify-content:center">${img(mark, 360)}</div>`;
  await png(b, card, 1200, 630, 'og-image.png');
  await b.close();
})();
