# EMBEDDED WEB UI

Generated: 2026-06-06

## OVERVIEW

The UI is embedded into the Go binary and served by Fiber. It uses Vue 3 plain JS modules, Fomantic UI, jQuery modals/toasts, Axios, and DataTables from CDNs.

## STRUCTURE

```text
views/
|-- index.html          # Imports all components, creates Vue app, manages selected device
|-- assets/app.css      # Local CSS
|-- assets/gowa.svg     # Logo
`-- components/         # One plain JS module per card/modal
    `-- generic/        # Reused form pieces
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add card/modal | `components/<Name>.js`, `index.html` | Import, register component, and place tag in the right grid. |
| Shared recipient fields | `components/generic/FormRecipient.js` | Used by send/message/group components. |
| Send forms | `components/Send*.js` | Text/link use JSON payloads; media forms append fields to `FormData`. |
| Reply Message ID UI | `SendMessage.js` and media/link send components | Keep optional `reply_message_id` omitted when blank. |
| Device selector | `components/DeviceManager.js`, `index.html` | Sets `X-Device-Id` and websocket query param. |
| Chat UI | `components/ChatList.js`, `components/ChatMessages.js` | Handles null/empty API result cases. |

## CONVENTIONS

- Components export default Vue option objects from plain `.js` files.
- Templates are inline backtick strings in component modules.
- Vue delimiters are `[[` and `]]` to avoid conflict with Go templates.
- API calls go through `window.http`, whose base URL includes `AppBasePath`.
- The selected device is sent as encoded `X-Device-Id`; websockets use `?device_id=`.
- Use `showSuccessInfo` and `showErrorInfo` for user feedback.
- Keep Fomantic UI modal IDs unique across components.

## ANTI-PATTERNS

- Do not add `.vue` single-file components or a frontend build step unless the project explicitly changes direction.
- Do not call `axios` directly if the request needs base path, auth, or device headers; use `window.http`.
- Do not add components to `components/` without importing/registering them in `index.html`.
- Do not assume there is always a selected or logged-in device.
