# Contentful CLI

> Contentful isn't just a headless CMS. It's a graph of references, locales, and publish state — every entry is a signal about content health.

That reframe is why this CLI ships `orphans`, `refs-broken`, `field-usage`, `migrate-gen`, and `webhooks health` alongside the raw API commands: each one reads the graph the API exposes piecewise and turns it into a single answer.

**Every Contentful API command, plus a local SQLite mirror that makes orphan detection, full environment diffs, and SQL-driven bulk publishing one-liners.**

The `contentful-pp-cli` binary wraps every Contentful surface (CMA, CDA, CPA, GraphQL, Images) and ships a local SQLite mirror of every space and environment. That mirror makes the questions every Contentful team writes a custom Node script for — "find orphans", "diff staging and master fully", "which entries have a broken reference", "generate a migration from this diff" — into one-liner commands with `--json` output, `--dry-run` guards, and rate-aware execution that respects `X-Contentful-RateLimit-*` headers.

## Install

The recommended path installs both the `contentful-pp-cli` binary and the `pp-contentful` agent skill in one shot:

```bash
npx -y @mvanhorn/printing-press install contentful
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press install contentful --cli-only
```


### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/contentful-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-contentful --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-contentful --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-contentful skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-contentful. The skill defines how its required CLI can be installed.
```

## Authentication

Auth uses a Contentful Personal Access Token (PAT) for the management plane (CMA), plus optional space-scoped delivery and preview tokens for read paths. Set CONTENTFUL_MANAGEMENT_TOKEN, CONTENTFUL_DELIVERY_TOKEN, CONTENTFUL_PREVIEW_TOKEN, CONTENTFUL_SPACE_ID, and CONTENTFUL_ENVIRONMENT_ID. Run `contentful-pp-cli auth set-token $CONTENTFUL_MANAGEMENT_TOKEN` to save the PAT, then `contentful-pp-cli auth status` to confirm it loads. EU residency: set CONTENTFUL_HOST=eu to flip every base URL.

## Quick Start

```bash
# save PAT and persist defaults
contentful-pp-cli auth set-token $CONTENTFUL_MANAGEMENT_TOKEN


# mirror the active space+env into local SQLite (drives offline queries)
contentful-pp-cli sync --full


# find every entry nothing references — the most-asked-for audit in Contentful
contentful-pp-cli orphans --entries --json


# full pre-promotion environment diff that contentful-merge skips
contentful-pp-cli diff staging master --include releases,scheduled-actions,tags --json


# preview rate-aware bulk publish from a SQL filter
contentful-pp-cli entries bulk-publish $CONTENTFUL_SPACE_ID master --where "json_extract(data,'$.sys.contentType.sys.id') = 'article'" --dry-run

```

## Unique Features

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

## Usage

Run `contentful-pp-cli --help` for the full command reference and flag list.

## Commands

### ai-actions

Manage AI Actions (LLM-backed entry transformations) (CMA)

- **`contentful-pp-cli ai-actions create`** - Create an AI Action
- **`contentful-pp-cli ai-actions delete`** - Delete an AI Action
- **`contentful-pp-cli ai-actions get`** - Get an AI Action by ID
- **`contentful-pp-cli ai-actions invocation`** - Get an AI Action invocation result by ID
- **`contentful-pp-cli ai-actions invoke`** - Invoke an AI Action (returns an invocation ID; poll with invocation)
- **`contentful-pp-cli ai-actions list`** - List AI Actions
- **`contentful-pp-cli ai-actions publish`** - Publish an AI Action (no body; requires X-Contentful-Version header)
- **`contentful-pp-cli ai-actions unpublish`** - Unpublish an AI Action
- **`contentful-pp-cli ai-actions update`** - Update an AI Action (requires X-Contentful-Version header)

### api-keys

Manage delivery API keys (CDA tokens) (CMA)

- **`contentful-pp-cli api-keys create`** - Create a delivery API key
- **`contentful-pp-cli api-keys delete`** - Delete a delivery API key
- **`contentful-pp-cli api-keys get`** - Get a delivery API key by ID
- **`contentful-pp-cli api-keys list`** - List delivery API keys
- **`contentful-pp-cli api-keys update`** - Update a delivery API key (requires X-Contentful-Version header)

### app-installations

Manage app installations in an environment (CMA)

- **`contentful-pp-cli app-installations get`** - Get an app installation by app definition ID
- **`contentful-pp-cli app-installations install`** - Install or reconfigure an app
- **`contentful-pp-cli app-installations list`** - List app installations
- **`contentful-pp-cli app-installations uninstall`** - Uninstall an app

### assets

Manage assets (CMA)

- **`contentful-pp-cli assets archive`** - Archive an asset (no body)
- **`contentful-pp-cli assets create`** - Create an asset (metadata only — call assets process to ingest binary)
- **`contentful-pp-cli assets delete`** - Delete an asset
- **`contentful-pp-cli assets get`** - Get an asset by ID
- **`contentful-pp-cli assets list`** - List assets in an environment
- **`contentful-pp-cli assets process`** - Trigger asset processing for a specific locale (no body)
- **`contentful-pp-cli assets publish`** - Publish an asset (no body; requires X-Contentful-Version header)
- **`contentful-pp-cli assets unarchive`** - Unarchive an asset
- **`contentful-pp-cli assets unpublish`** - Unpublish an asset
- **`contentful-pp-cli assets update`** - Update asset metadata (requires X-Contentful-Version header)

### comments

Manage entry comments (CMA)

- **`contentful-pp-cli comments create`** - Create a comment on an entry
- **`contentful-pp-cli comments delete`** - Delete a comment
- **`contentful-pp-cli comments get`** - Get a comment by ID
- **`contentful-pp-cli comments list`** - List comments on an entry
- **`contentful-pp-cli comments update`** - Update a comment (requires X-Contentful-Version header)

### content-type-snapshots

Read content type snapshots (history) (CMA)

- **`contentful-pp-cli content-type-snapshots get`** - Get a single content type snapshot
- **`contentful-pp-cli content-type-snapshots list`** - List snapshots for a content type

### content-types

Manage content types (CMA)

- **`contentful-pp-cli content-types create`** - Create a content type with the given ID
- **`contentful-pp-cli content-types delete`** - Delete a content type
- **`contentful-pp-cli content-types get`** - Get a content type by ID
- **`contentful-pp-cli content-types list`** - List content types in an environment
- **`contentful-pp-cli content-types publish`** - Publish a content type (no body; requires X-Contentful-Version header)
- **`contentful-pp-cli content-types unpublish`** - Unpublish a content type
- **`contentful-pp-cli content-types update`** - Update a content type (requires X-Contentful-Version header)

### delivery-assets

Read published assets via the Content Delivery API (CDA tier)

- **`contentful-pp-cli delivery-assets get`** - Get a published asset by ID (CDA)
- **`contentful-pp-cli delivery-assets list`** - List published assets (CDA)

### delivery-content-types

Read content types via the Content Delivery API (CDA tier)

- **`contentful-pp-cli delivery-content-types get`** - Get a content type by ID (CDA)
- **`contentful-pp-cli delivery-content-types list`** - List content types (CDA)

### delivery-entries

Read published entries via the Content Delivery API (CDA tier — needs CONTENTFUL_DELIVERY_TOKEN)

- **`contentful-pp-cli delivery-entries get`** - Get a published entry by ID (CDA)
- **`contentful-pp-cli delivery-entries list`** - List published entries (CDA)

### delivery-locales

Read locales via the Content Delivery API (CDA tier)

- **`contentful-pp-cli delivery-locales list`** - List locales (CDA)

### delivery-sync

CDA full + incremental sync (drives the local SQLite mirror) (CDA tier). Surfaced as 'delivery-sync' to avoid collision with the built-in 'sync' command that hydrates the local mirror.

- **`contentful-pp-cli delivery-sync run`** - Full or incremental sync. Use --initial=true once, then --sync-token from the prior nextSyncToken.

### editor-interfaces

Manage editor interfaces (Contentful UI controls per content type) (CMA)

- **`contentful-pp-cli editor-interfaces get`** - Get the editor interface for a content type
- **`contentful-pp-cli editor-interfaces list`** - List editor interfaces in an environment
- **`contentful-pp-cli editor-interfaces update`** - Update the editor interface for a content type (requires X-Contentful-Version header)

### entries

Manage entries (CMA)

- **`contentful-pp-cli entries archive`** - Archive an entry (no body)
- **`contentful-pp-cli entries create`** - Create an entry (requires X-Contentful-Content-Type header)
- **`contentful-pp-cli entries delete`** - Delete an entry
- **`contentful-pp-cli entries get`** - Get an entry by ID
- **`contentful-pp-cli entries list`** - List entries in an environment
- **`contentful-pp-cli entries publish`** - Publish an entry (no body; requires X-Contentful-Version header)
- **`contentful-pp-cli entries references`** - Get depth-1 references for an entry (online only — for deep refs use the local mirror)
- **`contentful-pp-cli entries unarchive`** - Unarchive an entry
- **`contentful-pp-cli entries unpublish`** - Unpublish an entry
- **`contentful-pp-cli entries update`** - Update an entry (requires X-Contentful-Version header)

### entry-snapshots

Read entry snapshots (history) (CMA)

- **`contentful-pp-cli entry-snapshots get`** - Get a single entry snapshot
- **`contentful-pp-cli entry-snapshots list`** - List snapshots for an entry

### environment-aliases

Manage environment aliases (CMA)

- **`contentful-pp-cli environment-aliases get`** - Get an environment alias by ID
- **`contentful-pp-cli environment-aliases list`** - List environment aliases
- **`contentful-pp-cli environment-aliases update`** - Update an environment alias to point to a different environment

### environments

Manage environments within a space (CMA)

- **`contentful-pp-cli environments create`** - Create an environment (optionally branched from a source)
- **`contentful-pp-cli environments delete`** - Delete an environment
- **`contentful-pp-cli environments get`** - Get an environment by ID
- **`contentful-pp-cli environments list`** - List environments in a space

### extensions

Manage UI extensions (CMA)

- **`contentful-pp-cli extensions create`** - Create an extension
- **`contentful-pp-cli extensions delete`** - Delete an extension
- **`contentful-pp-cli extensions get`** - Get an extension by ID
- **`contentful-pp-cli extensions list`** - List extensions
- **`contentful-pp-cli extensions update`** - Update an extension (requires X-Contentful-Version header)

### gql

Execute GraphQL queries via the Contentful GraphQL Content API (gql tier)

- **`contentful-pp-cli gql query`** - Run a GraphQL query against a space+environment

### locales

Manage locales (CMA)

- **`contentful-pp-cli locales create`** - Create a locale
- **`contentful-pp-cli locales delete`** - Delete a locale
- **`contentful-pp-cli locales get`** - Get a locale by ID
- **`contentful-pp-cli locales list`** - List locales in an environment
- **`contentful-pp-cli locales update`** - Update a locale (requires X-Contentful-Version header)

### organization-memberships

Read organization memberships (CMA)

- **`contentful-pp-cli organization-memberships list`** - List members of an organization

### organizations

Read organizations (CMA)

- **`contentful-pp-cli organizations get`** - Get an organization by ID
- **`contentful-pp-cli organizations list`** - List organizations the token can access

### preview-api-keys

Read preview API keys (CPA tokens) (CMA)

- **`contentful-pp-cli preview-api-keys get`** - Get a preview API key by ID
- **`contentful-pp-cli preview-api-keys list`** - List preview API keys

### preview-assets

Read draft + published assets via the Content Preview API (CPA tier)

- **`contentful-pp-cli preview-assets get`** - Get a draft+published asset by ID (CPA)
- **`contentful-pp-cli preview-assets list`** - List assets including drafts (CPA)

### preview-entries

Read draft + published entries via the Content Preview API (CPA tier — needs CONTENTFUL_PREVIEW_TOKEN)

- **`contentful-pp-cli preview-entries get`** - Get an entry including drafts by ID (CPA)
- **`contentful-pp-cli preview-entries list`** - List entries including drafts (CPA)

### release-actions

Read async release actions (publish/validate job results) (CMA)

- **`contentful-pp-cli release-actions get`** - Get a single release action's status and result
- **`contentful-pp-cli release-actions list`** - List actions performed against a release

### releases

Manage releases (batched publish bundles) (CMA)

- **`contentful-pp-cli releases create`** - Create a release
- **`contentful-pp-cli releases delete`** - Delete a release
- **`contentful-pp-cli releases get`** - Get a release by ID
- **`contentful-pp-cli releases list`** - List releases in an environment
- **`contentful-pp-cli releases publish`** - Publish all entities in a release
- **`contentful-pp-cli releases update`** - Update a release (requires X-Contentful-Version header)

### roles

Manage roles (RBAC) (CMA)

- **`contentful-pp-cli roles create`** - Create a role
- **`contentful-pp-cli roles delete`** - Delete a role
- **`contentful-pp-cli roles get`** - Get a role by ID
- **`contentful-pp-cli roles list`** - List roles in a space
- **`contentful-pp-cli roles update`** - Update a role (requires X-Contentful-Version header)

### scheduled-actions

Manage scheduled actions (publish/unpublish at a future time) (CMA)

- **`contentful-pp-cli scheduled-actions cancel`** - Cancel a scheduled action
- **`contentful-pp-cli scheduled-actions create`** - Schedule a publish or unpublish action
- **`contentful-pp-cli scheduled-actions get`** - Get a scheduled action by ID
- **`contentful-pp-cli scheduled-actions list`** - List scheduled actions

### space-memberships

Read space memberships (CMA)

- **`contentful-pp-cli space-memberships list`** - List members of a space

### spaces

Manage Contentful spaces (CMA)

- **`contentful-pp-cli spaces create`** - Create a new space
- **`contentful-pp-cli spaces delete`** - Delete a space
- **`contentful-pp-cli spaces get`** - Get a space by ID
- **`contentful-pp-cli spaces list`** - List all spaces accessible to the token
- **`contentful-pp-cli spaces update`** - Update a space (requires X-Contentful-Version header)

### tags

Manage tags (CMA)

- **`contentful-pp-cli tags create`** - Create a tag with the given ID
- **`contentful-pp-cli tags delete`** - Delete a tag
- **`contentful-pp-cli tags get`** - Get a tag by ID
- **`contentful-pp-cli tags list`** - List tags in an environment
- **`contentful-pp-cli tags update`** - Update a tag (requires X-Contentful-Version header)

### tasks

Manage entry tasks (workflow assignments) (CMA)

- **`contentful-pp-cli tasks create`** - Create a task on an entry
- **`contentful-pp-cli tasks delete`** - Delete a task
- **`contentful-pp-cli tasks get`** - Get a task by ID
- **`contentful-pp-cli tasks list`** - List tasks on an entry
- **`contentful-pp-cli tasks update`** - Update a task (requires X-Contentful-Version header)

### users

Read user info (CMA)

- **`contentful-pp-cli users me`** - Get the authenticated user's profile

### webhook-calls

Read webhook delivery logs (CMA)

- **`contentful-pp-cli webhook-calls get`** - Get a single webhook call (full request/response)
- **`contentful-pp-cli webhook-calls list`** - List recent webhook calls (delivery log)

### webhooks

Manage webhooks (CMA)

- **`contentful-pp-cli webhooks create`** - Create a webhook definition
- **`contentful-pp-cli webhooks delete`** - Delete a webhook
- **`contentful-pp-cli webhooks get`** - Get a webhook definition by ID
- **`contentful-pp-cli webhooks health`** - Get a webhook's health (success/failure counts)
- **`contentful-pp-cli webhooks list`** - List webhook definitions
- **`contentful-pp-cli webhooks update`** - Update a webhook (requires X-Contentful-Version header)


## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
contentful-pp-cli ai-actions list $CONTENTFUL_SPACE_ID master

# JSON for scripting and agents
contentful-pp-cli ai-actions list $CONTENTFUL_SPACE_ID master --json

# Filter to specific fields
contentful-pp-cli ai-actions list $CONTENTFUL_SPACE_ID master --json --select id,name,status

# Dry run — show the request without sending
contentful-pp-cli ai-actions list $CONTENTFUL_SPACE_ID master --dry-run

# Agent mode — JSON + compact + no prompts in one flag
contentful-pp-cli ai-actions list $CONTENTFUL_SPACE_ID master --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select id,name` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Explicit retries** - add `--idempotent` to create retries and `--ignore-missing` to delete retries when a no-op success is acceptable
- **Confirmable** - `--yes` for explicit confirmation of destructive actions
- **Piped input** - write commands can accept structured input when their help lists `--stdin`
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes (Printing Press convention):

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (bad arguments or invalid config) |
| 3 | Authentication error (token missing, expired, or lacks scope) |
| 4 | Resource not found |
| 5 | Rate limited — wait and retry |
| 7 | Upstream server error |

## Use with Claude Code

Install the focused skill — it auto-installs the CLI on first invocation:

```bash
npx skills add mvanhorn/printing-press-library/cli-skills/pp-contentful -g
```

Then invoke `/pp-contentful <query>` in Claude Code. The skill is the most efficient path — Claude Code drives the CLI directly without an MCP server in the middle.

<details>
<summary>Use as an MCP server in Claude Code (advanced)</summary>

If you'd rather register this CLI as an MCP server in Claude Code, install the MCP binary first:


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Then register it:

```bash
claude mcp add contentful contentful-pp-mcp -e CONTENTFUL_MANAGEMENT_TOKEN=<your-token>
```

</details>

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/contentful-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `CONTENTFUL_MANAGEMENT_TOKEN` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "contentful": {
      "command": "contentful-pp-mcp",
      "env": {
        "CONTENTFUL_MANAGEMENT_TOKEN": "<your-key>"
      }
    }
  }
}
```

</details>

## Health Check

```bash
contentful-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Config file: `~/.config/contentful-pp-cli/config.toml`

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `CONTENTFUL_MANAGEMENT_TOKEN` | per_call | Yes | Set to your API credential. |

## Troubleshooting
**Authentication errors (exit code 3)**
- Run `contentful-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $CONTENTFUL_MANAGEMENT_TOKEN`
**Not found errors (exit code 4)**
- Check the resource ID is correct
- Run the `list` command to see available items
**Rate limited (exit code 5)**
- Back off and retry; for migrations use `migrate run --resumable --rate-aware`

### API-specific

- **401 Unauthorized on every CMA call** — Re-validate your PAT: `contentful-pp-cli auth status`. PATs inherit your full user permissions; if your account lost space access, the token still authenticates but loses scope.
- **429 rate limit hit during a migration** — Use `migrate run --resumable --rate-aware`. The local checkpoint table picks up where the run left off and the adaptive token-bucket reads X-Contentful-RateLimit-Reset to time the next request.
- **VersionMismatch / 409 on entry update** — Re-run `contentful-pp-cli sync` to refresh the local copy; the server expects X-Contentful-Version to match the latest sys.version.
- **EU space data appears empty** — Set CONTENTFUL_HOST=eu before invoking. CMA, CDA, CPA, GraphQL, and Images all have EU base URLs.
- **Pagination drops entries past 1000** — Pass --cursor to use cursor-based pagination instead of offset; offset paging caps at 1000 and gets slow for 10k+.

---

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**contentful.js**](https://github.com/contentful/contentful.js) — TypeScript (1300 stars)
- [**contentful-cli**](https://github.com/contentful/contentful-cli) — JavaScript (357 stars)
- [**contentful-management.js**](https://github.com/contentful/contentful-management.js) — TypeScript (288 stars)
- [**ivo-toby/contentful-mcp**](https://github.com/ivo-toby/contentful-mcp) — TypeScript (60 stars)
- [**@contentful/mcp-server**](https://github.com/contentful/contentful-mcp-server) — TypeScript (53 stars)
- [**contentful-merge**](https://github.com/contentful/contentful-merge) — TypeScript
- [**contentful-migration**](https://github.com/contentful/contentful-migration) — JavaScript

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
