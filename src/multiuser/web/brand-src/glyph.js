// Shared geometry for the WTF mark (512x512 design grid).
// GLYPH is the white chat bubble with an interlocked chain link cut out of it.
const GLYPH = `
  <path d="M168 104h176c48 0 88 40 88 88v88c0 48-40 88-88 88H236l-84 64c-8 6-20 0-20-10v-58c-44-12-76-50-76-94v-78c0-48 40-88 88-88z" fill="#fff"/>
  <g fill="none" stroke="#059669" stroke-width="28" stroke-linecap="round" stroke-linejoin="round">
    <rect x="122" y="190" width="158" height="92" rx="46"/>
    <rect x="232" y="190" width="158" height="92" rx="46"/>
  </g>`;
function markSVG({ glyphScale = 1, gradient = true, rounded = true, id = 'g' } = {}) {
  const s = glyphScale, t = 256 - 256 * s;
  const bg = rounded ? '<rect width="512" height="512" rx="112" fill="url(#' + id + ')"/>' : '<rect width="512" height="512" fill="url(#' + id + ')"/>';
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512" width="512" height="512">
  <defs><linearGradient id="${id}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#10B981"/><stop offset="1" stop-color="#047857"/></linearGradient></defs>
  ${bg}
  <g transform="translate(${t} ${t}) scale(${s})">${GLYPH}</g>
</svg>`;
}
module.exports = { markSVG, GLYPH };
