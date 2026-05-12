// Copyright 2026 james-bongiovanni. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/commerce/shipstation/internal/store"

	"github.com/spf13/cobra"
)

func newBatchRetryCmd(flags *rootFlags) *cobra.Command {
	var onlyErrored bool

	cmd := &cobra.Command{
		Use:   "retry <batch-id>",
		Short: "Re-process only the errored sub-shipments in a batch.",
		Example: strings.Trim(`
  shipstation-pp-cli batches retry 550e8400-e29b-41d4-a716-446655440000 --only-errored
  shipstation-pp-cli batches retry 550e8400-e29b-41d4-a716-446655440000 --only-errored --dry-run
`, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if !onlyErrored {
				return usageErr(fmt.Errorf("currently only --only-errored is supported"))
			}
			batchID := args[0]

			path := "/v2/batches/{batch_id}/process/labels"
			path = replacePathParam(path, "batch_id", batchID)

			// In dry-run, skip DB + API entirely so the command works on a
			// brand-new install with no synced data and no live credentials.
			if dryRunOK(flags) {
				envelope := map[string]any{
					"action":               "post",
					"resource":             "process",
					"path":                 path,
					"dry_run":              true,
					"data":                 map[string]any{},
					"errored_subshipments": 0,
				}
				raw, _ := json.Marshal(envelope)
				return printOutputWithFlags(cmd.OutOrStdout(), raw, flags)
			}

			// Resolve errored sub-shipment IDs from the local store.
			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("shipstation-pp-cli"))
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			subIDs, err := erroredSubShipmentIDs(db, batchID)
			if err != nil {
				return fmt.Errorf("reading errors for batch %s: %w", batchID, err)
			}

			body := map[string]any{}
			if len(subIDs) > 0 {
				body["shipment_ids"] = subIDs
			}

			c, err := flags.newClient()
			if err != nil {
				return err
			}
			data, statusCode, err := c.Post(path, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			envelope := map[string]any{
				"action":               "post",
				"resource":             "process",
				"path":                 path,
				"status":               statusCode,
				"success":              statusCode >= 200 && statusCode < 300,
				"errored_subshipments": len(subIDs),
			}
			if len(data) > 0 {
				var parsed any
				if err := json.Unmarshal(data, &parsed); err == nil {
					envelope["data"] = parsed
				}
			}
			raw, _ := json.Marshal(envelope)
			return printOutputWithFlags(cmd.OutOrStdout(), raw, flags)
		},
	}

	cmd.Flags().BoolVar(&onlyErrored, "only-errored", false, "Only retry errored sub-shipments (currently the only supported mode)")
	return cmd
}

func erroredSubShipmentIDs(db *store.Store, batchID string) ([]string, error) {
	rows, err := db.DB().Query(`SELECT data FROM errors WHERE batches_id = ?`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var dataStr string
		if err := rows.Scan(&dataStr); err != nil {
			return nil, err
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(dataStr), &raw); err != nil {
			continue
		}
		// Try common keys; the shape of error data varies by carrier.
		for _, key := range []string{"external_shipment_id", "shipment_id", "subshipment_id", "ship_id"} {
			if v, ok := raw[key]; ok {
				if s, ok := v.(string); ok && s != "" && !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
