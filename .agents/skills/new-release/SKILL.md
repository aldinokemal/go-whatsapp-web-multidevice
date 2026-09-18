---
name: new-release
description: Publish a stable GOWA release. Use only for an explicit $new-release vX.Y.Z invocation.
---

# New release

Publish from a clean, current `main` checkout of
`aldinokemal/go-whatsapp-web-multidevice`: update only `AppVersion`, validate, commit,
push `main`, push the matching tag, and verify the release created by `release.yml`.
Release-note editing is a separate task; do not run `gh release create`.

## Input and boundaries

- Accept exactly one stable version matching `^v[0-9]+\.[0-9]+\.[0-9]+$`.
  Reject missing/extra arguments, surrounding whitespace, prereleases, and build
  metadata without normalizing them. Ask for a corrected `$new-release vX.Y.Z` invocation.
- Keep the explicit release gate: no source edits, commits, or pushes before the
  user replies exactly `yes` to the confirmation below. A confirmation already
  received for this same checked release remains valid; do not ask again.
- Run Git/GitHub commands from `git rev-parse --show-toplevel`; run Go from `src/`.
  Never force, move, overwrite, reuse, or delete branches, tags, or releases.
- Authentication, permission, network, or parsing failures do not prove absence.
  Stop on failed checks; do not repair repository state or retry failed publishing mutations.

## Preflight

1. Read all `origin` fetch and push URLs with `git remote get-url --all origin` and
   `git remote get-url --push --all origin`. Each must return exactly one nonempty URL.
   Resolve each with `gh repo view <url> --json nameWithOwner --jq .nameWithOwner`;
   both must equal the repository above. Require successful `gh auth status`.
2. Fetch `origin main --tags --prune`. Require branch `main`, clean
   `git status --porcelain=v1`, and `HEAD == origin/main`. Record the full `base_sha`
   and subject. In `src/config/settings.go`, require exactly one `AppVersion`
   declaration with a quoted stable `current_version` different from the requested version.
3. Establish tag/release absence: local `git show-ref --verify --quiet refs/tags/<version>`
   must exit `1`; remote `git ls-remote --exit-code --refs --tags <push_url> refs/tags/<version>`
   must exit `2`; `gh api -i repos/aldinokemal/go-whatsapp-web-multidevice/releases/tags/<version>`
   must return explicit HTTP `404`. A present ref/release or any other error stops the workflow.
4. Show the repository, requested/current versions, full `base_sha`, and subject. Ask:
   `Update AppVersion to <version>, commit, push main, and tag the release? [yes/no]`

## Publish after confirmation

Recheck preflight facts immediately before editing: URLs/repository/auth, clean
`main`, fresh `origin/main == HEAD == base_sha`, unchanged `current_version`, and
exact tag/release absence. If any differs, stop and report the changed fact.

1. Replace only the quoted `AppVersion` value. Require `git diff --check` and verify
   that the entire diff is that one replacement in `src/config/settings.go`.
2. Run `go test ./...` from `src/` and require success. This validates the release
   candidate; do not also require an unchanged baseline suite before confirmation.
3. Stage only `src/config/settings.go`; verify the staged diff is still exactly
   that version replacement. Commit `chore(release): <version>` and record the full
   `release_sha`, which must differ from `base_sha`.
4. Push `origin HEAD:refs/heads/main`, fetch `origin main --tags --prune`, and require
   `HEAD == origin/main == release_sha`. Recheck local/remote tag and release absence.
5. Create `git tag <version> <release_sha>` and push only
   `origin refs/tags/<version>:refs/tags/<version>`.

## Completion and failure

After the tag push, allow up to two minutes for `gh run list --workflow release.yml
--branch <version> --json databaseId,event,headSha,status,conclusion,url --limit 20`
to show a `push` run with `headSha == release_sha`. This deadline is for run
appearance, not build completion. Follow the matching run to a terminal result
and require success; report its URL and any failed jobs.

Read `gh release view <version> --json tagName,isDraft,isPrerelease,url,publishedAt,targetCommitish`.
Require the exact tag, a published non-draft/non-prerelease release, and
`targetCommitish == release_sha` before reporting the release URL as complete.

On failure, report the failed check and completed mutations. After a commit/main
push, include local and freshly read remote SHA/status evidence. After a tag-push
failure, report exact local/remote refs as present, absent, or unknown on read errors.
Do not retry, reset, delete, or clean up failed publishing state; ask for direction.
