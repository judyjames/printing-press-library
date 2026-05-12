# Shipcheck Report — shipstation-pp-cli

## Verdict: PASS (6/6 legs)

## Leg results

| Leg | Result | Exit | Elapsed |
|-----|--------|------|---------|
| dogfood | PASS | 0 | 2.343s |
| verify | PASS | 0 | 5.909s |
| workflow-verify | PASS | 0 | 17ms |
| verify-skill | PASS | 0 | 1.048s |
| validate-narrative | PASS | 0 | 261ms |
| scorecard | PASS | 0 | 218ms |

## Verify pass rate
- Before fix loop: 100% (39/39)
- After fix loop: 100% (39/39)

## Scorecard
- Total: 84/100 — Grade A
- 100% on: Output Modes, Auth, Error Handling, Doctor, Agent Native, MCP Quality, Local Cache, Breadth, Workflows, Insight, Path Validity, Sync Correctness, Dead Code
- Lowest dimensions:
  - MCP Surface Strategy: 2/10 — generator emitted 101 endpoint-mirror tools without the Cloudflare-pattern (`mcp.transport: [stdio, http]`, `mcp.orchestration: code`, `mcp.endpoint_tools: hidden`). The generator warned about this at generate time but does not expose a CLI flag to enable it for OpenAPI specs. Captured for retro.
  - MCP Tool Design: 5/10
  - MCP Remote Transport: 5/10
  - MCP Token Efficiency: 7/10
  - Cache Freshness: 5/10
  - Type Fidelity: 3/5
  - Auth Protocol: 8/10
  - Data Pipeline Integrity: 7/10
  - README: 8/10
  - Vision: 8/10

## Sample Output Probe
- 8/8 (100% pass rate) on novel commands.

## Top blockers found and fixed (one fix loop)

1. **Migration failure in store.go: `near "add": syntax error`.** Generator named several SQLite tables after reserved words / sub-resource leaf names (`add`, `errors`, `process`, `remove`, `options`, `services`, `return`, `track`, `void`, `documents`, `receives`, `status`, `cancel`). Patched: wrapped each occurrence in double-quoted SQLite identifiers across CREATE TABLE / CREATE INDEX / INSERT / UPDATE / FROM / JOIN / DELETE FROM. 40 substitutions in `internal/store/store.go`. Backup at `store.go.bak`.

2. **Narrative referenced `batch triage` and `batch retry` (singular).** Actual commands are `batches triage` and `batches retry` (subcommands of `batches` parent). Fixed in `research.json` `novel_features`, `narrative.value_prop`, `narrative.troubleshoots`. Re-run of `validate-narrative` PASSed with 11/11 commands resolved.

3. **Narrative quickstart used `sync --resource` (singular) and `--since 2026-04-01` (date).** Actual sync uses `--resources` (plural) and `--since` takes a duration like `30d`. Fixed in `research.json`.

## Known gaps (acknowledged, not blocking)

- **MCP architectural strategy.** Default endpoint-mirror surface for 101 tools is past the recommended 50-tool threshold. Recommended Cloudflare-pattern enrichment requires a generator-level path the current binary doesn't expose for OpenAPI specs. Polish (Phase 5.5) may flag this; if it does, will fix; otherwise will accept the scorecard hit on three MCP architectural dimensions.
- **Four POST endpoints have skipped request bodies** (oneOf/anyOf in spec): POST /v2/batches, /v2/manifests, /v2/rates, /v2/rates/estimate. These commands still work but accept no typed body fields; users must send via the generic HTTP path or wait for a generator improvement.

## Final ship recommendation: ship

Verify pass-rate 100%, scorecard 84/100 Grade A, all 6 shipcheck legs green, 8/8 novel feature probes PASS. No known broken flagship features. Ready for Phase 5 dogfood + Phase 5.5 polish.
