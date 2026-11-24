const { chromium } = require('playwright'); const { markSVG } = require('./glyph');
(async()=>{const b=await chromium.launch();const p=await b.newPage({viewport:{width:700,height:300}});
await p.setContent(`<body style="margin:0;background:#ddd">${[256,64,32,24].map(s=>`<img width=${s} src="data:image/svg+xml;utf8,${encodeURIComponent(markSVG())}">`).join('')}</body>`);
await p.screenshot({path:process.argv[2]});await b.close()})();
