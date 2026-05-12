# ShipStation CLI

**Every ShipStation v2 endpoint plus rate-quote history, batch-error triage, cost roll-ups, and inventory velocity that no MCP or web UI surfaces.**

Wraps the entire 101-endpoint ShipStation API v2 with a local SQLite store so you can answer questions the live API cannot. `rate-history compare` replays persisted quotes; `batch triage` aggregates open errors by reason and age; `inventory velocity` flags stockout risk before it happens. Agent-native output (`--json`, `--select`, typed exit codes) and offline replay come standard.

## Install

The recommended path installs both the `shipstation-pp-cli` binary and the `pp-shipstation` agent skill in one shot:

```bash
npx -y @mvanhorn/printing-press install shipstation
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press install shipstation --cli-only
```


### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/shipstation-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-shipstation --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-shipstation --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-shipstation skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-shipstation. The skill defines how its required CLI can be installed.
```

## Authentication

Uses a single header `api-key`. Set `SHIPSTATION_API_KEY` in your environment (the same env var the official MCP uses) or pass `--api-key` per call. v1 legacy auth (key + secret over Basic) is not supported; this CLI targets the v2 ShipStation API at api.shipstation.com.

## Quick Start

```bash
# Verifies the API key reaches api.shipstation.com and reports rate-limit budget.
shipstation-pp-cli doctor


# Confirms which carriers are connected; small payload, fast first call.
shipstation-pp-cli carriers list --json --select carrier_id,friendly_name,services_count


# Pulls every shipment, label, rate quote, and batch from April forward into the local SQLite store.
shipstation-pp-cli sync --resource shipments,labels,rates,batches --since 2026-04-01


# Answers 'what would FedEx have cost on every UPS Ground label last month?' without re-quoting.
shipstation-pp-cli rate-history compare --from 2026-04-01 --to 2026-04-30 --carrier ups_ground --vs fedex_home_delivery --json


# Lists every batch with unresolved errors older than two hours, grouped by error reason.
shipstation-pp-cli batch triage --age 2h --json

```

## Unique Features

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

## Usage

Run `shipstation-pp-cli --help` for the full command reference and flag list.

## Commands

### batches

Process labels in bulk and receive a large number of labels and customs forms in bulk responses. Batching is ideal for workflows that need to process hundreds or thousands of labels quickly.

- **`shipstation-pp-cli batches create-batch`** - Create a batch containing multiple labels.
- **`shipstation-pp-cli batches delete-batch`** - Delete a batch based on its batch id. Sets its status to 'archived'.
- **`shipstation-pp-cli batches get-batch-by-external-id`** - Retreive a batch using an external batch ID
- **`shipstation-pp-cli batches get-batch-by-id`** - Get batch details for a specific batch id.
- **`shipstation-pp-cli batches list`** - List the batches associated with your ShipStation account.
- **`shipstation-pp-cli batches update-batch`** - Update a batch by id setting its status to 'archived'.

### carriers

Retreive useful details about the carriers connected to your accounts, including carrier IDs, service IDs, advanced options, and available carrier package types.

- **`shipstation-pp-cli carriers get-by-id`** - Retrive details about a specific carrier by its carrier id.
- **`shipstation-pp-cli carriers list`** - List all carriers that have been added to this account.

### downloads

Download your label files in PDF, PNG, and ZPL.

- **`shipstation-pp-cli downloads file`** - Download labels and other shipment-related documents.

### environment

Manage environment

- **`shipstation-pp-cli environment create-webhook`** - Create a webhook for specific events in the environment.
- **`shipstation-pp-cli environment delete-webhook`** - Delete webhook by id
- **`shipstation-pp-cli environment get-webhook-by-id`** - Retrieve individual webhook by an ID
- **`shipstation-pp-cli environment list-webhooks`** - List all webhooks currently enabled for the account.
- **`shipstation-pp-cli environment update-webhook`** - Update the webhook url property

### fulfillments

Manage fulfillments which represent completed shipments. Create fulfillments to mark orders as shipped with tracking information and notify customers and marketplaces.

- **`shipstation-pp-cli fulfillments create`** - Create one or more fulfillments by marking shipments as shipped with tracking information.
This will notify customers and marketplaces according to your configuration.
- **`shipstation-pp-cli fulfillments list`** - Retrieve a list of fulfillments based on various filter criteria. You can filter by shipment details,
tracking information, dates, and more to find the specific fulfillments you need.

### inventory

Manage inventory levels, warehouses, and locations.

- **`shipstation-pp-cli inventory get-levels`** - List SKU inventory levels
- **`shipstation-pp-cli inventory update-skustock-levels`** - Update SKU stock levels and related properties

### inventory-locations

Manage inventory locations

- **`shipstation-pp-cli inventory-locations create`** - Create a new inventory location
- **`shipstation-pp-cli inventory-locations delete-by-id`** - Delete an inventory location
- **`shipstation-pp-cli inventory-locations get-by-id`** - Get inventory location by ID
- **`shipstation-pp-cli inventory-locations list`** - List all inventory locations
- **`shipstation-pp-cli inventory-locations update`** - Update an inventory location

### inventory-warehouses

Manage inventory warehouses

- **`shipstation-pp-cli inventory-warehouses add-new`** - Create a new inventory warehouse
- **`shipstation-pp-cli inventory-warehouses delete`** - Delete an inventory warehouse
- **`shipstation-pp-cli inventory-warehouses get`** - List all inventory warehouses
- **`shipstation-pp-cli inventory-warehouses get-by-id`** - Get a specific inventory warehouse and related properties using its warehouse ID
- **`shipstation-pp-cli inventory-warehouses update`** - Update an inventory warehouse name

### labels

Purchase and print shipping labels for any carrier active on your account. The labels endpoint also supports creating return labels, voiding labels, and getting label details like tracking.

- **`shipstation-pp-cli labels create`** - Purchase and print a label for shipment.
- **`shipstation-pp-cli labels create-from-rate`** - When retrieving rates for shipments using the `/rates` endpoint, the returned information contains a `rate_id` property that can be used to generate a label without having to refill in the shipment information repeatedly.
- **`shipstation-pp-cli labels create-from-rate-shopper`** - Create a label using Rate Shopper to automatically select the best
carrier and service based on the specified strategy.

For more information about Rate Shopper strategies and use cases, see [Rate Shopping](/rate-shopping#automatic-label-creation-with-rate-shopper).
- **`shipstation-pp-cli labels create-from-shipment`** - Purchase a label using a shipment ID that has already been created with the desired address and package info.
- **`shipstation-pp-cli labels get-by-external-shipment-id`** - Find a label by using the external shipment id that was used during label creation.

> **Warning:** This endpoint returns only the first label found with the specified `external_shipment_id`. If multiple labels share the same `external_shipment_id`, only the earliest created label will be returned. To retrieve all labels with a specific `external_shipment_id`, use the [list labels endpoint](#operation/list_labels) with the `external_shipment_id` query parameter.
- **`shipstation-pp-cli labels get-by-id`** - Retrieve a specific label by its label id.
- **`shipstation-pp-cli labels list`** - This method returns a list of labels that you've created. You can optionally filter the results as well as control their sort order and the number of results returned at a time.

By default all labels are returned 25 at a time, starting with the most recently created ones. You can combine multiple filter options to narrow-down the results.  For example, if you only want your UPS labels for your east coast warehouse you could query by both `warehouse_id` and `carrier_id`.

### mailing

Create mailing labels for USPS including NetStamps, mail labels and envelopes.

- **`shipstation-pp-cli mailing create-envelope`** - Create a single envelope shipment.
- **`shipstation-pp-cli mailing create-labels`** - Create one or more mailing labels on a sheet layout.
Use `mailing_options` to control which row and column to start printing from.
- **`shipstation-pp-cli mailing create-netstamps`** - Create one or more NetStamps. Each shipment in the request produces an individual stamp
with optional row/column positioning via `advanced_options.netstamps_options`.

### manifests

A manifest is a document that provides a list of the day's shipments. It typically contains a barcode that allows the pickup driver to scan a single document to register all shipments, rather than scanning each shipment individually.

- **`shipstation-pp-cli manifests create`** - Each ShipStation manifest is created for a specific warehouse, so you'll need to provide the warehouse_id
rather than the ship_from address. You can create a warehouse for each location that you want to create manifests for.
- **`shipstation-pp-cli manifests get-by-id`** - Get Manifest By Id
- **`shipstation-pp-cli manifests list`** - Similar to querying shipments, we allow you to query manifests since there will likely be a large number over a long period of time.

### packages

Manage packages

- **`shipstation-pp-cli packages create-type`** - Create a custom package type to better assist in getting accurate rate estimates
- **`shipstation-pp-cli packages delete-typ`** - Delete a custom package using the ID
- **`shipstation-pp-cli packages get-type-by-id`** - Get Custom Package Type by ID
- **`shipstation-pp-cli packages list-types`** - List the custom package types associated with the account
- **`shipstation-pp-cli packages update-type`** - Update the custom package type object by ID

### pickups

Manage pickups

- **`shipstation-pp-cli pickups delete-scheduled`** - Delete a previously-scheduled pickup by ID
- **`shipstation-pp-cli pickups get-by-id`** - Get Pickup By ID
- **`shipstation-pp-cli pickups list-scheduled`** - List all pickups that have been scheduled for this carrier
- **`shipstation-pp-cli pickups schedule`** - Schedule a package pickup with a carrier

### products

Manage products in your ShipStation account. Products represent the items you sell and ship to customers.

- **`shipstation-pp-cli products list`** - List products

### purchase-orders

Create and manage purchase orders from suppliers to replenish inventory. Track order status, receive products, and update inventory levels automatically.

- **`shipstation-pp-cli purchase-orders create`** - Create a new purchase order with products from a supplier.
- **`shipstation-pp-cli purchase-orders get`** - Retrieve detailed information about a specific purchase order including all products.
- **`shipstation-pp-cli purchase-orders list`** - Retrieve a paginated list of purchase orders with optional filtering by status, warehouse, dates, and other criteria.
- **`shipstation-pp-cli purchase-orders update`** - Update an existing purchase order.

Editing limitations:
- In the `draft` status, all fields can be edited.
- Once a purchase order moves to any other status (open, receiving, received, cancelled, or closed),
updating via PUT is no longer allowed.
- For purchase orders in `open` status, use the POST /v2/purchase_orders/{purchase_order_id}/shipping_details endpoint to update shipping-related details.

### rates

Quickly compare rates using the Rates endpoint. You can see and compare rates for the carriers connected to your account (as long as they support sending rates).

- **`shipstation-pp-cli rates calculate`** - It's not uncommon that you want to give your customer the choice between whether they want to ship the fastest, cheapest, or the most trusted route. Most companies don't solely ship things using a single shipping option;
so we provide functionality to show you all your options!
- **`shipstation-pp-cli rates estimate`** - Get Rate Estimates
- **`shipstation-pp-cli rates get-by-id`** - Retrieve a previously queried rate by its ID

### shipments

Shipments are at the core of most ShipStation capabilities. Shipment objects are required for cretaing labels and manifests, as well as getting rates.

- **`shipstation-pp-cli shipments assign-user-to`** - Assigns a user to one or more shipments. You can assign a single user
to up to 500 shipments at once.
- **`shipstation-pp-cli shipments create`** - Create one or more shipments
- **`shipstation-pp-cli shipments get-by-external-id`** - Query Shipments created using your own custom ID convention using this endpoint
- **`shipstation-pp-cli shipments get-by-id`** - Get an individual shipment based on its ID
- **`shipstation-pp-cli shipments list`** - Get list of Shipments
- **`shipstation-pp-cli shipments update`** - Update an existing shipment's details including addresses, package information,
carrier service, and shipping options. Use this endpoint to modify shipment
information before purchasing a label.

Note: The following fields are read-only and cannot be updated: shipment_id,
created_at, modified_at, shipment_status, tags, and total_weight.

### suppliers

Manage supplier information including contact details, email addresses, and physical addresses. Suppliers are used when creating purchase orders.

- **`shipstation-pp-cli suppliers create`** - Create a new supplier with contact and address information.
- **`shipstation-pp-cli suppliers get`** - Retrieve detailed information about a specific supplier.
- **`shipstation-pp-cli suppliers list`** - Retrieve a paginated list of all suppliers with optional filtering by supplier name.
- **`shipstation-pp-cli suppliers update`** - Update an existing supplier's information.

### tags

Tags are text-based identifiers you can add to shipments to help in your shipment management workflows.

- **`shipstation-pp-cli tags create`** - Create a new Tag for customizing how you track your shipments
- **`shipstation-pp-cli tags list`** - Get a list of all tags associated with an account.

### totes

Manage totes (bins or containers) used in warehouse picking and packing operations. Create, update, delete totes and track tote quantities by warehouse.

- **`shipstation-pp-cli totes create-batch`** - Create multiple totes at once. Returns both successfully created totes and any failures.
- **`shipstation-pp-cli totes delete`** - Delete a tote by its ID.
- **`shipstation-pp-cli totes get-by-id`** - Retrieve details of a specific tote.
- **`shipstation-pp-cli totes get-quantities`** - Get the number of totes in each warehouse.
- **`shipstation-pp-cli totes list`** - Get all totes for the seller, optionally filtered by warehouse.
- **`shipstation-pp-cli totes update`** - Update the name or barcode of an existing tote.

### tracking

Use the tracking endpoint to stop receiving tracking updates (more dedicated tracking endpoint methods coming soon).

- **`shipstation-pp-cli tracking stop`** - Unsubscribe from tracking updates for a package.

### users

Manage and retrieve user information for the ShipStation account. This endpoint allows you to list users with various filtering options.

- **`shipstation-pp-cli users list`** - List users

### warehouses

Get warehouse details like warehouse ID and related addresses using the warehouses endpoint.

- **`shipstation-pp-cli warehouses get-by-id`** - Retrieve warehouse data based on the warehouse ID
- **`shipstation-pp-cli warehouses list`** - Retrieve a list of warehouses associated with this account.


## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
shipstation-pp-cli batches list

# JSON for scripting and agents
shipstation-pp-cli batches list --json

# Filter to specific fields
shipstation-pp-cli batches list --json --select id,name,status

# Dry run — show the request without sending
shipstation-pp-cli batches list --dry-run

# Agent mode — JSON + compact + no prompts in one flag
shipstation-pp-cli batches list --agent
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

Exit codes: `0` success, `2` usage error, `3` not found, `4` auth error, `5` API error, `7` rate limited, `10` config error.

## Use with Claude Code

Install the focused skill — it auto-installs the CLI on first invocation:

```bash
npx skills add mvanhorn/printing-press-library/cli-skills/pp-shipstation -g
```

Then invoke `/pp-shipstation <query>` in Claude Code. The skill is the most efficient path — Claude Code drives the CLI directly without an MCP server in the middle.

<details>
<summary>Use as an MCP server in Claude Code (advanced)</summary>

If you'd rather register this CLI as an MCP server in Claude Code, install the MCP binary first:


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Then register it:

```bash
claude mcp add shipstation shipstation-pp-mcp -e SHIPSTATION_API_KEY=<your-key>
```

</details>

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/shipstation-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `SHIPSTATION_API_KEY` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "shipstation": {
      "command": "shipstation-pp-mcp",
      "env": {
        "SHIPSTATION_API_KEY": "<your-key>"
      }
    }
  }
}
```

</details>

## Health Check

```bash
shipstation-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Config file: `~/.config/shipstation-pp-cli/config.toml`

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `SHIPSTATION_API_KEY` | per_call | Yes | Set to your API credential. |

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `shipstation-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $SHIPSTATION_API_KEY`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

### API-specific

- **HTTP 429 with `Retry-After` header** — ShipStation rate limit hit. Default is 200 req/min on production, 20 on sandbox. Re-run after the seconds in `Retry-After`, or pass `--rate-limit 100` to throttle preemptively.
- **401 Unauthorized on every call** — The `SHIPSTATION_API_KEY` env var is missing, expired, or pointing at the wrong environment. Generate a new key in app settings; verify with `shipstation-pp-cli doctor`.
- **`rate-history compare` returns empty** — Rates persist only when you run `rates calculate` or `rates estimate` through this CLI. Run a few rate calls first, or run `sync --resource rates` if you have realized rates from labels.
- **`batch retry --only-errored` aborts with 'no errored sub-shipments found'** — Run `batches list-errors <batch-id>` first to populate the local batch_errors table. Errors are only fetched once when you list them.

---

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**shipstation/mcp-shipstation-api**](https://github.com/shipstation/mcp-shipstation-api) — JavaScript
- [**rip-technologies/shipstation-node**](https://github.com/rip-technologies/shipstation-node) — TypeScript
- [**ShipEngine/shipengine-python**](https://github.com/ShipEngine/shipengine-python) — Python
- [**AustinBratcher/shipengine-js**](https://github.com/AustinBratcher/shipengine-js) — JavaScript
- [**kjaenicke/shipstation-node**](https://github.com/kjaenicke/shipstation-node) — JavaScript
- [**JustMaier/node-shipstation**](https://github.com/JustMaier/node-shipstation) — JavaScript
- [**mattcoatsworth/shipstation-mcp-server**](https://github.com/mattcoatsworth/shipstation-mcp-server) — JavaScript
- [**CDataSoftware/shipstation-mcp-server-by-cdata**](https://github.com/CDataSoftware/shipstation-mcp-server-by-cdata) — JavaScript

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
