// Copyright 2026 james-bongiovanni. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/commerce/shipstation/internal/store"

	"github.com/spf13/cobra"
)

type stuckShipment struct {
	ID                 string `json:"id"`
	Status             string `json:"status,omitempty"`
	WarehouseID        string `json:"warehouse_id,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	AgeMinutes         int    `json:"age_minutes"`
	ExternalShipmentID string `json:"external_shipment_id,omitempty"`
}

type orphansResult struct {
	Stuck   []stuckShipment `json:"stuck"`
	Missing []string        `json:"missing"`
}

func newOrphansCmd(flags *rootFlags) *cobra.Command {
	var externalIDsFile, stuckStr string

	cmd := &cobra.Command{
		Use:   "orphans",
		Short: "Find shipments stuck without a label, plus optional cross-check of OMS-side IDs.",
		Example: strings.Trim(`
  shipstation-pp-cli orphans --stuck 4h
  shipstation-pp-cli orphans --external-ids ./oms-ids.txt --stuck 24h --json
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), orphansResult{Stuck: []stuckShipment{}, Missing: []string{}}, flags)
			}
			if stuckStr == "" {
				stuckStr = "4h"
			}
			stuckDur, err := time.ParseDuration(stuckStr)
			if err != nil {
				return usageErr(fmt.Errorf("invalid --stuck %q: %w", stuckStr, err))
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("shipstation-pp-cli"))
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			stuck, err := findStuckShipments(db, stuckDur)
			if err != nil {
				return fmt.Errorf("finding stuck shipments: %w", err)
			}

			missing := []string{}
			if externalIDsFile != "" {
				ids, err := readExternalIDsFile(externalIDsFile)
				if err != nil {
					return fmt.Errorf("reading %s: %w", externalIDsFile, err)
				}
				known, err := knownExternalIDs(db)
				if err != nil {
					return fmt.Errorf("scanning shipments: %w", err)
				}
				for _, id := range ids {
					if !known[id] {
						missing = append(missing, id)
					}
				}
				sort.Strings(missing)
			}

			result := orphansResult{Stuck: stuck, Missing: missing}
			if result.Stuck == nil {
				result.Stuck = []stuckShipment{}
			}
			if result.Missing == nil {
				result.Missing = []string{}
			}

			if flags.asJSON || flags.csv || flags.compact || flags.quiet || flags.selectFields != "" || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), result, flags)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Stuck (no label after %s): %d\n", stuckDur, len(result.Stuck))
			for _, s := range result.Stuck {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-36s %-20s %dmin\n", s.ID, s.Status, s.AgeMinutes)
			}
			if externalIDsFile != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\nMissing from ShipStation: %d\n", len(result.Missing))
				for _, id := range result.Missing {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", id)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&externalIDsFile, "external-ids", "", "Newline-delimited file of external IDs to cross-check against ShipStation")
	cmd.Flags().StringVar(&stuckStr, "stuck", "4h", "Duration shipments must be without a label to count as stuck (e.g. 4h, 24h)")
	return cmd
}

func findStuckShipments(db *store.Store, stuck time.Duration) ([]stuckShipment, error) {
	cutoff := time.Now().Add(-stuck)
	q := `
		SELECT id, data
		FROM resources
		WHERE resource_type = 'shipments'
		  AND id NOT IN (
		    SELECT json_extract(data, '$.shipment_id')
		    FROM resources
		    WHERE resource_type = 'labels'
		      AND json_extract(data, '$.shipment_id') IS NOT NULL
		  )`
	rows, err := db.DB().Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []stuckShipment
	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return nil, err
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(dataStr), &raw); err != nil {
			continue
		}
		createdStr := stringField(raw, "created_at")
		t, terr := time.Parse(time.RFC3339, createdStr)
		if terr != nil {
			t, terr = time.Parse("2006-01-02 15:04:05", createdStr)
		}
		if terr != nil {
			continue
		}
		if !t.Before(cutoff) {
			continue
		}
		status := stringField(raw, "shipment_status")
		if status == "" {
			status = stringField(raw, "status")
		}
		out = append(out, stuckShipment{
			ID:                 id,
			Status:             status,
			WarehouseID:        stringField(raw, "warehouse_id"),
			CreatedAt:          createdStr,
			AgeMinutes:         int(time.Since(t).Minutes()),
			ExternalShipmentID: stringField(raw, "external_shipment_id"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgeMinutes > out[j].AgeMinutes })
	return out, nil
}

func readExternalIDsFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var out []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out, scanner.Err()
}

func knownExternalIDs(db *store.Store) (map[string]bool, error) {
	rows, err := db.DB().Query(`
		SELECT json_extract(data, '$.external_shipment_id')
		FROM resources
		WHERE resource_type = 'shipments'
		  AND json_extract(data, '$.external_shipment_id') IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != "" {
			out[id] = true
		}
	}
	return out, rows.Err()
}
