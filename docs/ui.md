# Web UI design system

The multi-user web UI (`src/multiuser/web/`) is server-rendered Go
`html/template` plus htmx and a small vanilla `app.js`. Everything is vendored
and the CSP stays strict (`script-src 'self'`, `style-src 'self'`): no inline
`style=` attributes, no inline `<script>`, nothing from a CDN.

## Files

| Path | What it holds |
|---|---|
| `static/app.css` | The one stylesheet: tokens, base, shell, components, pages. |
| `static/app.js` | Passkeys, dialogs (open, focus return, backdrop close), drawer and rail, copy, busy buttons, QR refresh. |
| `static/boot.js` | Runs in `<head>` before paint: restores the folded desktop menu (`localStorage` key `wtf-nav`). |
| `static/fonts/` | Inter variable (latin, `@fontsource-variable/inter` 5.3.0, SIL OFL, licence alongside). |
| `static/brand/` | Logo mark, favicon, manifest (owned by the brand work; the `<head>` has a marked slot for its tags). |
| `icons/` | Lucide icons (`lucide-static` 1.78.0, ISC licence alongside), inner SVG only. |
| `templates/layout.html` | `head`/`foot`, the signed-in shell (`app_start`/`app_end`), the signed-out card (`auth_start`/`auth_end`), `brand`, `nav_link`, `confirm` and `sheet` dialogs. |

## Tokens

All defined once on `:root` in `app.css`. Colours use `light-dark(light, dark)`,
so dark mode follows `prefers-color-scheme` with no second block.

- **Surfaces:** `--bg`, `--surface`, `--surface-2`, `--surface-hover`, `--line`, `--line-strong`.
- **Text:** `--ink` (primary), `--ink-2` (secondary), `--ink-3` (muted, still AA), `--ink-on-accent`.
- **Brand (emerald):** `--brand` `#0E9F6E` (decorative only), `--accent` (button fill:
  `#047857` light, `#10B981` dark), `--accent-hover`, `--accent-ink` (emerald text on a surface),
  `--accent-soft`, `--accent-line`.
- **Status:** `--ok-*`, `--warn-*`, `--danger*`, `--muted-soft`. One meaning each:
  ok = Connected / Active, warn = Not connected, muted = Not linked / Disabled, danger = destructive.
- **Type:** Inter, `--text-xs` 12 to `--text-3xl` 28 (`--text-code` 32 for pairing codes);
  weights 400 / 500 / 600 only.
- **Space:** a 4px grid, `--s-1` (4) to `--s-16` (64).
- **Shape and depth:** `--radius-sm` 6 to `--radius-xl` 16; `--shadow-sm`, `--shadow`, `--shadow-lg`, `--shadow-btn`.
- **Motion:** `--dur-fast` 120ms, `--dur` 180ms, `--dur-slow` 260ms, one `--ease`. Everything
  collapses under `prefers-reduced-motion`.

Contrast: every text pair the UI paints passes WCAG AA 4.5:1 in both themes
(white on `#047857` is 5.5:1; checked with `ui-polish/scripts/contrast_audit.py`).

## Theme

Dark mode follows the system unless someone picks **System / Light / Dark**
(the switcher in the menu, above Sign out, and Account › Appearance). The
choice is stored per browser in `localStorage` (`wtf-theme`). `boot.js` applies
it before paint as `data-theme="light|dark"` on `<html>`, which sets
`color-scheme`, so every `light-dark()` token follows; it also pins both
`theme-color` metas. Browsers without `light-dark()` get the light values from an
`@supports not` block.

The switcher is one partial (`theme_switcher`) drawn as a row of buttons, or as
one `<select>` below 600px (house rule B4).

## Shell

- Above 960px: a 248px sidebar (brand, nav with icons, user and Sign out at the
  bottom). The panel button folds it to a 72px icon rail with tooltips; the
  choice is remembered.
- 960px and below: a sticky top bar (menu button, brand) and a slide-over drawer
  with a dimmed backdrop. Esc, the backdrop, the close button or choosing a link
  closes it; focus moves in on open and back on close.
- The current entry carries `aria-current="page"` (tint plus an accent bar).
- Signed-out pages (sign-in, verify, errors) use the centred auth card. On a wide
  screen with a pointer, sign-in splits and shows a large QR on an emerald panel;
  a phone never sees the QR (it cannot scan its own screen).

## Components

| Class | Use |
|---|---|
| `.page-head` (`.page-title`, `.page-sub`, `.page-actions`) | Every signed-in page; the main action sits top-right. |
| `.card`, `.card-head`, `.card-title`, `.card-sub` | Sections. |
| `.rows` > `.row` (`.row-icon`, `.row-main`, `.row-title`, `.row-sub`, `.pill`, `.row-actions`) | Boxed lists; on a phone the actions drop under the text column. |
| `.table` > `.trow` | The admin user list: a grid on desktop, stacked rows below 860px. |
| `.pill` + `-ok` / `-warn` / `-brand` / (muted default) | State, derived at render time. |
| `.btn` + `-primary` / `-secondary` / `-ghost` / `-danger`, `-sm` / `-lg` / `-block`; `.icon-btn` | Buttons; 44px tall on touch screens. A submitted form's button shows busy at once. |
| `.empty` (`.empty-art`, `.empty-title`, `.empty-text`) | Empty states, with the action inside. |
| `.stats` > `.stat` | Stat tiles. |
| `.field`, `.label`, `.input`, `.select-wrap` | Form fields; 16px text on touch screens. |
| `.form-error`, `.notice` | Errors (hidden while empty, so JS can fill them) and neutral notices. |
| `{{template "confirm" (dict ...)}}` | Every destructive confirmation: native `<dialog>`, Cancel focused first. |
| `{{template "sheet" (dict "Title" ...)}}` | The htmx-filled content dialog (`#link-dialog-body`); shows a spinner while empty. |
| `{{template "menu_start" "More actions"}}` … `{{template "menu_end"}}` | A row's kebab (⋯) menu: items are `.menu-item` buttons or links, destructive ones last with `.menu-item-danger`. Positioned by app.js so a clipped card never cuts it off. |
| `.disclosure` + `data-disclosure` + `aria-controls` | A count with a chevron that opens a child row (`.trow-child`), e.g. a user's numbers on Admin › Users. |
| `{{template "number_state" (dict "N" numberView "Whose" "your")}}` | A WhatsApp number's icon, words and pill (connected, not connected, signed out from the phone, refused, not linked yet), shared by My WhatsApp and Admin › Users. |
| `{{template "form_dialog" (dict ...)}}` | One small form dialog shared by every row: the opener's `data-dialog-action`, `data-dialog-value` and `data-dialog-subject` fill it. `confirm` with an empty `Action` works the same way, and takes an optional ticked `Check`/`CheckName`. |
| `data-dialog-title` on a `sheet` opener | Sets the shared sheet's title (Link WhatsApp, Webhook, Sign-in link). |

| `.notice`, `.banner-danger` | Neutral notices (device limit) and blocking problems (sign-in is down). |
| `.nudge` (`data-nudge`, `data-dismiss`) | A dismissible suggestion card (add a passkey); dismissal is remembered per browser. |
| `.row-flash` | In-row feedback (Reconnecting…, Reconnected., Couldn't reconnect). |

Phone numbers are shown with the `{{phone}}` template func: `+`, country code,
then digit groups (`+27 82 555 0100`); inputs and links keep the raw value.
One word per concept: a linked WhatsApp **number**.

The link dialog opens on the QR code on a computer and on the pairing code on a
phone (it cannot scan its own screen); `data-link-mode` buttons switch. A code
ends after two minutes, or when its QR keeps failing, and offers "Get a new
code"; polling stops then, on success, and when the dialog closes.

Dialogs are native `<dialog>` opened with `data-open-dialog="<id>"` and closed with
`data-close-dialog`, Esc or a backdrop click; focus returns to the opener. Below
600px they are bottom sheets.

## Icons

`{{icon "smartphone"}}` renders `icons/smartphone.svg` inline (stroke uses
`currentColor`, `aria-hidden`). An unknown name fails the render, so tests catch
typos. To add one, copy the inner elements of a Lucide SVG (from
`npm pack lucide-static`) into `icons/<name>.svg`. Never emoji. Icon-only
buttons carry an `aria-label`.

## Preview harness and screenshots

`preview_test.go` renders every page and state (sign-in and its error and
not-set-up states, verify valid and expired, My WhatsApp with devices and empty,
every link-dialog state, account with and without passkeys and its dialogs, admin
users and its dialogs, settings with and without numbers, 404 and 500, the
drawer and the rail, the theme choices, Add user, reconnect, sign-in down, the
offline page) with fixture data into a static site plus `manifest.json`.
It is skipped unless `WTF_PREVIEW_DIR` is set.

```sh
cd src
WTF_PREVIEW_DIR=../.preview/site go test ./multiuser/web/ -run TestPreviewSite -count=1
node multiuser/web/preview/shoot.js ../.preview/site ../.preview/shots
# options: --viewports phone,tablet,desktop  --schemes light,dark  --only home,signin
# a shot may also list "click" selectors (a kebab, then an item) and "viewportOnly".
```

`shoot.js` needs Playwright (`require('playwright')`). It writes
`<shot>-<phone|tablet|desktop>-<light|dark>.png` (each shot starts with clean
`localStorage`, plus any `storage` the manifest sets, e.g. a chosen theme), a `report.json` (horizontal
overflow and console errors per shot) and `contact-sheet.png`. `.preview/` is
gitignored.
