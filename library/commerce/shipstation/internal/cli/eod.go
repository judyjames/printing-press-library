// Copyright 2026 james-bongiovanni. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/commerce/shipstation/internal/store"

	"github.com/spf13/cobra"
)

type eodSampleShipment struct {
	ID          string `json:"id"`
	Status      string `json:"status,omitempty"`
	WarehouseID string `json:"warehouse_id,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type eodGroupCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type eodResult struct {
	Date         string              `json:"date"`
	WarehouseID  string              `json:"warehouse_id,omitempty"`
	TotalUnlabel int                 `json:"total_unlabeled"`
	Sample       []eodSampleShipment `json:"sample"`
	ByStatus     []eodGroupCount     `json:"by_status"`
}

func newEodCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "eod",
		Short:       "End-of-day operational rollups (e.g., burndown of unlabeled shipments).",
		Annotations: map[string]string{"mcp:read-only": "true"},
	}
	cmd.AddCommand(newEodBurndownCmd(flags))
	return cmd
}

func newEodBurndownCmd(flags *rootFlags) *cobra.Command {
	var warehouse, date string

	cmd := &cobra.Command{
		Use:   "burndown",
		Short: "Today's shipments without a label, with a sample and a status breakdown.",
		Example: strings.Trim(`
  shipstation-pp-cli eod burndown
  shipstation-pp-cli eod burndown --warehouse wh_123
  shipstation-pp-cli eod burndown --date 2026-04-30 --json
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), eodResult{Sample: []eodSampleShipment{}, ByStatus: []eodGroupCount{}}, flags)
			}
			if date == "" {
				date = time.Now().Format("2006-01-02")
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("shipstation-pp-cli"))
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			result, err := computeBurndown(db, date, warehouse)
			if err != nil {
				return fmt.Errorf("scanning shipments: %w", err)
			}

			if flags.asJSON || flags.csv || flags.compact || flags.quiet || flags.selectFields != "" || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), result, flags)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Burndown for %s", result.Date)
			if result.WarehouseID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " (warehouse %s)", result.WarehouseID)
			}
			fmt.Fprintf(cmd.OutOrStdout(), ": %d unlabeled shipment(s)\n", result.TotalUnlabel)
			if len(result.ByStatus) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "")
				fmt.Fprintln(cmd.OutOrStdout(), "By status:")
				for _, g := range result.ByStatus {
					fmt.Fprintf(cmd.OutOrStdout(), "  %-25s %d\n", g.Status, g.Count)
				}
			}
			if len(result.Sample) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "")
				fmt.Fprintln(cmd.OutOrStdout(), "Sample:")
				for _, s := range result.Sample {
					fmt.Fprintf(cmd.OutOrStdout(), "  %-36s %-20s %s\n", s.ID, s.Status, s.CreatedAt)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&warehouse, "warehouse", "", "Restrict to a single warehouse_id")
	cmd.Flags().StringVar(&date, "date", "", "Date (YYYY-MM-DD); defaults to today")
	return cmd
}

func computeBurndown(db *store.Store, date, warehouse string) (eodResult, error) {
	q := `
		SELECT id, data
		FROM resources
		WHERE resource_type = 'shipments'
		  AND date(json_extract(data, '$.created_at')) = date(?)
		  AND id NOT IN (
		    SELECT json_extract(data, '$.shipment_id')
		    FROM resources
	q := `
		SELECT id, data
		FROM resources
		WHERE resource_type = 'shipments'
		  AND date(json_extract(data, '$.created_at')) = date(?)
		  AND id NOT IN (
		    SELECT json_extract(data, '$.shipment_id')
		    FROM resources
		    WHERE resource_type = 'labels'
		      AND json_extract(data, '$.shipment_id') IS NOT NULL
		  )`
	rows, err := db.DB().Query(q, date)
	rows, err := db.DB().Query(q, date, date)
	if err != nil {
		return eodResult{}, err
	}
	defer rows.Close()

	statusCounts := map[string]int{}
	var sample []eodSampleShipment
	total := 0

	for rows.Next() {
		var id, dataStr string
		if err := rows.Scan(&id, &dataStr); err != nil {
			return eodResult{}, err
		}
		var raw map[string]any
		_ = json.Unmarshal([]byte(dataStr), &raw)
		wh := stringField(raw, "warehouse_id")
		if warehouse != "" && wh != warehouse {
			continue
		}
		total++
		status := stringField(raw, "shipment_status")
		if status == "" {
			status = stringField(raw, "status")
		}
		if status == "" {
			status = "unknown"
		}
		statusCounts[status]++
		if len(sample) < 10 {
			sample = append(sample, eodSampleShipment{
				ID:          id,
				Status:      status,
				WarehouseID: wh,
				CreatedAt:   stringField(raw, "created_at"),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return eodResult{}, err
	}

	groups := make([]eodGroupCount, 0, len(statusCounts))
	for s, c := range statusCounts {
		groups = append(groups, eodGroupCount{Status: s, Count: c})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Count > groups[j].Count })

	if sample == nil {
		sample = []eodSampleShipment{}
	}
	if groups == nil {
		groups = []eodGroupCount{}
	}

	return eodResult{
		Date:         date,
		WarehouseID:  warehouse,
		TotalUnlabel: total,
		Sample:       sample,
		ByStatus:     groups,
	}, nil
}
