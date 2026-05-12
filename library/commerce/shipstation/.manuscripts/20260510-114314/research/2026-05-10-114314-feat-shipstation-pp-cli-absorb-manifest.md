# shipstation-pp-cli — Absorb Manifest

## Sources surveyed

| Source | Type | Features touched |
|--------|------|------------------|
| Official OpenAPI 3.1 spec (docs.shipstation.com, 13.5k lines, 101 ops, 21 tags) | spec | All endpoints |
| `shipstation/mcp-shipstation-api` (official MCP, 52 tools, 7 domains) | MCP | Subset of 101 endpoints |
| `rip-technologies/shipstation-node` (TypeScript, both v1+v2, rate-limit + retry) | SDK | Library wrapper |
| `ShipEngine/shipengine-python` (official Python SDK, env `SHIPENGINE_API_KEY`) | SDK | Library wrapper |
| `AustinBratcher/shipengine-js`, `kjaenicke/shipstation-node`, `JustMaier/node-shipstation` | SDK | Older library wrappers |
| `mattcoatsworth/shipstation-mcp-server`, `CDataSoftware/shipstation-mcp-server-by-cdata` | MCP | Read-only mirrors |
| ShipStation Help Center, ShipEngine docs (rate limits, 429, X-Rate-Limit-* headers) | docs | Operational guidance |

## Absorbed (match or beat everything that exists)

The OpenAPI spec covers the entire surface area exposed by competing tools — the official MCP's 52 tools are a strict subset of the 101 spec operations. The generator emits all 101 as endpoint-mirror commands; rows below are grouped by resource family rather than 1-row-per-op.

| # | Feature group | Best Source | Our Implementation | Added Value |
|---|---------------|-------------|--------------------|-------------|
| A1 | Shipments full surface (list, get, get-by-external-id, create, update, cancel, list rates, tag/untag, internal notes, user assign) — 11 ops | OpenAPI shipments tag + MCP shipment tools | endpoint-mirror commands `shipments list/get/...` | --json/--csv/--select, --dry-run, typed exit codes, local SQLite cache, FTS search across recipients/tracking/external IDs |
| A2 | Labels full surface (list, create, create-from-rate, create-from-shipment, create-from-rate-shopper, get, get-by-external-shipment-id, return, track, void, cancel-refund) — 11 ops | OpenAPI labels tag + MCP 8 label tools | endpoint-mirror commands `labels list/get/...` | offline track replay, --select, persistent label cost ledger |
| A3 | Rates (calculate, estimate, get-by-id) — 3 ops | OpenAPI rates tag + MCP rate tools | `rates calculate/estimate/get` | rate quotes persisted to SQLite for the rate-history transcendence feature |
| A4 | Batches full surface (list, create, get, get-by-external-id, update, delete, add, remove, process, list-errors) — 10 ops | OpenAPI batches tag + MCP 10 batch tools | endpoint-mirror commands `batches list/get/...` | local batch_errors table joined to shipments; foundation for triage + retry |
| A5 | Manifests (list, create, get) — 3 ops | OpenAPI manifests tag + MCP 3 manifest tools | `manifests list/create/get` | manifest contents stored locally |
| A6 | Carriers (list, get, services, packages, options) — 5 ops | OpenAPI carriers tag + MCP 5 carrier tools | `carriers list/get/services/packages/options` | carrier services synced for offline rate comparison |
| A7 | Inventory (levels, warehouses CRUD, locations CRUD) — 12 ops | OpenAPI inventory tag + MCP 13 inventory tools | endpoint-mirror commands | inventory snapshots stored daily for drift + velocity |
| A8 | Purchase Orders full surface (list/create/get/update + receive + status + 2 PDF summaries) — 9 ops | OpenAPI purchase_orders tag | endpoint-mirror commands | local PO ledger |
| A9 | Totes (list, create-batch, quantities, get, update, delete) — 6 ops | OpenAPI totes tag | endpoint-mirror commands | tote contents synced |
| A10 | Package Pickups (list, schedule, get, delete) — 4 ops | OpenAPI package_pickups tag | endpoint-mirror commands | calendar of upcoming pickups |
| A11 | Package Types CRUD — 5 ops | OpenAPI package_types tag | endpoint-mirror commands | local catalog |
| A12 | Mailing (NetStamps, mail labels, envelopes) — 3 ops | OpenAPI mailing tag | endpoint-mirror commands | -- |
| A13 | Suppliers CRUD — 4 ops | OpenAPI suppliers tag | endpoint-mirror commands | -- |
| A14 | Webhooks CRUD — 5 ops | OpenAPI webhooks tag | endpoint-mirror commands | local subscription list |
| A15 | Tags (list, create) — 2 ops | OpenAPI tags tag | endpoint-mirror commands | -- |
| A16 | Warehouses (list, get) — 2 ops | OpenAPI warehouses tag | endpoint-mirror commands | -- |
| A17 | Fulfillments (list, create) — 2 ops | OpenAPI fulfillments tag | endpoint-mirror commands | -- |
| A18 | Tracking (stop) — 1 op | OpenAPI tracking tag | endpoint-mirror command | -- |
| A19 | Downloads (file) — 1 op | OpenAPI downloads tag | endpoint-mirror command | --output to save PDF/PNG/ZPL bytes |
| A20 | Users + Products list — 2 ops | OpenAPI users + products tags | endpoint-mirror commands | -- |
| A21 | sync (full + incremental, all entities, cursor per resource) | framework | `sync` command | -- |
| A22 | search (FTS5 across recipients, addresses, tracking numbers, external IDs, SKUs, supplier names) | framework | `search` command | works offline |
| A23 | sql (SELECT-only over local store; pipe-friendly) | framework | `sql` command | composable with jq, awk, claude |
| A24 | doctor (auth, reachability, version, rate-limit-budget) | framework | `doctor` command | -- |

**Total absorbed surface:** all 101 OpenAPI operations + 24 framework features = 125 things this CLI can do that exist somewhere else, all with `--json`, `--select`, `--csv`, `--compact`, `--dry-run`, `--limit`, typed exit codes, and a local SQLite store.

## Transcendence (only possible with our approach)

| # | Feature | Command | Score | How It Works | Evidence |
|---|---------|---------|-------|--------------|----------|
| T1 | Rate-quote history compare | `rate-history compare --from <d> --to <d> --carrier A --vs B` | 9/10 | Joins persisted `rates` snapshots with realized `labels` in local SQLite; no API call needed at query time | Brief Data Layer line "rate quotes (so you can answer 'what did UPS Ground cost yesterday at 9 AM?')"; Product Thesis; Devon weekly ritual |
| T2 | Batch-error triage | `batch triage [--age 2h] [--reason ...]` | 9/10 | `SELECT batch_id, error_code, count(*) FROM batch_errors WHERE resolved=0 GROUP BY ...` over locally-stored batch error rows | Brief Top Workflow #2 names `batch list-errors`; Product Thesis "stale batches >24h with errors"; MCP exposes batches but not triage |
| T3 | Batch-error retry (errored-only) | `batch retry <id> --only-errored` | 8/10 | Reads errored sub-shipment IDs from local batch_errors, posts fresh batch process with that subset via `/v2/batches/{id}/process` | Maya frustration; MCP's 10 batch tools are 1:1 mirrors with no retry helper |
| T4 | Cost roll-up | `labels cost --by carrier,service --week current` | 8/10 | Local SQL aggregate over labels with carrier/service/weight bands; CSV/JSON output | Devon Friday CFO report; Brief P2 names "label cost roll-up" |
| T5 | Inventory drift | `inventory drift --vs file.csv [--threshold N]` | 7/10 | Reads external truth file, diffs against `inventory_levels`, prints drifted SKU rows | Priya frustration with manual Sheet; Brief P2 names "inventory drift detection" |
| T6 | SKU velocity / stockout risk | `inventory velocity [--days-cover N]` | 8/10 | Joins last-30-days shipped units against current `inventory_levels`, computes days-of-cover, flags stockout-risk SKUs | Priya weekly stockout check; Brief P2 names "SKU velocity"; web UI lacks any velocity view |
| T7 | End-of-day burndown | `eod burndown` | 7/10 | Local left-anti-join: today's shipments not present in labels; returns gap list + counts | Brief P2 names "end-of-day burndown"; Maya EOD ritual |
| T8 | Orphan finder | `orphans [--external-ids file] [--stuck 4h]` | 7/10 | Local SQL: shipments without labels after N hours; optional left-join of OMS external_id list against `shipments.external_shipment_id` | Sam morning health-check; Top Workflow #6 names external_shipment_id lookups |

**Total novel features:** 8 transcendence rows, all 7-9/10, all serving a named persona in the customer model. None require LLM, scraping, or external services beyond the ShipStation API + optional user-supplied files.

## Stubs

None. Every absorbed feature ships as full endpoint-mirror commands; every transcendence feature has a clear local-data buildability proof and is shipping scope.

## Why ship this

Existing tools fall into three buckets — the official MCP (read-mostly, stateless, 52 tools), unofficial language SDKs (libraries, not CLIs), and the web UI (great for one-offs, terrible for analytics). A SQLite-backed CLI fills the gap: agent-friendly, offline-replayable, composable in pipelines, and the only way to answer "which batches still have errors older than 2 hours?" or "what would UPS Ground have cost on every FedEx label we cut last Tuesday?" without a custom integration.
