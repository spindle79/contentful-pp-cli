---
name: pp-contentful
description: "Every Contentful API command, plus a local SQLite mirror that makes orphan detection, full environment diffs, and... Trigger phrases: `find orphans in contentful`, `diff contentful environments`, `generate a contentful migration`, `bulk publish contentful entries`, `build a contentful image url`, `use contentful-pp-cli`, `run contentful-pp-cli`."
author: "spindle79"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - contentful-pp-cli
---

# Contentful — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `contentful-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer:
   ```bash
   npx -y @mvanhorn/printing-press install contentful --cli-only
   ```
2. Verify: `contentful-pp-cli --version`
3. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the install step did not put the binary on `$PATH`. Do not proceed with skill commands until verification succeeds.

The `contentful-pp-cli` binary wraps every Contentful surface (CMA, CDA, CPA, GraphQL, Images) and ships a local SQLite mirror of every space and environment. That mirror makes the questions every Contentful team writes a custom Node script for — "find orphans", "diff staging and master fully", "which entries have a broken reference", "generate a migration from this diff" — into one-liner commands with `--json` output, `--dry-run` guards, and rate-aware execution that respects `X-Contentful-RateLimit-*` headers.

## When to Use This CLI

Reach for contentful-pp-cli when you need to audit a Contentful space without writing a custom Node script: orphan detection, broken-reference finding, full environment diffs, field usage analysis, or SQL-driven bulk operations. It is also the right CLI for any rate-sensitive workflow — large migrations, scheduled bulk publishes, or content backfills — because rate-aware execution and resumable migrations are first-class. The local SQLite mirror means agents can ask join questions like “which content types are unused” that no single CMA endpoint answers.

## Unique Capabilities

These capabilities aren't available in any other tool for this API.

### Local state that compounds
- **`orphans`** — Find entries with no incoming references, content types with zero entries, and assets nobody links to — across the entire space, in seconds.

  _Every Contentful team writes this script (Pain #6). Reach for orphans before any model cleanup or migration._

  ```bash
  contentful-pp-cli orphans --entries --json
  ```
- **`refs`** — Walk the reference graph forward (what does this entry reference) and reverse (what references this entry) to any depth, offline.

  _When an agent needs to know the blast radius of changing or deleting an entry, this is the only command that answers without N+1 API calls._

  ```bash
  contentful-pp-cli refs 5K8aX --depth 3 --reverse --json
  ```
- **`field-usage`** — Per-locale fill rate for a field; distribution of value shapes; flags fields defined on a content type that no entry uses.

  _Before renaming or removing a field, know exactly how often it's used and in what shape._

  ```bash
  contentful-pp-cli field-usage article.heroImage --json
  ```
- **`validate-content`** — Run the content-type validations (regex, range, in, linkContentType) against the local entries mirror and report violations — no API calls.

  _Catches violations across thousands of entries in seconds, vs. opening each one in the web UI._

  ```bash
  contentful-pp-cli validate-content --content-type article --locale en-US --json
  ```
- **`refs-broken`** — Flags entries whose link fields point at deleted, archived, or unpublished targets — per locale, per environment.

  _Frontend bugs from dangling links are a weekly occurrence; this is the canonical fix-list._

  ```bash
  contentful-pp-cli refs-broken --locale en-US --json
  ```

### Cross-environment workflows
- **`diff`** — Diff two environments across entries, content types, releases, scheduled actions, tags, tasks, and roles — the surfaces contentful-merge skips.

  _Pre-promotion audits today require manual checks across multiple UIs. One command, exit codes encoded._

  ```bash
  contentful-pp-cli diff staging master --include releases,scheduled-actions,tags --json
  ```
- **`migrate-gen`** — Diff two environments and emit a runnable contentful-migration JS script. Inverts the painful 'make changes manually then write a script that mimics them' workflow.

  _The single most-asked-for feature in the Contentful migration ecosystem. No other tool generates migrations from diffs._

  ```bash
  contentful-pp-cli migrate-gen --from staging --to master --output migrations/2026-05-09.js
  ```

### Rate-limit mitigation
- **`entries bulk-publish`** — Use a SQL WHERE against the local mirror to pick which entries to publish, unpublish, or validate — with --dry-run for safety and rate-aware execution that respects X-Contentful-RateLimit-* headers.

  _The 7 req/s rate limit dominates large operations. Selecting precisely with SQL plus adaptive backoff is the only way to ship a 5k-entry batch safely._

  ```bash
  contentful-pp-cli entries bulk-publish 550e8400-e29b-41d4-a716-446655440000 master --where "json_extract(data,'$.sys.contentType.sys.id') = 'article'" --dry-run --json
  ```
- **`migrate run --resumable`** — Run a contentful-migration script with a local checkpoint table; reads X-Contentful-RateLimit-Reset to time backoff; resumes on crash without re-running completed entries.

  _10k-entry migrations that take 25 minutes today no longer leak partway. Pain #1 + Pain #4._

  ```bash
  contentful-pp-cli migrate run migrations/2026-05-09.js --resumable --rate-aware
  ```

### Cross-source insight
- **`gql-impact`** — Given a content-type field about to be removed, scan local .graphql/.ts/.tsx files in cwd for queries referencing it and print file:line.

  _Frontend devs (Sam) catch breaking content-model changes before they ship; no other tool joins content schema with source._

  ```bash
  contentful-pp-cli gql-impact article.heroImage
  ```
- **`images url`** — Build images.ctfassets.net URLs with all transforms (w/h/fit/format/quality); --bulk for many assets; --srcset emits ready-to-paste HTML.

  _Daily task for any frontend on Contentful; replaces hand-built URL strings with a typed CLI._

  ```bash
  contentful-pp-cli images url 4Hx9 --width 800 --fit fill --format webp --srcset 320,640,1280
  ```

## Command Reference

**ai-actions** — Manage AI Actions (LLM-backed entry transformations) (CMA)

- `contentful-pp-cli ai-actions create` — Create an AI Action
- `contentful-pp-cli ai-actions delete` — Delete an AI Action
- `contentful-pp-cli ai-actions get` — Get an AI Action by ID
- `contentful-pp-cli ai-actions invocation` — Get an AI Action invocation result by ID
- `contentful-pp-cli ai-actions invoke` — Invoke an AI Action (returns an invocation ID; poll with invocation)
- `contentful-pp-cli ai-actions list` — List AI Actions
- `contentful-pp-cli ai-actions publish` — Publish an AI Action (no body; requires X-Contentful-Version header)
- `contentful-pp-cli ai-actions unpublish` — Unpublish an AI Action
- `contentful-pp-cli ai-actions update` — Update an AI Action (requires X-Contentful-Version header)

**api-keys** — Manage delivery API keys (CDA tokens) (CMA)

- `contentful-pp-cli api-keys create` — Create a delivery API key
- `contentful-pp-cli api-keys delete` — Delete a delivery API key
- `contentful-pp-cli api-keys get` — Get a delivery API key by ID
- `contentful-pp-cli api-keys list` — List delivery API keys
- `contentful-pp-cli api-keys update` — Update a delivery API key (requires X-Contentful-Version header)

**app-installations** — Manage app installations in an environment (CMA)

- `contentful-pp-cli app-installations get` — Get an app installation by app definition ID
- `contentful-pp-cli app-installations install` — Install or reconfigure an app
- `contentful-pp-cli app-installations list` — List app installations
- `contentful-pp-cli app-installations uninstall` — Uninstall an app

**assets** — Manage assets (CMA)

- `contentful-pp-cli assets archive` — Archive an asset (no body)
- `contentful-pp-cli assets create` — Create an asset (metadata only — call assets process to ingest binary)
- `contentful-pp-cli assets delete` — Delete an asset
- `contentful-pp-cli assets get` — Get an asset by ID
- `contentful-pp-cli assets list` — List assets in an environment
- `contentful-pp-cli assets process` — Trigger asset processing for a specific locale (no body)
- `contentful-pp-cli assets publish` — Publish an asset (no body; requires X-Contentful-Version header)
- `contentful-pp-cli assets unarchive` — Unarchive an asset
- `contentful-pp-cli assets unpublish` — Unpublish an asset
- `contentful-pp-cli assets update` — Update asset metadata (requires X-Contentful-Version header)

**comments** — Manage entry comments (CMA)

- `contentful-pp-cli comments create` — Create a comment on an entry
- `contentful-pp-cli comments delete` — Delete a comment
- `contentful-pp-cli comments get` — Get a comment by ID
- `contentful-pp-cli comments list` — List comments on an entry
- `contentful-pp-cli comments update` — Update a comment (requires X-Contentful-Version header)

**content-type-snapshots** — Read content type snapshots (history) (CMA)

- `contentful-pp-cli content-type-snapshots get` — Get a single content type snapshot
- `contentful-pp-cli content-type-snapshots list` — List snapshots for a content type

**content-types** — Manage content types (CMA)

- `contentful-pp-cli content-types create` — Create a content type with the given ID
- `contentful-pp-cli content-types delete` — Delete a content type
- `contentful-pp-cli content-types get` — Get a content type by ID
- `contentful-pp-cli content-types list` — List content types in an environment
- `contentful-pp-cli content-types publish` — Publish a content type (no body; requires X-Contentful-Version header)
- `contentful-pp-cli content-types unpublish` — Unpublish a content type
- `contentful-pp-cli content-types update` — Update a content type (requires X-Contentful-Version header)

**delivery-assets** — Read published assets via the Content Delivery API (CDA tier)

- `contentful-pp-cli delivery-assets get` — Get a published asset by ID (CDA)
- `contentful-pp-cli delivery-assets list` — List published assets (CDA)

**delivery-content-types** — Read content types via the Content Delivery API (CDA tier)

- `contentful-pp-cli delivery-content-types get` — Get a content type by ID (CDA)
- `contentful-pp-cli delivery-content-types list` — List content types (CDA)

**delivery-entries** — Read published entries via the Content Delivery API (CDA tier — needs CONTENTFUL_DELIVERY_TOKEN)

- `contentful-pp-cli delivery-entries get` — Get a published entry by ID (CDA)
- `contentful-pp-cli delivery-entries list` — List published entries (CDA)

**delivery-locales** — Read locales via the Content Delivery API (CDA tier)

- `contentful-pp-cli delivery-locales <space_id> <environment_id>` — List locales (CDA)

**delivery-sync** — CDA full + incremental sync (drives the local SQLite mirror) (CDA tier). Surfaced as 'delivery-sync' to avoid collision with the built-in 'sync' command that hydrates the local mirror.

- `contentful-pp-cli delivery-sync <space_id> <environment_id>` — Full or incremental sync. Use --initial=true once, then --sync-token from the prior nextSyncToken.

**editor-interfaces** — Manage editor interfaces (Contentful UI controls per content type) (CMA)

- `contentful-pp-cli editor-interfaces get` — Get the editor interface for a content type
- `contentful-pp-cli editor-interfaces list` — List editor interfaces in an environment
- `contentful-pp-cli editor-interfaces update` — Update the editor interface for a content type (requires X-Contentful-Version header)

**entries** — Manage entries (CMA)

- `contentful-pp-cli entries archive` — Archive an entry (no body)
- `contentful-pp-cli entries create` — Create an entry (requires X-Contentful-Content-Type header)
- `contentful-pp-cli entries delete` — Delete an entry
- `contentful-pp-cli entries get` — Get an entry by ID
- `contentful-pp-cli entries list` — List entries in an environment
- `contentful-pp-cli entries publish` — Publish an entry (no body; requires X-Contentful-Version header)
- `contentful-pp-cli entries references` — Get depth-1 references for an entry (online only — for deep refs use the local mirror)
- `contentful-pp-cli entries unarchive` — Unarchive an entry
- `contentful-pp-cli entries unpublish` — Unpublish an entry
- `contentful-pp-cli entries update` — Update an entry (requires X-Contentful-Version header)

**entry-snapshots** — Read entry snapshots (history) (CMA)

- `contentful-pp-cli entry-snapshots get` — Get a single entry snapshot
- `contentful-pp-cli entry-snapshots list` — List snapshots for an entry

**environment-aliases** — Manage environment aliases (CMA)

- `contentful-pp-cli environment-aliases get` — Get an environment alias by ID
- `contentful-pp-cli environment-aliases list` — List environment aliases
- `contentful-pp-cli environment-aliases update` — Update an environment alias to point to a different environment

**environments** — Manage environments within a space (CMA)

- `contentful-pp-cli environments create` — Create an environment (optionally branched from a source)
- `contentful-pp-cli environments delete` — Delete an environment
- `contentful-pp-cli environments get` — Get an environment by ID
- `contentful-pp-cli environments list` — List environments in a space

**extensions** — Manage UI extensions (CMA)

- `contentful-pp-cli extensions create` — Create an extension
- `contentful-pp-cli extensions delete` — Delete an extension
- `contentful-pp-cli extensions get` — Get an extension by ID
- `contentful-pp-cli extensions list` — List extensions
- `contentful-pp-cli extensions update` — Update an extension (requires X-Contentful-Version header)

**gql** — Execute GraphQL queries via the Contentful GraphQL Content API (gql tier)

- `contentful-pp-cli gql <space_id> <environment_id>` — Run a GraphQL query against a space+environment

**locales** — Manage locales (CMA)

- `contentful-pp-cli locales create` — Create a locale
- `contentful-pp-cli locales delete` — Delete a locale
- `contentful-pp-cli locales get` — Get a locale by ID
- `contentful-pp-cli locales list` — List locales in an environment
- `contentful-pp-cli locales update` — Update a locale (requires X-Contentful-Version header)

**organization-memberships** — Read organization memberships (CMA)

- `contentful-pp-cli organization-memberships <organization_id>` — List members of an organization

**organizations** — Read organizations (CMA)

- `contentful-pp-cli organizations get` — Get an organization by ID
- `contentful-pp-cli organizations list` — List organizations the token can access

**preview-api-keys** — Read preview API keys (CPA tokens) (CMA)

- `contentful-pp-cli preview-api-keys get` — Get a preview API key by ID
- `contentful-pp-cli preview-api-keys list` — List preview API keys

**preview-assets** — Read draft + published assets via the Content Preview API (CPA tier)

- `contentful-pp-cli preview-assets get` — Get a draft+published asset by ID (CPA)
- `contentful-pp-cli preview-assets list` — List assets including drafts (CPA)

**preview-entries** — Read draft + published entries via the Content Preview API (CPA tier — needs CONTENTFUL_PREVIEW_TOKEN)

- `contentful-pp-cli preview-entries get` — Get an entry including drafts by ID (CPA)
- `contentful-pp-cli preview-entries list` — List entries including drafts (CPA)

**release-actions** — Read async release actions (publish/validate job results) (CMA)

- `contentful-pp-cli release-actions get` — Get a single release action's status and result
- `contentful-pp-cli release-actions list` — List actions performed against a release

**releases** — Manage releases (batched publish bundles) (CMA)

- `contentful-pp-cli releases create` — Create a release
- `contentful-pp-cli releases delete` — Delete a release
- `contentful-pp-cli releases get` — Get a release by ID
- `contentful-pp-cli releases list` — List releases in an environment
- `contentful-pp-cli releases publish` — Publish all entities in a release
- `contentful-pp-cli releases update` — Update a release (requires X-Contentful-Version header)

**roles** — Manage roles (RBAC) (CMA)

- `contentful-pp-cli roles create` — Create a role
- `contentful-pp-cli roles delete` — Delete a role
- `contentful-pp-cli roles get` — Get a role by ID
- `contentful-pp-cli roles list` — List roles in a space
- `contentful-pp-cli roles update` — Update a role (requires X-Contentful-Version header)

**scheduled-actions** — Manage scheduled actions (publish/unpublish at a future time) (CMA)

- `contentful-pp-cli scheduled-actions cancel` — Cancel a scheduled action
- `contentful-pp-cli scheduled-actions create` — Schedule a publish or unpublish action
- `contentful-pp-cli scheduled-actions get` — Get a scheduled action by ID
- `contentful-pp-cli scheduled-actions list` — List scheduled actions

**space-memberships** — Read space memberships (CMA)

- `contentful-pp-cli space-memberships <space_id>` — List members of a space

**spaces** — Manage Contentful spaces (CMA)

- `contentful-pp-cli spaces create` — Create a new space
- `contentful-pp-cli spaces delete` — Delete a space
- `contentful-pp-cli spaces get` — Get a space by ID
- `contentful-pp-cli spaces list` — List all spaces accessible to the token
- `contentful-pp-cli spaces update` — Update a space (requires X-Contentful-Version header)

**tags** — Manage tags (CMA)

- `contentful-pp-cli tags create` — Create a tag with the given ID
- `contentful-pp-cli tags delete` — Delete a tag
- `contentful-pp-cli tags get` — Get a tag by ID
- `contentful-pp-cli tags list` — List tags in an environment
- `contentful-pp-cli tags update` — Update a tag (requires X-Contentful-Version header)

**tasks** — Manage entry tasks (workflow assignments) (CMA)

- `contentful-pp-cli tasks create` — Create a task on an entry
- `contentful-pp-cli tasks delete` — Delete a task
- `contentful-pp-cli tasks get` — Get a task by ID
- `contentful-pp-cli tasks list` — List tasks on an entry
- `contentful-pp-cli tasks update` — Update a task (requires X-Contentful-Version header)

**users** — Read user info (CMA)

- `contentful-pp-cli users` — Get the authenticated user's profile

**webhook-calls** — Read webhook delivery logs (CMA)

- `contentful-pp-cli webhook-calls get` — Get a single webhook call (full request/response)
- `contentful-pp-cli webhook-calls list` — List recent webhook calls (delivery log)

**webhooks** — Manage webhooks (CMA)

- `contentful-pp-cli webhooks create` — Create a webhook definition
- `contentful-pp-cli webhooks delete` — Delete a webhook
- `contentful-pp-cli webhooks get` — Get a webhook definition by ID
- `contentful-pp-cli webhooks health` — Get a webhook's health (success/failure counts)
- `contentful-pp-cli webhooks list` — List webhook definitions
- `contentful-pp-cli webhooks update` — Update a webhook (requires X-Contentful-Version header)


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
contentful-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Recipes


### Find orphan entries before a model cleanup

```bash
contentful-pp-cli orphans --entries --json --select sys.id,sys.contentType.sys.id
```

Lists every entry with zero incoming references, scoped to ID and content type for compact agent context.

### Diff two environments fully before promotion

```bash
contentful-pp-cli diff staging master --include releases,scheduled-actions,tags,tasks,roles --json
```

Comprehensive pre-promotion check covering surfaces contentful-merge skips.

### Generate a migration from staging→master diff

```bash
contentful-pp-cli migrate-gen --from staging --to master --output migrations/2026-05-09-promote.js
```

Inverts the painful 'edit manually then write the migration script' workflow.

### Rate-aware bulk publish of stale articles

```bash
contentful-pp-cli entries bulk-publish 550e8400-e29b-41d4-a716-446655440000 master --where "json_extract(data,'$.sys.contentType.sys.id') = 'article'" --rate-aware --json
```

Selects targets with SQL against the local mirror; live run respects `X-Contentful-RateLimit-*` headers. Defaults to dry-run preview; pass `--confirm` to execute.

### Trim a deeply-nested entry response with --select

```bash
contentful-pp-cli entries get 550e8400-e29b-41d4-a716-446655440000 master 5K8aXghc8eUqYKGm0IqsAU --locale '*' --json --select sys.id,sys.version,fields.title,fields.body.nodeType
```

CMA entries are deeply nested across locales and rich-text trees; `--select` with dotted paths keeps agent context bounded.

## Auth Setup

Auth uses a Contentful Personal Access Token (PAT) for the management plane (CMA), plus optional space-scoped delivery and preview tokens for read paths. Set CONTENTFUL_MANAGEMENT_TOKEN, CONTENTFUL_DELIVERY_TOKEN, CONTENTFUL_PREVIEW_TOKEN, CONTENTFUL_SPACE_ID, and CONTENTFUL_ENVIRONMENT_ID. Run `contentful-pp-cli auth set-token $CONTENTFUL_MANAGEMENT_TOKEN` to save the PAT, then `contentful-pp-cli auth status` to confirm it loads. EU residency: set CONTENTFUL_HOST=eu to flip every base URL.

Run `contentful-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  contentful-pp-cli ai-actions list $CONTENTFUL_SPACE_ID master --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Explicit retries** — use `--idempotent` only when an already-existing create should count as success, and `--ignore-missing` only when a missing delete target should count as success

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal — piped/agent consumers get pure JSON on stdout.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
contentful-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
contentful-pp-cli feedback --stdin < notes.txt
contentful-pp-cli feedback list --json --limit 10
```

Entries are stored locally at `~/.contentful-pp-cli/feedback.jsonl`. They are never POSTed unless `CONTENTFUL_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `CONTENTFUL_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` (tmp + rename) |
| `webhook:<url>` | POST the output body to the URL (`application/json` or `application/x-ndjson` when `--compact`) |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures return non-zero and log the URL + HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled agent calls the same command every run with the same configuration - HeyGen's "Beacon" pattern.

```
contentful-pp-cli profile save briefing --json
contentful-pp-cli --profile briefing ai-actions list $CONTENTFUL_SPACE_ID master
contentful-pp-cli profile list --json
contentful-pp-cli profile show briefing
contentful-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 4 | Authentication required |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `contentful-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

Install the MCP binary from this CLI's published public-library entry or pre-built release, then register it:

```bash
claude mcp add contentful-pp-mcp -- contentful-pp-mcp
```

Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which contentful-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   contentful-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `contentful-pp-cli <command> --help`.
