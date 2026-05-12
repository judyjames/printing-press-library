# ShipStation CLI Brief

## API Identity
- **Domain:** Multi-carrier shipping & fulfillment (USPS, UPS, FedEx, DHL, etc.)
- **API generation:** v2 (`api.shipstation.com`), the modern ShipEngine-rebranded API. v1 (`ssapi.shipstation.com`) is legacy SOAP-style and out of scope per user choice.
- **Users:** E-commerce ops, 3PL warehouses, brand-direct sellers, Shopify/WooCommerce merchants who batch labels and reconcile shipments daily.
- **Data profile:** Long-lived order/shipment records, nightly label burst, sparse webhooks, slow-moving inventory & SKU master, fast-moving rate quotes (often discarded).

## Reachability Risk
- **None.** Official OpenAPI 3.1 spec is downloadable (453 KB YAML, 101 operations, 21 resource tags), production base URL is canonical, multiple actively-maintained SDKs, docs are first-class. Auth is well-defined (`api-key` header). No bot protection, no challenge tokens.

## Top Workflows
1. **Buy a label fast** — `rate compare → label create-from-rate → download PDF/PNG/ZPL`. The single most-run workflow; latency-sensitive.
2. **Bulk label burst** — `batch create → batch add → batch process → batch list-errors → download zip`. The only sane way to ship 100+ packages a day.
3. **End-of-day manifest** — `manifest create → fetch barcode PDF`. SCAN form for pickup driver; otherwise warehouses scan every label.
4. **Track a shipment** — `label track <id>` for a single package; `tracking stop` to silence noisy event streams.
5. **Rate shopping with cost optimization** — compare every connected carrier/service for a given parcel before printing.
6. **Reconcile orders to shipments** — list shipments with filters, tag for sweep, query by external_shipment_id from upstream OMS.
7. **Inventory + warehouse keep-alive** — list inventory by SKU, by warehouse, by location; adjust stock; receive POs.

## Table Stakes (must match)
- Labels: create, void, return, get, track, list with filters
- Shipments: list, get, get-by-external-id, create, update, cancel, tag/untag, list rates for shipment
- Rates: calculate, estimate, get-by-id
- Batches: create, add, remove, process, list errors, get
- Manifests: list, create, get
- Carriers: list, get, services, package types, options
- Inventory: levels, warehouses, locations (full CRUD)
- Purchase Orders: full CRUD + receive + status + summary PDFs
- Suppliers, Products, Tags, Warehouses, Webhooks, Pickups, Totes, Mailing/NetStamps
- **Local cache** — every shipment, label, rate, manifest, batch, inventory snapshot in SQLite for offline query
- **Sync** — full + incremental, with cursors per resource
- **Search** — FTS across recipient names, addresses, tracking numbers, external IDs

## Data Layer
- **Primary entities:** `shipments`, `labels`, `rates`, `batches`, `manifests`, `carriers`, `inventory_levels`, `inventory_warehouses`, `inventory_locations`, `purchase_orders`, `suppliers`, `webhooks`, `pickups`, `totes`, `tags`, `package_types`, `products`
- **Sync cursor:** `created_at` for shipments/labels (with paginated cursor); rates are stored only when realized into a label.
- **FTS5/search:** recipient name, address line 1, city, postal code, tracking number, external_shipment_id, label_id, batch_id, sku, supplier_name.
- **Snapshots:** rate quotes (so you can answer "what did UPS Ground cost yesterday at 9 AM?"), inventory levels (so you can chart stockout risk), batch errors (so you can re-run failed sub-shipments).

## Codebase Intelligence
- **Auth:** `api-key` header (lowercase, name `api-key` per spec). MCP and unofficial Node SDK use env var `SHIPSTATION_API_KEY`.
- **Rate limit:** default 200/min (v2 API), 100/min on high-volume plans, 20/min on sandbox. Server returns `429` with `Retry-After`, plus `X-Rate-Limit-{Limit,Remaining,Reset}` on every response.
- **Pagination:** `page`/`page_size` query params, with `pages` and `total` in the envelope.
- **Mock environment:** `https://docs.shipstation.com/_mock/apis/openapi/` available for non-destructive testing without a paid carrier connection.
- **Spec source:** `https://docs.shipstation.com/_bundle/apis/@shipstation-v2/openapi.yaml?download` — fetched, parsed, 101 ops cleanly.

## User Vision
- Targeting **v2 only** (per user choice); legacy v1 deferred. Live testing planned via `SHIPSTATION_API_KEY`. No combo-source priorities — single canonical spec.

## Product Thesis
- **Name:** `shipstation-pp-cli` — a power-user CLI for ShipStation v2 with offline replay, rate-quote history, batch-error triage, label cost analytics, and SKU inventory drift detection that no MCP or web UI surfaces.
- **Why it should exist:** The official MCP exposes 52 tools but is read-mostly and stateless. Existing wrappers (Node, Python `shipengine`) are libraries, not CLIs — agents have to write code to use them. Web UI is fine for one-offs but terrible at "tell me which carrier was 11% cheaper for 1-lb-box-to-CA last week" or "which batches have errored sub-shipments older than 24 hours." A SQLite-backed CLI with `--json`, `--select`, typed exit codes, and shell-pipe-friendly output is the missing third tool.

## Build Priorities
1. **P0** — Generate from OpenAPI spec, wire `api-key` header auth, verify 200 OK against `/v2/carriers` and `/v2/labels?page_size=1`.
2. **P1 (absorb)** — All 101 endpoints become CLI subcommands. Local SQLite store for the 10 highest-gravity entities. `sync`, `search`, `sql`. Match all 52 MCP tools.
3. **P2 (transcend)** — Rate-quote history, batch-error triage, label cost roll-up, manifest reconciliation, inventory drift, SKU velocity, end-of-day burndown.
