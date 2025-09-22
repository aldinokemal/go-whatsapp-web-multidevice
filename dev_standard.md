Code & Docs: What “Good” Looks Like

I’m not here to pull rank—I’m here to help us move faster. If someone can’t skim a module and explain it, we slow down. New joiners should be productive within 24 hours. "Code should be optimized for the reader, not the writer."

Baseline

Readable at a glance. Names > comments. Small functions > long ones. Clear flow > clever tricks.

Comment the “why,” not the “what.” Aim for 1–2 intent comments per file/section. If you need more than ~5, consider splitting or refactoring (truly complex math/algorithms are the exception).

Document as you go. Dependencies, setup steps, and decisions live with the code—not at the end.

Treat docs like code. Short README + how-tos; API docs from docstrings/JSDoc; changelog + meaningful commits; ADRs for major decisions.


Good vs Bad (quick examples)

1) Commit messages

Bad

update stuff
fix things
misc changes

Good

feat(auth): add refresh-token flow; rotates every 15m (#123)
fix(cache): prevent stale reads after bulk import
docs(api): clarify pagination semantics in /v2/orders

2) Self-explanatory code vs narrated code

Bad

// get data
d = g(u)
// filter invalid
d2 = f(d)
// sort desc
s = sort(d2, "ts", desc=true)
// print
p(s)

Good

rawEvents = fetchUserEvents(userId)
validEvents = discardInvalid(rawEvents)
recentEvents = sortByTimestampDesc(validEvents)
printSummary(recentEvents)

A single top-level comment is fine if it explains why this flow exists.

3) Minimal, useful comments

Bad

// add one
i = i + 1
// check if bigger
if i > max { ... }

Good

// Safety: throttle retries to avoid API rate limits.
retryCount = min(retryCount + 1, MAX_RETRIES)

4) Function/API docs (for tools to scrape)

Bad

function run(a, b) { ... } // does things

Good

/**
 * runJob executes the pipeline from fetch → transform → upload.
 * @param sourceUrl  HTTP/HTTPS endpoint to fetch from.
 * @param dryRun     When true, skips upload.
 * @returns          Summary with counts + duration.
 */
function runJob(sourceUrl, dryRun=false) { ... }

(Keep names clear so readers barely need this. The doc is for generated API ref.)

5) Dependencies & setup

Bad

Just run it.

Good (README excerpt)

Dependencies
- Runtime: <version>
- Package manager: <name & version>
- Services: <DB name> (local or URL)
Setup
- <install cmd>
- <env vars required> (with sane defaults)
- <how to run tests>

6) ADR (Architecture Decision Record)

Bad

We chose Service A.

Good

ADR-012: Choose Service A for metrics
Context: We need 90d retention + <feature>.
Decision: Service A over B due to native histograms & <reason>.
Consequences: + simpler ops; – higher cost. Revisit if volume > X.

7) Changelog entry

Bad

v1.4 – updates

Good

[1.4.0] – 2025-08-30
Added: CSV export in reports.
Changed: Auth tokens now 15m TTL (see ADR-009).
Fixed: Race in cache warmup on cold start.

Lightweight team practice

PR template includes: “docs updated,” “changelog entry,” “ADR link if decision.”

Keep README quickstart accurate.

Autogenerate API docs from docstrings/JSDoc.

Use clear, action-based commit messages (Conventional-Commits style is ideal).

8) Formatting & linting (automate it, don’t debate it)

Use a formatter everywhere. One config per repo, committed: .clang-format, .prettierrc, pyproject.toml (Black), rustfmt.toml, etc. Add .editorconfig so editors agree.

Run it automatically.

Local: enable “format on save” + pre-commit hooks.

CI: fail fast on diffs (no human nitpicks).


Keep presets, minimize tweaks. Start with the tool’s default style; only change what’s necessary.

Linters complement formatters. Keep rules small and clear. Examples: clang-tidy, ESLint, Ruff/Flake8, Pylint, golangci-lint, SwiftLint, ktlint.

Legacy code policy: “format-on-touch” (only the lines/files you modify) to avoid noisy diffs.


Good vs Bad

Bad

PR review: “Use spaces here.” “Rename this per style.” (Manual nitpicks)
Mixed styles in one file.
CI warns but doesn’t block on format issues.

Good

pre-commit: clang-format / Black / Prettier runs before commit
CI: “format check” + “lint” steps; PR fails on style diff
Scripts: make fmt && make lint (or npm run fmt && npm run lint)
Editor: format on save

Common picks by ecosystem

C/C++: clang-format (+ clang-tidy)

JS/TS: Prettier (+ ESLint)

Python: Black (+ isort, Ruff/Flake8)

Go: go fmt (+ golangci-lint)

Rust: rustfmt (+ Clippy)

Kotlin/Android: ktlint / Ktlint Gradle

.NET/C#: dotnet format (+ analyzers)
A special instruction for nginx users

Do not use if statements in nginx at all unless the use case legitimately cannot be solved without these statements

@HackWrld04 you use this wrapper i wrote

https://github.com/GlassROM/linux-hardened/blob/main/dockersafebuild

Uncomment line 2 if you face DNS issues

dockersafebuild -t TAG

Where TAG is any relatable name that is easy for you to type

In docker-compose.yml add

pull_policy: never 

And set image to

image: TAG

In production you do

pull_policy: always

And image should point to your production CI/CD pipeline

