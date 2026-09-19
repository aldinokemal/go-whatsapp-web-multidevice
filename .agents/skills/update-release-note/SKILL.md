---
name: update-release-note
description: Edit the notes of an existing GitHub release when requested.
---

# Update release notes

Resolve the requested repository and release tag; clarify only if the target is
ambiguous. Use the same explicit repository and tag for every read and write.
Read its current body and the previous release with
`gh release view <tag> --repo <owner/repo> --json name,body,url`; release-list
JSON does not include `body`. Inspect commits/PRs between the relevant tags to
support each change listed, while retaining existing accurate content and links.

Match the established language, headings, and contributor/changelog format. Write
the complete intended Markdown to a temporary file. If the current body already
matches, return its URL without rewriting it. For a requested update, use
`gh release edit <tag> --repo <owner/repo> --notes-file <path>` and read the release
back to compare the stored body with the intended Markdown before reporting success.
A request for a draft or review ends with the proposed notes; it does not authorize
publishing them.
Creating a release or changing version/tag metadata requires a separate request.
