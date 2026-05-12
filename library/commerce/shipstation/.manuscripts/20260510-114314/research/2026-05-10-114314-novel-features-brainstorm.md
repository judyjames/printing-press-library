# Novel Features Brainstorm — shipstation-pp-cli

## Customer model

**Persona A — Maya, the 3PL warehouse lead**
- **Today:** Runs a 3PL fulfillment floor in Reno; ships for 14 Shopify brands. Logs into ShipStation web UI 30+ times a day; keeps a stack of post-its with batch IDs taped to her monitor.
- **Weekly ritual:** Monday 6am she creates 4-6 batches (one per brand), drops in 80-150 shipments each, hits Process, then babysits the errors tab refreshing until every sub-shipment clears. End of day she prints the SCAN-form manifest for the UPS driver. Wednesday and Friday she repeats with a smaller burst.
- **Frustration:** When a batch errors, the web UI shows one error at a time and doesn't tell her which orders failed for the same reason. She often re-processes the whole batch instead of fixing the 4 broken rows. There's no way to ask "which batches today still have unresolved errors older than 2 hours?"

**Persona B — Devon, the brand-direct shipping ops lead**
- **Today:** Runs ops at a DTC apparel brand doing 600 orders/day across UPS, USPS, and FedEx. Reports weekly to the CFO on shipping spend.
- **Weekly ritual:** Friday afternoon pulls a CSV of every label cut that week, pivots in Sheets by carrier+service+weight band, compares to last week's average cost-per-pound. Monday morning rate-shops the top-3 SKUs by volume against all connected carriers to see if a service swap saves >5%.
- **Frustration:** Rates are ephemeral in the API — once you print a label, the alternate quotes are gone. She can't answer "what would UPS Ground have cost on the 412 FedEx Home Delivery labels we cut last Tuesday?" without re-quoting (and rates change daily).

**Persona C — Priya, the inventory + PO coordinator**
- **Today:** Manages 2,400 SKUs across 3 warehouses. Cuts 8-12 POs a week to suppliers, receives 3-5.
- **Weekly ritual:** Tuesday morning reconciles inventory levels against the OMS; flags any SKU where ShipStation's level has drifted >5 units from the OMS truth. Friday she runs a stockout-risk check: SKUs with <14 days of cover at current 30-day velocity.
- **Frustration:** ShipStation gives her the level today and the level yesterday but no series. She maintains a manual Google Sheet of weekly snapshots to compute velocity. POs and receiving events live separate from inventory levels — she can't easily ask "what's the inbound + on-hand position for SKU X across all warehouses?"

**Persona D — Sam, the integration engineer at a Shopify ops shop**
- **Today:** Builds and babysits ShipStation integrations for 9 merchant clients. On call when webhooks stop firing or external_shipment_id mappings drift.
- **Weekly ritual:** Daily morning health-check across clients: any shipments stuck without a label after 4h? Any external_shipment_ids in OMS that don't have a matching ShipStation shipment? Any webhook subs that haven't fired in 24h?
- **Frustration:** Each check needs a different curl + jq incantation. There's no "give me the orphans" button. He's written the same Node script three times and hates it.

## Candidates (pre-cut)

(See main subagent output above; 18 candidates before cut.)

## Survivors and kills

### Survivors

| # | Feature | Command | Score | How It Works | Evidence |
|---|---------|---------|-------|--------------|----------|
| 1 | Rate-quote history compare | `rate-history compare --from <d> --to <d> --carrier A --vs B` | 9/10 | Joins persisted `rates` snapshots with realized `labels` in local SQLite; no API call needed at query time | Brief Data Layer line "rate quotes (so you can answer 'what did UPS Ground cost yesterday at 9 AM?')"; Product Thesis line "tell me which carrier was 11% cheaper for 1-lb-box-to-CA last week"; Devon weekly ritual |
| 2 | Batch-error triage | `batch triage [--age 2h] [--reason ...]` | 9/10 | `SELECT batch_id, error_code, count(*) FROM batch_errors WHERE resolved=0 GROUP BY ...` over locally-stored batch error rows | Brief Top Workflow #2 names `batch list-errors`; Product Thesis line "which batches have errored sub-shipments older than 24 hours"; MCP exposes batches but not triage |
| 3 | Batch-error retry (errored-only) | `batch retry <id> --only-errored` | 8/10 | Reads errored sub-shipment IDs from local batch_errors, posts a fresh batch process with that subset via `/v2/batches/{id}/process` | Maya frustration (re-process whole batch); MCP's 10 batch tools are all 1:1 endpoint mirrors with no retry helper |
| 4 | Cost roll-up | `labels cost --by carrier,service --week current` | 8/10 | Local SQL aggregate over the labels table with carrier/service/weight bands; CSV/JSON output | Devon weekly Sheets pivot; brief Build Priority P2 names "label cost roll-up" |
| 5 | Inventory drift | `inventory drift --vs file.csv [--threshold N]` | 7/10 | Reads external truth file, diffs against `inventory_levels` table, prints drifted SKU rows | Priya frustration (manual sheet); brief P2 names "inventory drift detection"; MCP/SDKs offer no diff |
| 6 | SKU velocity / stockout risk | `inventory velocity [--days-cover N]` | 8/10 | Joins last-30-days `shipments`/`labels` line items against current `inventory_levels`, computes days-of-cover | Priya weekly stockout check; brief P2 names "SKU velocity"; web UI lacks any velocity view |
| 7 | End-of-day burndown | `eod burndown` | 7/10 | Local left-anti-join: shipments with `created_at::date = today` not present in labels table; returns the gap list + counts | Brief P2 names "end-of-day burndown"; Maya/Sam morning + EOD ritual |
| 8 | Orphan finder | `orphans [--external-ids file] [--stuck 4h]` | 7/10 | Local SQL: shipments without labels after N hours, plus optional left-join of an external_id list against `shipments.external_shipment_id` | Sam morning health-check; Top Workflow #6 names external_shipment_id lookups; MCP has no orphan finder |

### Killed candidates

| Feature | Kill reason | Closest surviving sibling |
|---------|-------------|---------------------------|
| Cheapest-rate live shop | Thin sort over absorbed `rates calculate`; framework `--select` already covers it | Rate-history compare |
| Manifest reconcile | Narrow; absorbed `manifests get` + SQL gets there | Cost roll-up + EOD burndown |
| Inventory snapshot history | Subsumed by velocity command (same snapshot table); reachable via `sql` | SKU velocity |
| PO position | Niche; reachable via `sql` join | SKU velocity |
| Carrier-mix recommender | Risks LLM-shape opinion; overlaps rate-history with proof | Rate-history compare |
| Webhook silence check | Unbuildable: no public delivery-log endpoint; would need a sidecar receiver | (cut) |
| Label PDF batch download | Thin loop over absorbed `downloads file` + shell `for` | Absorbed `downloads file` |
| Address validator | No standalone validate endpoint in v2; would be reimplementation | Absorbed `rates calculate` |
| Pickup planner | Reformat of absorbed `package_pickups list`; no new join | Absorbed `package_pickups list` |
| Tracking sweep | Framework `search` + `sql` over labels covers "stale tracking" | `search` / `sql` + absorbed `labels track` |
