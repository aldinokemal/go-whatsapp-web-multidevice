---
name: new-release
description: Prepare and publish a stable GitHub release for this project by updating the hardcoded AppVersion, testing, committing, pushing main, creating the matching tag, and verifying the release. Use only for an explicit $new-release vX.Y.Z invocation.
---

# New Release

Publish a stable release from a clean, current `main` checkout. After the sole affirmative confirmation, update only `AppVersion`, test, commit, push `main`, push the tag, and verify the GitHub release. Do not create release notes or run `gh release create`.

## Argument contract

Accept exactly one argument: a stable version matching `^v[0-9]+\.[0-9]+\.[0-9]+$` (for example, `$new-release v9.1.0`). Reject missing, extra, malformed, whitespace-padded, or prerelease/build-metadata versions. On rejection, stop before preflight and require a new invocation in the literal form `$new-release v9.1.0`; do not request an action token or a bare version. Do not normalize input.

## Safety rules

- Run every Git and GitHub command from the repository root returned by `git rev-parse --show-toplevel`.
- Never force, move, overwrite, reuse, or delete a branch, tag, or release.
- Make no mutation before the user replies exactly `yes`.
- After confirmation, edit only `src/config/settings.go`; commit only that file.
- Treat only the documented absence statuses as availability. Stop on every command, network, authentication, authorization, parsing, or API error.

## Preflight

1. Read fetch URLs with `git remote get-url --all origin` and push URLs with `git remote get-url --push --all origin`. Require each command to succeed and return exactly one nonempty URL; call them `fetch_url` and `push_url`. Normalize each with `gh repo view <url> --json nameWithOwner --jq .nameWithOwner` and require both to equal `aldinokemal/go-whatsapp-web-multidevice`. Require `gh auth status` to succeed.
2. Run `git fetch origin main --tags --prune`. Require branch `main`, empty `git status --porcelain=v1`, and `git rev-parse HEAD` exactly equal to `git rev-parse origin/main`. Record that SHA as `base_sha` and its subject.
3. In `src/config/settings.go`, find exactly one declaration matching `^\s*AppVersion\s*=`. Extract its quoted value as `current_version`. Require it to be a stable version and not equal the requested version. Do not edit it yet.
4. Check exact local ref `refs/tags/<version>` with `git show-ref --verify --quiet`. Continue only on expected absent exit status; stop if present or any other error. Check exact remote ref with `git ls-remote --exit-code --refs --tags <push_url> refs/tags/<version>`; continue only on exit status `2` (absent), stop on `0` (present) or any other result.
5. Run `gh api -i repos/aldinokemal/go-whatsapp-web-multidevice/releases/tags/<version>`. Continue only after an explicit HTTP `404` not-found response. Stop if a release is returned or any other API, network, authentication, authorization, or permission error occurs.
6. Run `go test ./...` from `src/`, require success, then return to the repository root.
7. Show exact repository, requested version, detected `current_version`, full `base_sha`, and subject. Output this confirmation prompt verbatim, replacing only placeholders: `Update AppVersion to <version>, commit, push main, and tag the release? [yes/no]`

## Confirmation, version commit, and tag push

Stop without mutation unless the user replies exactly `yes`.

Immediately before mutation, repeat preflight steps 1, 2, 4, and 5. Require the freshly read `HEAD` and `origin/main` both equal `base_sha`, clean porcelain status, and unchanged tag/release absence. Re-read `AppVersion` and require it still equals `current_version`.

Then perform this sequence:

1. Change the single `AppVersion` declaration in `src/config/settings.go` to the requested version. Do not alter whitespace or any other file.
2. Require `git diff --check` to succeed. Require `git diff --name-only` to output only `src/config/settings.go`, and require the diff to replace exactly the `AppVersion` quoted value from `current_version` to the requested version.
3. Run `go test ./...` from `src/` and require success. Return to the repository root.
4. Stage exactly `src/config/settings.go`. Require `git diff --cached --name-only` to output only that path. Create exactly one commit with `git commit -m "chore(release): <version>"`. Record `release_sha=$(git rev-parse HEAD)` and require it differs from `base_sha`.
5. Push only the release commit to main using `git push origin HEAD:refs/heads/main`. If the push fails, stop without retrying, forcing, resetting, or cleaning up. Read `git rev-parse HEAD`, `git rev-parse origin/main`, and `git status --porcelain=v1`; report the evidence and ask for direction.
6. Run `git fetch origin main --tags --prune`; require local `HEAD`, freshly read `origin/main`, and `release_sha` all equal. Recheck exact local and remote tag absence and exact release HTTP `404` as above.
7. Run `git tag <version> <release_sha>` followed by `git push origin refs/tags/<version>:refs/tags/<version>`. Do not add force flags or push any other ref. If tag creation fails, stop and report it. If tag push fails, do not retry, delete, force, or clean up. Read back the exact local and remote tag refs and report each as `present`, `absent`, or `unknown` when its read-back has an operational error; then stop.

## Workflow verification

For no more than two minutes after the tag push, poll `gh run list --workflow release.yml --branch <version> --json databaseId,event,headSha,status,conclusion,url --limit 20`. Select only a run whose `event` is `push` and whose `headSha` equals `release_sha`; show each observed status/progress. Stop and report failure if no matching push run appears before the deadline.

Run `gh run watch <run-id> --exit-status`. Require success and report terminal state and URL. Then run `gh release view <version> --json tagName,isDraft,isPrerelease,url,publishedAt,targetCommitish`; require exact `tagName`, `isDraft=false`, `isPrerelease=false`, and `targetCommitish` equal to `release_sha`; then report the published release URL. Stop and report any mismatch or lookup error.

## Failure reporting

On every stop, state the failed check, observed safe evidence, and whether a mutation has occurred. Before confirmation, report that no mutation was performed. After the version commit or `main` push, report local and remote SHA/status evidence and ask for direction; never repair state autonomously. After a pushed tag, report the failed workflow or release evidence and ask the user for direction.
