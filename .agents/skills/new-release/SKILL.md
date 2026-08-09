---
name: new-release
description: Use when publishing a new stable GitHub release for this project after updating its hardcoded AppVersion.
---

# New Release

Release only an already-prepared, tested `main` commit. This skill never edits source, release notes, tags, or releases except for the single confirmed tag push.

## Argument contract

Accept exactly one argument: a stable version matching `^v[0-9]+\.[0-9]+\.[0-9]+$` (for example, `$new-release v9.1.0`). Reject missing, extra, malformed, whitespace-padded, or prerelease/build-metadata versions. On rejection, stop before preflight and require a new invocation in the literal form `$new-release v9.1.0`; do not request an action token or a bare version. Do not normalize input.

## Safety rules

- Never edit `AppVersion`, release notes, or any repository file. Stop and tell the user to prepare those changes separately.
- Never run `gh release create`.
- Never force, move, overwrite, reuse, or delete tags or releases.
- Treat only the documented absence statuses as availability. Stop on every command, network, authentication, authorization, parsing, or API error.

## Preflight

1. Resolve the repository root with `git rev-parse --show-toplevel` and run every command from it. Read fetch URLs with `git remote get-url --all origin` and push URLs with `git remote get-url --push --all origin`. Require each command to succeed and return exactly one nonempty URL; call them `fetch_url` and `push_url`. Normalize each with `gh repo view <url> --json nameWithOwner --jq .nameWithOwner` and require both to equal `aldinokemal/go-whatsapp-web-multidevice`; require `gh auth status` to succeed.
2. Run `git fetch origin main --tags --prune`. Require branch `main`, an empty `git status --porcelain=v1`, and `git rev-parse HEAD` exactly equal to `git rev-parse origin/main`.
3. In `src/config/settings.go`, find exactly one declaration matching `^\s*AppVersion\s*=`. Extract its quoted value and require exact equality with the requested version; otherwise stop without editing it.
4. Check the exact local ref `refs/tags/<version>` with `git show-ref --verify --quiet`. Continue only on its expected absent exit status; stop if present or if any other error occurs. Check the exact remote ref on `push_url` with `git ls-remote --exit-code --refs --tags <push_url> refs/tags/<version>`; continue only on exit status `2` (absent), stop on `0` (present) or any other result.
5. Perform an exact GitHub Release lookup with `gh api -i repos/aldinokemal/go-whatsapp-web-multidevice/releases/tags/<version>`. Continue only after an explicit HTTP `404` not-found response. Stop if a release is returned or the response is any other API, network, authentication, authorization, or permission error.
6. Run `go test ./...` from `src/` and require success. Return to the repository root. Record `validated_sha=$(git rev-parse HEAD)` and `subject=$(git log -1 --format=%s "$validated_sha")`.
7. Show the exact repository, requested version, detected `AppVersion`, full validated SHA, and subject. Output this confirmation prompt verbatim, replacing only placeholders: `Push tag <version> at <validated_sha>? [yes/no]` Do not paraphrase it or add signing, release-creation, or other actions.

## Confirmation and tag push

Stop without mutation unless the user replies `yes` affirmatively. Immediately before any mutation, repeat the fetch/push URL collection, exactly-one-nonempty-URL, and repository-identity checks above, then run `git fetch origin main --tags --prune` to refresh remote state. Require branch `main`, empty porcelain status, `HEAD`, freshly read `origin/main`, and the validated SHA all equal `validated_sha`; recheck the exact local and remote tag absence using the same expected-status rules above, and recheck that the exact GitHub Release lookup returns explicit HTTP `404`.

Only then run `git tag <version> <validated_sha>` followed by `git push origin refs/tags/<version>:refs/tags/<version>`. Do not add force flags or push any other ref. If tag creation fails, stop and report it. If the push fails, do not retry, delete, force, or clean up: read back the exact local ref and the exact remote ref on the sole validated `push_url` using the same checks above, and report each as `present`, `absent`, or `unknown` when its read-back has an operational error; then stop.

## Workflow verification

For no more than two minutes after the push, poll `gh run list --workflow release.yml --branch <version> --json databaseId,event,headSha,status,conclusion,url --limit 20`. Select only a run whose `event` is `push` and whose `headSha` equals `validated_sha`; show each observed status/progress. Stop and report failure if no matching push run appears before the deadline.

Run `gh run watch <run-id> --exit-status`. Require success and report its terminal state and URL. Then run `gh release view <version> --json tagName,isDraft,isPrerelease,url,publishedAt,targetCommitish`; require exact `tagName`, `isDraft=false`, and `isPrerelease=false`, then report the published release URL. Stop and report any mismatch or lookup error.

## Failure reporting

On every stop, state the failed check, the observed safe evidence, and that no further mutation was performed. After a push failure, include the exact local and remote tag read-back states, preserving `unknown` for any read-back error. Do not suggest changing version state, creating a release directly, reusing an existing tag, or deleting a failed tag. After a pushed tag, report the failed workflow or release evidence and ask the user for direction.
