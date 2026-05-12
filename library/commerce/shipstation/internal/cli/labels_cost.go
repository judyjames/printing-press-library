// Copyright 2026 james-bongiovanni. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/commerce/shipstation/internal/store"

	"github.com/spf13/cobra"
)

type labelsCostRow struct {
	CarrierID   string  `json:"carrier_id,omitempty"`
	ServiceCode string  `json:"service_code,omitempty"`
	Count       int     `json:"count"`
	TotalCost   float64 `json:"total_cost"`
	AvgCost     float64 `json:"avg_cost"`
	MinCost     float64 `json:"min_cost"`
	MaxCost     float64 `json:"max_cost"`
}

func newLabelsCostCmd(flags *rootFlags) *cobra.Command {
	var by, week, fromDate, toDate string

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Pivot labels by carrier / service with totals, averages, and ranges.",
		Example: strings.Trim(`
  shipstation-pp-cli labels cost
  shipstation-pp-cli labels cost --by carrier --week last
  shipstation-pp-cli labels cost --by carrier,service --from 2026-04-01 --to 2026-04-30 --json
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), []labelsCostRow{}, flags)
			}

			byFields := parseByFields(by)
			start, end := novelDateRange(week, fromDate, toDate)

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("shipstation-pp-cli"))
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			rows, err := pivotLabelsCost(db.DB(), byFields, start, end)
			if err != nil {
				return fmt.Errorf("querying labels: %w", err)
			}

			if flags.asJSON || flags.csv || flags.compact || flags.quiet || flags.selectFields != "" || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}
			if len(rows) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no labels found for %s to %s\n", start, end)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-30s %-25s %6s %12s %10s %10s %10s\n",
				"CARRIER", "SERVICE", "COUNT", "TOTAL", "AVG", "MIN", "MAX")
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-30s %-25s %6d %12.2f %10.2f %10.2f %10.2f\n",
					truncate(r.CarrierID, 30), truncate(r.ServiceCode, 25),
					r.Count, r.TotalCost, r.AvgCost, r.MinCost, r.MaxCost)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&by, "by", "carrier,service", "Group by: carrier, service, or carrier,service")
	cmd.Flags().StringVar(&week, "week", "current", "Time window: current | last | YYYY-WW (overridden by --from/--to)")
	cmd.Flags().StringVar(&fromDate, "from", "", "Start date YYYY-MM-DD (overrides --week)")
	cmd.Flags().StringVar(&toDate, "to", "", "End date YYYY-MM-DD (overrides --week)")
	return cmd
}

func parseByFields(s string) []string {
	if s == "" {
		return []string{"carrier", "service"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "carrier" || p == "service" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"carrier", "service"}
	}
	return out
}

func pivotLabelsCost(db *sql.DB, by []string, start, end string) ([]labelsCostRow, error) {
	q := `
		SELECT
			COALESCE(json_extract(data, '$.carrier_id'), ''),
			COALESCE(json_extract(data, '$.service_code'), ''),
			COALESCE(json_extract(data, '$.shipment_cost.amount'), json_extract(data, '$.shipment_cost'), '0'),
			COALESCE(json_extract(data, '$.insurance_cost.amount'), json_extract(data, '$.insurance_cost'), '0')
		FROM resources
		WHERE resource_type = 'labels'
		  AND (
		    json_extract(data, '$.created_at') IS NULL
		    OR date(json_extract(data, '$.created_at')) BETWEEN date(?) AND date(?)
		  )`
	rows, err := db.Query(q, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type stats struct {
		count                 int
		total, min, max, sumS float64
	}
	groups := map[string]*stats{}
	keys := map[string][2]string{} // group key → (carrier, service)

	wantCarrier := false
	wantService := false
	for _, b := range by {
		if b == "carrier" {
			wantCarrier = true
		}
		if b == "service" {
			wantService = true
		}
	}

	for rows.Next() {
		var carrier, service string
		var shipCostStr, insCostStr sql.NullString
		if err := rows.Scan(&carrier, &service, &shipCostStr, &insCostStr); err != nil {
			return nil, err
		}
		shipCost := floatFromString(shipCostStr.String)
		insCost := floatFromString(insCostStr.String)
		cost := shipCost + insCost

		var k string
		c, s := "", ""
		if wantCarrier {
			c = carrier
		}
		if wantService {
			s = service
		}
		k = c + "||" + s

		st, ok := groups[k]
		if !ok {
			st = &stats{min: cost, max: cost}
			groups[k] = st
			keys[k] = [2]string{c, s}
		}
		st.count++
		st.total += cost
		if cost < st.min {
			st.min = cost
		}
		if cost > st.max {
			st.max = cost
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]labelsCostRow, 0, len(groups))
	for k, st := range groups {
		avg := 0.0
		if st.count > 0 {
			avg = st.total / float64(st.count)
		}
		row := labelsCostRow{
			CarrierID:   keys[k][0],
			ServiceCode: keys[k][1],
			Count:       st.count,
			TotalCost:   st.total,
			AvgCost:     avg,
			MinCost:     st.min,
			MaxCost:     st.max,
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TotalCost > out[j].TotalCost
	})
	return out, nil
}
