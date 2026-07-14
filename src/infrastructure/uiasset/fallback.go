package uiasset

import (
	"fmt"
)

// FallbackHTML is served at "/" until a dashboard download succeeds.
func FallbackHTML(version, repo string) []byte {
	return fmt.Appendf(nil, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="refresh" content="30">
<title>gowa %s</title>
<style>body{font-family:system-ui,sans-serif;display:flex;min-height:100vh;align-items:center;justify-content:center;background:#0a0a0a;color:#e5e5e5}main{max-width:34rem;padding:2rem;text-align:center}code{background:#262626;padding:.15rem .4rem;border-radius:.25rem}</style>
</head>
<body>
<main>
<h1>gowa %s</h1>
<p>The API is running, but the dashboard has not been downloaded yet.</p>
<p>gowa fetches the UI from the latest <code>%s</code> release. If this server
has no internet access, pre-seed <code>storages/ui/index.html</code> and set
<code>APP_UI_AUTO_UPDATE=false</code>, or disable the UI with
<code>APP_UI_ENABLED=false</code>.</p>
<p>This page refreshes automatically.</p>
</main>
</body>
</html>`, version, version, repo)
}
