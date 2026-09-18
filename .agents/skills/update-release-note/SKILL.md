---
name: update-release-note
description: Update an existing GitHub release's notes when asked, preserving the style of recent releases.
---

# Update release notes

Resolve the requested repository and release tag. Read its current body and the
previous release with `gh release view <tag> --json name,body,url`; release-list
JSON does not include `body`. Inspect commits/PRs between the relevant tags to
support each change listed, while retaining existing accurate content and links.

Match the established language, headings, and contributor/changelog format. Write
the complete intended Markdown to a temporary file and use `gh release edit <tag>
--notes-file <path>` for the authorized update. Read the release back to verify the
stored body, then return its URL. Creating a release or changing version/tag metadata
requires a separate request.
