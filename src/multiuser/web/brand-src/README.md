# Brand assets

`glyph.js` holds the mark geometry (512 grid). `generate.js` rewrites everything in
`../static/brand/` (SVGs, favicon.ico, PNG icons, maskable icon, og-image):

    NODE_PATH=$(npm root -g) node generate.js

Needs `playwright` with its Chromium. Not embedded in the binary; do not commit node_modules.
