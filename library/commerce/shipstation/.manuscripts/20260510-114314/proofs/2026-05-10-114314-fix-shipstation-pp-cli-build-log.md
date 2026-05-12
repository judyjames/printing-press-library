# Build Log — shipstation-pp-cli

## Generation
- Spec: `https://docs.shipstation.com/_bundle/apis/@shipstation-v2/openapi.yaml?download` (453 KB, 13,505 lines, OpenAPI 3.1)
- Pre-generation enrichment: `x-auth-env-vars: [SHIPSTATION_API_KEY]` added under `securitySchemes.api_keys`
- Generator: `printing-press generate --spec <yaml> --force --lenient --validate`
- Result: 157 generated `internal/cli/*.go` files; PASS on go mod tidy / go vet / govulncheck / go build / runnable binary / --help / version / doctor.

## Skipped fields (intentional, generator limitation)
- POST `/v2/batches` — request body skipped (oneOf/anyOf)
- POST `/v2/manifests` — request body skipped (oneOf/anyOf)
- POST `/v2/rates` — request body skipped (oneOf/anyOf)
- POST `/v2/rates/estimate` — request body skipped (oneOf/anyOf)

These four endpoints are still callable through the CLI (with empty bodies); clients that need full request shapes should populate the body manually until the generator handles oneOf properly.

## Phase 3 transcendence features built (8/8)

| # | Command | File | Status |
|---|---------|------|--------|
| 1 | `rate-history compare` | `internal/cli/rate_history.go` | OK — empty store returns `[]` with stderr hint |
| 2 | `batches triage` | `internal/cli/batch_triage.go` | OK — empty store returns `[]` |
| 3 | `batches retry` | `internal/cli/batch_retry.go` | OK — `--dry-run` clean; only `--only-errored` mode supported |
| 4 | `labels cost` | `internal/cli/labels_cost.go` | OK — empty store returns `[]` |
| 5 | `inventory drift` | `internal/cli/inventory_drift.go` | OK — requires `--vs <file>`; dry-run clean |
| 6 | `inventory velocity` | `internal/cli/inventory_velocity.go` | OK — empty store returns `[]` |
| 7 | `eod burndown` | `internal/cli/eod.go` | OK — empty store returns `{date, total_unlabeled:0, sample:[], by_status:[]}` |
| 8 | `orphans` | `internal/cli/orphans.go` | OK — empty store returns `{stuck:[], missing:[]}` |

Shared helpers added: `internal/cli/novel_helpers.go` (date range parsing, ISO week math, tolerant float parsing for stringified amounts).

## Generator-level fix applied (defensive patch on this run)

The generated `internal/store/store.go` named several SQLite tables after reserved words / sub-resource leaf names: `add`, `errors`, `process`, `remove`, `options`, `services`, `return`, `track`, `void`, `documents`, `receives`, `status`, `cancel`. The migration failed immediately with `near "add": syntax error` because `ADD` is parsed by SQLite as the start of an ALTER-style clause when used as a CREATE TABLE identifier without quoting.

**Patch:** wrapped each occurrence of these 13 table names in double-quoted SQLite identifiers (`"add"`, `"errors"`, ...) across CREATE TABLE / CREATE INDEX / INSERT INTO / UPDATE / FROM / JOIN / DELETE FROM. 40 substitutions total. Backup at `internal/store/store.go.bak`.

This is a **generator bug** that should be fixed upstream in the Printing Press — every CLI generated against an OpenAPI spec with sub-resources whose path leaf name is a reserved word will hit this. Captured for retro.

## MCP enrichment NOT applied

The generator warned that 101 endpoint-mirror MCP tools is past the 50-tool threshold and recommended the Cloudflare pattern (`mcp.transport: [stdio, http]`, `mcp.orchestration: code`, `mcp.endpoint_tools: hidden`). For OpenAPI specs, the skill's enrichment path is via spec-level extension, but the generator does not currently expose a CLI flag for this. Tracking as a Phase 5.5 polish concern; will accept the scorecard hit on `mcp_surface_strategy` if polish does not flag it.

## Acceptance evidence

```
$ go build -o shipstation-pp-cli ./cmd/shipstation-pp-cli
$ echo $?  → 0

$ ./shipstation-pp-cli doctor --json
  api: "reachable (HTTP 404 at /)"  → ShipStation v2 returns 404 on root, normal
  auth: "configured", auth_source: "env:SHIPSTATION_API_KEY"
  cache: status: "unknown" (DB not yet hydrated)

$ ./shipstation-pp-cli rate-history compare --from 2026-04-01 --to 2026-04-30 --carrier ups_ground --vs fedex_home_delivery --json
  []  (empty DB; correct)
$ ./shipstation-pp-cli batches triage --json                → []
$ ./shipstation-pp-cli batches retry batch_test --only-errored --dry-run → exit 0
$ ./shipstation-pp-cli labels cost --json                   → []
$ ./shipstation-pp-cli inventory drift --vs /tmp/no.csv --dry-run → exit 0
$ ./shipstation-pp-cli inventory velocity --json            → []
$ ./shipstation-pp-cli eod burndown --json                  → {date, total_unlabeled:0, sample:[], by_status:[]}
$ ./shipstation-pp-cli orphans --json                       → {stuck:[], missing:[]}
```

Ready for shipcheck.
