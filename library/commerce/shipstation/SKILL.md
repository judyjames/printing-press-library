---
name: pp-shipstation
description: "Every ShipStation v2 endpoint plus rate-quote history, batch-error triage, cost roll-ups, and inventory velocity... Trigger phrases: `ship a package via ShipStation`, `rate-shop carriers`, `process a ShipStation batch`, `triage batch errors`, `check inventory velocity`, `stockout risk`, `end-of-day shipping reconcile`, `shipping cost report`, `use shipstation`, `run shipstation`."
author: "James Bongiovanni"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - shipstation-pp-cli
---

# ShipStation — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `shipstation-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer:
   ```bash
   npx -y @mvanhorn/printing-press install shipstation --cli-only
   ```
2. Verify: `shipstation-pp-cli --version`
3. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the install step did not put the binary on `$PATH`. Do not proceed with skill commands until verification succeeds.

Wraps the entire 101-endpoint ShipStation API v2 with a local SQLite store so you can answer questions the live API cannot. `rate-history compare` replays persisted quotes; `batch triage` aggregates open errors by reason and age; `inventory velocity` flags stockout risk before it happens. Agent-native output (`--json`, `--select`, typed exit codes) and offline replay come standard.

## When to Use This CLI

Use this CLI when you need to script ShipStation operations from a shell, run a Claude Code agent against a real shipping account, or run analytics that the web UI cannot answer. It is best at: rate-shopping with historical comparison, bulk label burst with error triage, inventory drift detection across an OMS, and end-of-day reconciliation before a carrier pickup. Skip it if you just need to print one label \u2014 the web UI is faster.

## Unique Capabilities

These capabilities aren't available in any other tool for this API.

### Rate intelligence
- **`rate-history compare`** — Replay persisted rate quotes to answer 'what would carrier B have cost on the labels carrier A actually shipped?' across any date range.

  _Use this when a finance or ops stakeholder asks 'what would we have saved on UPS vs FedEx last month?' without re-quoting hundreds of stale shipments._

  ```bash
  shipstation-pp-cli rate-history compare --from 2026-04-01 --to 2026-04-30 --carrier ups_ground --vs fedex_home_delivery --json --select carrier,total_cost,delta
  ```

### Batch ops
- **`batches triage`** — Lists every batch with unresolved errored sub-shipments, grouped by error reason and aged so you can fix the worst things first.

  _Reach for this any time a warehouse asks 'which batches still have errors older than X hours?' — it replaces babysitting the errors tab._

  ```bash
  shipstation-pp-cli batches triage --age 2h --json
  ```
- **`batches retry`** — Re-process only the errored sub-shipments in a batch, instead of re-running the whole thing.

  _Use when 4 of 150 sub-shipments fail and you want to retry exactly those 4 without re-paying the postage on the 146 that succeeded._

  ```bash
  shipstation-pp-cli batches retry batch_a1b2c3 --only-errored --dry-run
  ```

### Cost analytics
- **`labels cost`** — Pivot all labels by carrier / service / weight band with totals, averages, and week-over-week deltas.

  _Use for the weekly CFO shipping-spend report — pipe the CSV straight into Sheets._

  ```bash
  shipstation-pp-cli labels cost --by carrier,service --week current --csv
  ```

### Inventory intelligence
- **`inventory drift`** — Diff ShipStation inventory levels against an external truth file (OMS export, supplier catalog) and print every drifted SKU.

  _Use Tuesday morning to surface SKUs where ShipStation and the OMS disagree, before stockouts cause oversells._

  ```bash
  shipstation-pp-cli inventory drift --vs ./oms-snapshot.csv --threshold 5 --json
  ```
- **`inventory velocity`** — Joins last 30 days of shipped units against current on-hand inventory to compute days-of-cover per SKU and flag stockout risk.

  _Use Friday afternoon to see which SKUs run out before next week's PO arrives._

  ```bash
  shipstation-pp-cli inventory velocity --days-cover 14 --json --select sku,velocity_per_day,days_cover
  ```

### Operations
- **`eod burndown`** — Today's shipments with no label, grouped by warehouse and age, so you can clear the floor before pickup.

  _Run at 4:30pm before the carrier pickup to confirm every order on the floor has a label printed._

  ```bash
  shipstation-pp-cli eod burndown --warehouse main --json
  ```
- **`orphans`** — Find shipments stuck without a label after N hours, plus optionally cross-check an OMS-side external-ID list against ShipStation shipments.

  _Use any time an integration goes quiet — orphans tells you exactly which orders are missing or stalled._

  ```bash
  shipstation-pp-cli orphans --external-ids ./oms-orders.txt --stuck 4h --json
  ```

## Command Reference

**batches** — Process labels in bulk and receive a large number of labels and customs forms in bulk responses. Batching is ideal for workflows that need to process hundreds or thousands of labels quickly.

- `shipstation-pp-cli batches create-batch` — Create a batch containing multiple labels.
- `shipstation-pp-cli batches delete-batch` — Delete a batch based on its batch id. Sets its status to 'archived'.
- `shipstation-pp-cli batches get-batch-by-external-id` — Retreive a batch using an external batch ID
- `shipstation-pp-cli batches get-batch-by-id` — Get batch details for a specific batch id.
- `shipstation-pp-cli batches list` — List the batches associated with your ShipStation account.
- `shipstation-pp-cli batches update-batch` — Update a batch by id setting its status to 'archived'.

**carriers** — Retreive useful details about the carriers connected to your accounts, including carrier IDs, service IDs, advanced options, and available carrier package types.

- `shipstation-pp-cli carriers get-by-id` — Retrive details about a specific carrier by its carrier id.
- `shipstation-pp-cli carriers list` — List all carriers that have been added to this account.

**downloads** — Download your label files in PDF, PNG, and ZPL.

- `shipstation-pp-cli downloads <subdir> <filename> <dir>` — Download labels and other shipment-related documents.

**environment** — Manage environment

- `shipstation-pp-cli environment create-webhook` — Create a webhook for specific events in the environment.
- `shipstation-pp-cli environment delete-webhook` — Delete webhook by id
- `shipstation-pp-cli environment get-webhook-by-id` — Retrieve individual webhook by an ID
- `shipstation-pp-cli environment list-webhooks` — List all webhooks currently enabled for the account.
- `shipstation-pp-cli environment update-webhook` — Update the webhook url property

**fulfillments** — Manage fulfillments which represent completed shipments. Create fulfillments to mark orders as shipped with tracking information and notify customers and marketplaces.

- `shipstation-pp-cli fulfillments create` — Create one or more fulfillments by marking shipments as shipped with tracking information. This will notify...
- `shipstation-pp-cli fulfillments list` — Retrieve a list of fulfillments based on various filter criteria. You can filter by shipment details, tracking...

**inventory** — Manage inventory levels, warehouses, and locations.

- `shipstation-pp-cli inventory get-levels` — List SKU inventory levels
- `shipstation-pp-cli inventory update-skustock-levels` — Update SKU stock levels and related properties

**inventory-locations** — Manage inventory locations

- `shipstation-pp-cli inventory-locations create` — Create a new inventory location
- `shipstation-pp-cli inventory-locations delete-by-id` — Delete an inventory location
- `shipstation-pp-cli inventory-locations get-by-id` — Get inventory location by ID
- `shipstation-pp-cli inventory-locations list` — List all inventory locations
- `shipstation-pp-cli inventory-locations update` — Update an inventory location

**inventory-warehouses** — Manage inventory warehouses

- `shipstation-pp-cli inventory-warehouses add-new` — Create a new inventory warehouse
- `shipstation-pp-cli inventory-warehouses delete` — Delete an inventory warehouse
- `shipstation-pp-cli inventory-warehouses get` — List all inventory warehouses
- `shipstation-pp-cli inventory-warehouses get-by-id` — Get a specific inventory warehouse and related properties using its warehouse ID
- `shipstation-pp-cli inventory-warehouses update` — Update an inventory warehouse name

**labels** — Purchase and print shipping labels for any carrier active on your account. The labels endpoint also supports creating return labels, voiding labels, and getting label details like tracking.

- `shipstation-pp-cli labels create` — Purchase and print a label for shipment.
- `shipstation-pp-cli labels create-from-rate` — When retrieving rates for shipments using the `/rates` endpoint, the returned information contains a `rate_id`...
- `shipstation-pp-cli labels create-from-rate-shopper` — Create a label using Rate Shopper to automatically select the best carrier and service based on the specified...
- `shipstation-pp-cli labels create-from-shipment` — Purchase a label using a shipment ID that has already been created with the desired address and package info.
- `shipstation-pp-cli labels get-by-external-shipment-id` — Find a label by using the external shipment id that was used during label creation. > **Warning:** This endpoint...
- `shipstation-pp-cli labels get-by-id` — Retrieve a specific label by its label id.
- `shipstation-pp-cli labels list` — This method returns a list of labels that you've created. You can optionally filter the results as well as control...

**mailing** — Create mailing labels for USPS including NetStamps, mail labels and envelopes.

- `shipstation-pp-cli mailing create-envelope` — Create a single envelope shipment.
- `shipstation-pp-cli mailing create-labels` — Create one or more mailing labels on a sheet layout. Use `mailing_options` to control which row and column to start...
- `shipstation-pp-cli mailing create-netstamps` — Create one or more NetStamps. Each shipment in the request produces an individual stamp with optional row/column...

**manifests** — A manifest is a document that provides a list of the day's shipments. It typically contains a barcode that allows the pickup driver to scan a single document to register all shipments, rather than scanning each shipment individually.

- `shipstation-pp-cli manifests create` — Each ShipStation manifest is created for a specific warehouse, so you'll need to provide the warehouse_id rather...
- `shipstation-pp-cli manifests get-by-id` — Get Manifest By Id
- `shipstation-pp-cli manifests list` — Similar to querying shipments, we allow you to query manifests since there will likely be a large number over a long...

**packages** — Manage packages

- `shipstation-pp-cli packages create-type` — Create a custom package type to better assist in getting accurate rate estimates
- `shipstation-pp-cli packages delete-typ` — Delete a custom package using the ID
- `shipstation-pp-cli packages get-type-by-id` — Get Custom Package Type by ID
- `shipstation-pp-cli packages list-types` — List the custom package types associated with the account
- `shipstation-pp-cli packages update-type` — Update the custom package type object by ID

**pickups** — Manage pickups

- `shipstation-pp-cli pickups delete-scheduled` — Delete a previously-scheduled pickup by ID
- `shipstation-pp-cli pickups get-by-id` — Get Pickup By ID
- `shipstation-pp-cli pickups list-scheduled` — List all pickups that have been scheduled for this carrier
- `shipstation-pp-cli pickups schedule` — Schedule a package pickup with a carrier

**products** — Manage products in your ShipStation account. Products represent the items you sell and ship to customers.

- `shipstation-pp-cli products` — List products

**purchase-orders** — Create and manage purchase orders from suppliers to replenish inventory. Track order status, receive products, and update inventory levels automatically.

- `shipstation-pp-cli purchase-orders create` — Create a new purchase order with products from a supplier.
- `shipstation-pp-cli purchase-orders get` — Retrieve detailed information about a specific purchase order including all products.
- `shipstation-pp-cli purchase-orders list` — Retrieve a paginated list of purchase orders with optional filtering by status, warehouse, dates, and other criteria.
- `shipstation-pp-cli purchase-orders update` — Update an existing purchase order. Editing limitations: - In the `draft` status, all fields can be edited. - Once a...

**rates** — Quickly compare rates using the Rates endpoint. You can see and compare rates for the carriers connected to your account (as long as they support sending rates).

- `shipstation-pp-cli rates calculate` — It's not uncommon that you want to give your customer the choice between whether they want to ship the fastest,...
- `shipstation-pp-cli rates estimate` — Get Rate Estimates
- `shipstation-pp-cli rates get-by-id` — Retrieve a previously queried rate by its ID

**shipments** — Shipments are at the core of most ShipStation capabilities. Shipment objects are required for cretaing labels and manifests, as well as getting rates.

- `shipstation-pp-cli shipments assign-user-to` — Assigns a user to one or more shipments. You can assign a single user to up to 500 shipments at once.
- `shipstation-pp-cli shipments create` — Create one or more shipments
- `shipstation-pp-cli shipments get-by-external-id` — Query Shipments created using your own custom ID convention using this endpoint
- `shipstation-pp-cli shipments get-by-id` — Get an individual shipment based on its ID
- `shipstation-pp-cli shipments list` — Get list of Shipments
- `shipstation-pp-cli shipments update` — Update an existing shipment's details including addresses, package information, carrier service, and shipping...

**suppliers** — Manage supplier information including contact details, email addresses, and physical addresses. Suppliers are used when creating purchase orders.

- `shipstation-pp-cli suppliers create` — Create a new supplier with contact and address information.
- `shipstation-pp-cli suppliers get` — Retrieve detailed information about a specific supplier.
- `shipstation-pp-cli suppliers list` — Retrieve a paginated list of all suppliers with optional filtering by supplier name.
- `shipstation-pp-cli suppliers update` — Update an existing supplier's information.

**tags** — Tags are text-based identifiers you can add to shipments to help in your shipment management workflows.

- `shipstation-pp-cli tags create` — Create a new Tag for customizing how you track your shipments
- `shipstation-pp-cli tags list` — Get a list of all tags associated with an account.

**totes** — Manage totes (bins or containers) used in warehouse picking and packing operations. Create, update, delete totes and track tote quantities by warehouse.

- `shipstation-pp-cli totes create-batch` — Create multiple totes at once. Returns both successfully created totes and any failures.
- `shipstation-pp-cli totes delete` — Delete a tote by its ID.
- `shipstation-pp-cli totes get-by-id` — Retrieve details of a specific tote.
- `shipstation-pp-cli totes get-quantities` — Get the number of totes in each warehouse.
- `shipstation-pp-cli totes list` — Get all totes for the seller, optionally filtered by warehouse.
- `shipstation-pp-cli totes update` — Update the name or barcode of an existing tote.

**tracking** — Use the tracking endpoint to stop receiving tracking updates (more dedicated tracking endpoint methods coming soon).

- `shipstation-pp-cli tracking` — Unsubscribe from tracking updates for a package.

**users** — Manage and retrieve user information for the ShipStation account. This endpoint allows you to list users with various filtering options.

- `shipstation-pp-cli users` — List users

**warehouses** — Get warehouse details like warehouse ID and related addresses using the warehouses endpoint.

- `shipstation-pp-cli warehouses get-by-id` — Retrieve warehouse data based on the warehouse ID
- `shipstation-pp-cli warehouses list` — Retrieve a list of warehouses associated with this account.


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
shipstation-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Recipes


### Compare every UPS Ground label this month against FedEx

```bash
shipstation-pp-cli rate-history compare --from 2026-04-01 --to 2026-04-30 --carrier ups_ground --vs fedex_home_delivery --json --select label_id,carrier,total_cost,alt_carrier,alt_cost,delta
```

Joins persisted UPS labels with the closest stored FedEx quotes; `--select` keeps the response compact for agents to parse.

### Find every batch with errors older than 2 hours

```bash
shipstation-pp-cli batch triage --age 2h --json
```

Local SQL over the batch_errors table; sorts by oldest unresolved error so you fix the worst things first.

### Re-process only the errored sub-shipments in a batch

```bash
shipstation-pp-cli batch retry batch_a1b2c3 --only-errored --dry-run
```

Reads errored sub-shipment IDs from the local batch_errors table; `--dry-run` shows what would re-process without spending postage.

### Generate Friday's CFO shipping-spend report

```bash
shipstation-pp-cli labels cost --by carrier,service --week current --csv > shipping-spend.csv
```

Pivots every label cut this week by carrier and service; pipe straight into Sheets for the CFO.

### Surface stockout-risk SKUs (narrowing a verbose response)

```bash
shipstation-pp-cli inventory velocity --days-cover 14 --json --select sku,on_hand,velocity_per_day,days_cover,warehouse
```

Velocity calls return a row per SKU per warehouse; `--select` narrows the deeply-nested payload to the five fields agents need.

### Find orphan orders missing from ShipStation

```bash
shipstation-pp-cli orphans --external-ids ./oms-orders.txt --stuck 4h --json
```

Left-joins your OMS order list against ShipStation shipments; surfaces both stuck shipments and missing-from-ShipStation orders.

## Auth Setup

Uses a single header `api-key`. Set `SHIPSTATION_API_KEY` in your environment (the same env var the official MCP uses) or pass `--api-key` per call. v1 legacy auth (key + secret over Basic) is not supported; this CLI targets the v2 ShipStation API at api.shipstation.com.

Run `shipstation-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  shipstation-pp-cli batches list --agent --select id,name,status
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
shipstation-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
shipstation-pp-cli feedback --stdin < notes.txt
shipstation-pp-cli feedback list --json --limit 10
```

Entries are stored locally at `~/.shipstation-pp-cli/feedback.jsonl`. They are never POSTed unless `SHIPSTATION_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `SHIPSTATION_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

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
shipstation-pp-cli profile save briefing --json
shipstation-pp-cli --profile briefing batches list
shipstation-pp-cli profile list --json
shipstation-pp-cli profile show briefing
shipstation-pp-cli profile delete briefing --yes
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

1. **Empty, `help`, or `--help`** → show `shipstation-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

Install the MCP binary from this CLI's published public-library entry or pre-built release, then register it:

```bash
claude mcp add shipstation-pp-mcp -- shipstation-pp-mcp
```

Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which shipstation-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   shipstation-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `shipstation-pp-cli <command> --help`.
