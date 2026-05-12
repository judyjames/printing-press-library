// Copyright 2026 james-bongiovanni. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/commerce/shipstation/internal/store"

	"github.com/spf13/cobra"
)

type velocityRow struct {
	SKU               string  `json:"sku"`
	WarehouseID       string  `json:"warehouse_id,omitempty"`
	ShippedLastWindow int64   `json:"shipped_last_window"`
	VelocityPerDay    float64 `json:"velocity_per_day"`
	OnHand            int64   `json:"on_hand"`
	DaysCover         float64 `json:"days_cover"`
}

func newInventoryVelocityCmd(flags *rootFlags) *cobra.Command {
	var daysCover, windowDays int
	var warehouse string

	cmd := &cobra.Command{
		Use:   "velocity",
		Short: "Compute days-of-cover per SKU from recent shipped units vs current on-hand inventory.",
		Example: strings.Trim(`
  shipstation-pp-cli inventory velocity
  shipstation-pp-cli inventory velocity --days-cover 7 --window-days 30
  shipstation-pp-cli inventory velocity --warehouse wh_123 --json
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), []velocityRow{}, flags)
			}
			if windowDays <= 0 {
				windowDays = 30
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("shipstation-pp-cli"))
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			cutoff := time.Now().AddDate(0, 0, -windowDays).Format("2006-01-02")
			shipped, err := shippedUnitsBySKU(db, cutoff, warehouse)
			if err != nil {
				return fmt.Errorf("scanning shipments: %w", err)
			}

			onHand, err := onHandBySKU(db, warehouse)
			if err != nil {
				return fmt.Errorf("scanning inventory: %w", err)
			}

			rows := buildVelocityRows(shipped, onHand, windowDays, daysCover)

			if flags.asJSON || flags.csv || flags.compact || flags.quiet || flags.selectFields != "" || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no SKUs at risk within the requested days-cover")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-30s %-15s %12s %12s %10s %10s\n",
				"SKU", "WAREHOUSE", "SHIPPED", "VEL/DAY", "ON_HAND", "DAYS_COVER")
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-30s %-15s %12d %12.2f %10d %10.1f\n",
					truncate(r.SKU, 30), truncate(r.WarehouseID, 15),
					r.ShippedLastWindow, r.VelocityPerDay, r.OnHand, r.DaysCover)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&daysCover, "days-cover", 14, "Flag SKUs with fewer than this many days of cover")
	cmd.Flags().IntVar(&windowDays, "window-days", 30, "Window (in days) of shipped history to use for velocity")
	cmd.Flags().StringVar(&warehouse, "warehouse", "", "Restrict to a single warehouse_id")
	return cmd
}

type skuKey struct {
	sku string
	wh  string
}

func shippedUnitsBySKU(db *store.Store, cutoff, warehouse string) (map[skuKey]int64, error) {
	rows, err := db.DB().Query(`
		SELECT data
		FROM resources
		WHERE resource_type = 'shipments'
		  AND (
		    json_extract(data, '$.created_at') IS NULL
		    OR date(json_extract(data, '$.created_at')) >= date(?)
		  )`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[skuKey]int64{}
	for rows.Next() {
		var dataStr string
		if err := rows.Scan(&dataStr); err != nil {
			return nil, err
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(dataStr), &raw); err != nil {
			continue
		}
		wh := stringField(raw, "warehouse_id")
		if warehouse != "" && wh != warehouse {
			continue
		}
		items, _ := raw["items"].([]any)
		for _, it := range items {
			obj, ok := it.(map[string]any)
			if !ok {
				continue
			}
			sku := stringField(obj, "sku")
			if sku == "" {
				continue
			}
			var qty int64
			switch v := obj["quantity"].(type) {
			case float64:
				qty = int64(v)
			case string:
				qty, _ = strconv.ParseInt(v, 10, 64)
			}
			if qty <= 0 {
				qty = 1
			}
			out[skuKey{sku: sku, wh: wh}] += qty
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func onHandBySKU(db *store.Store, warehouse string) (map[skuKey]int64, error) {
	rows, err := db.DB().Query(`SELECT data FROM inventory`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[skuKey]int64{}
	for rows.Next() {
		var dataStr string
		if err := rows.Scan(&dataStr); err != nil {
			return nil, err
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(dataStr), &raw); err != nil {
			continue
		}
		sku := stringField(raw, "sku")
		if sku == "" {
			continue
		}
		wh := stringField(raw, "warehouse_id")
		if warehouse != "" && wh != warehouse {
			continue
		}
		var qty int64
		switch v := raw["on_hand"].(type) {
		case float64:
			qty = int64(v)
		case string:
			qty, _ = strconv.ParseInt(v, 10, 64)
		}
		if qty == 0 {
			if v, ok := raw["available"].(float64); ok {
				qty = int64(v)
			}
		}
		out[skuKey{sku: sku, wh: wh}] = qty
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func buildVelocityRows(shipped, onHand map[skuKey]int64, windowDays, daysCoverThreshold int) []velocityRow {
	keys := map[skuKey]bool{}
	for k := range shipped {
		keys[k] = true
	}
	out := make([]velocityRow, 0, len(keys))
	for k := range keys {
		s := shipped[k]
		oh := onHand[k]
		if oh == 0 {
			oh = onHand[skuKey{sku: k.sku}]
		}
		velocity := float64(s) / float64(windowDays)
		var dc float64
		if velocity > 0 {
			dc = float64(oh) / velocity
		} else {
			dc = -1 // sentinel: no cover risk computable
		}
		if daysCoverThreshold > 0 && (dc < 0 || dc >= float64(daysCoverThreshold)) {
			continue
		}
		out = append(out, velocityRow{
			SKU:               k.sku,
			WarehouseID:       k.wh,
			ShippedLastWindow: s,
			VelocityPerDay:    velocity,
			OnHand:            oh,
			DaysCover:         dc,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		// Ascending days_cover, but treat -1 (no risk computable) as last.
		ai := out[i].DaysCover
		aj := out[j].DaysCover
		if ai < 0 && aj < 0 {
			return out[i].SKU < out[j].SKU
		}
		if ai < 0 {
			return false
		}
		if aj < 0 {
			return true
		}
		return ai < aj
	})
	return out
}

// stringFieldFromAny is reserved for future field-extraction use; suppress unused.
var _ = strings.TrimSpace
