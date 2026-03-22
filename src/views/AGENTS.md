# views

Vue.js 3 frontend components using Semantic UI. Served as embedded HTML via Go's `embed` package.

## STRUCTURE
```
views/
├── components/     # 48 Vue.js components (one per feature)
│   ├── App*.js         # Login, logout, reconnect
│   ├── Account*.js     # Avatar, profile, privacy, contacts
│   ├── Chat*.js        # ChatList, ChatMessages, ChatPinManager, ChatDisappearingManager
│   ├── Device*.js      # DeviceManager
│   ├── Group*.js       # Group operations
│   ├── Newsletter*.js  # Newsletter operations
│   ├── Send*.js        # All message types (text, image, video, etc.)
│   └── Message*.js     # Reactions, deletion, starring
├── assets/         # CSS, vendor libs
└── index.html      # Main SPA entry (embedded)
```

## CONVENTIONS
- Components are plain JS objects (`export default { name, data, methods, template }`)
- API calls: `window.http.get/post(...)` (axios instance)
- Modals: Semantic UI `$('#modalName').modal('show')`
- Errors: `showErrorInfo(message)` global helper
- Global component refs: `window.XxxComponent = this` in `mounted()`
- Each component is self-contained with inline `template` string

## ANTI-PATTERNS
- Never use SFC (`.vue` files) — this project uses plain JS components
- Never import from node_modules — all deps are loaded via CDN/vendor
