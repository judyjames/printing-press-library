// Copyright 2026 james-bongiovanni. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/commerce/shipstation/internal/store"

	"github.com/spf13/cobra"
)

type driftRow struct {
	SKU            string `json:"sku"`
	WarehouseID    string `json:"warehouse_id,omitempty"`
	LocalOnHand    int64  `json:"local_on_hand"`
	ExternalOnHand int64  `json:"external_on_hand"`
	Delta          int64  `json:"delta"`
	Drifted        bool   `json:"drifted"`
}

type externalLevel struct {
	SKU         string
	OnHand      int64
	WarehouseID string
}

func newInventoryDriftCmd(flags *rootFlags) *cobra.Command {
	var vsFile, format string
	var threshold int

	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Diff ShipStation inventory levels against an external truth file (CSV or JSONL).",
		Example: strings.Trim(`
  shipstation-pp-cli inventory drift --vs ./oms-export.csv
  shipstation-pp-cli inventory drift --vs ./supplier.jsonl --threshold 5 --json
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				if vsFile == "" {
					fmt.Fprintln(os.Stderr, "would diff local inventory against --vs <file> (CSV or JSONL)")
				} else {
					fmt.Fprintf(os.Stderr, "would diff local inventory against %s (threshold=%d)\n", vsFile, threshold)
				}
				return printJSONFiltered(cmd.OutOrStdout(), []driftRow{}, flags)
			}
			if vsFile == "" {
				return usageErr(fmt.Errorf("--vs <file.csv|file.jsonl> is required"))
			}

			external, err := readExternalLevels(vsFile, format)
			if err != nil {
				return fmt.Errorf("reading %s: %w", vsFile, err)
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("shipstation-pp-cli"))
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			localBySku, err := localInventoryBySKU(db)
			if err != nil {
				return fmt.Errorf("scanning local inventory: %w", err)
			}

			rows := computeDrift(localBySku, external, threshold)

			if flags.asJSON || flags.csv || flags.compact || flags.quiet || flags.selectFields != "" || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no SKUs drifted beyond threshold")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-30s %-20s %10s %10s %10s\n", "SKU", "WAREHOUSE", "LOCAL", "EXTERNAL", "DELTA")
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-30s %-20s %10d %10d %10d\n",
					truncate(r.SKU, 30), truncate(r.WarehouseID, 20),
					r.LocalOnHand, r.ExternalOnHand, r.Delta)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&vsFile, "vs", "", "External truth file: .csv or .jsonl with sku, on_hand, warehouse_id")
	cmd.Flags().IntVar(&threshold, "threshold", 0, "Only emit rows whose absolute delta exceeds this value")
	cmd.Flags().StringVar(&format, "format", "", "Override format detection: csv | jsonl")
	return cmd
}

func readExternalLevels(path, format string) ([]externalLevel, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	switch format {
	case "csv":
		return parseExternalCSV(f)
	case "jsonl", "json":
		return parseExternalJSONL(f)
	default:
		// Fallback: try JSONL first, then CSV.
		return parseExternalJSONL(f)
	}
}

func parseExternalCSV(r io.Reader) ([]externalLevel, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	header := records[0]
	skuIdx, qtyIdx, whIdx := -1, -1, -1
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "sku":
			skuIdx = i
		case "on_hand", "onhand", "qty", "quantity":
			qtyIdx = i
		case "warehouse_id", "warehouse", "warehouseid":
			whIdx = i
		}
	}
	if skuIdx == -1 || qtyIdx == -1 {
		return nil, fmt.Errorf("CSV must have headers including 'sku' and 'on_hand' (or 'qty')")
	}
	out := make([]externalLevel, 0, len(records)-1)
	for _, row := range records[1:] {
		if skuIdx >= len(row) || qtyIdx >= len(row) {
			continue
		}
		qty, _ := strconv.ParseInt(strings.TrimSpace(row[qtyIdx]), 10, 64)
		lv := externalLevel{SKU: strings.TrimSpace(row[skuIdx]), OnHand: qty}
		if whIdx >= 0 && whIdx < len(row) {
			lv.WarehouseID = strings.TrimSpace(row[whIdx])
		}
		if lv.SKU != "" {
			out = append(out, lv)
		}
	}
	return out, nil
}

func parseExternalJSONL(r io.Reader) ([]externalLevel, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var out []externalLevel
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		sku := stringField(raw, "sku")
		if sku == "" {
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
			if v, ok := raw["qty"].(float64); ok {
				qty = int64(v)
			}
		}
		out = append(out, externalLevel{
			SKU:         sku,
			OnHand:      qty,
			WarehouseID: stringField(raw, "warehouse_id"),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type localLevel struct {
	OnHand      int64
	WarehouseID string
}

func localInventoryBySKU(db *store.Store) (map[string]localLevel, error) {
	rows, err := db.DB().Query(`SELECT data FROM inventory`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]localLevel{}
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
		key := sku
		wh := stringField(raw, "warehouse_id")
		if wh != "" {
			key = sku + "@" + wh
		}
		out[key] = localLevel{OnHand: qty, WarehouseID: wh}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func computeDrift(local map[string]localLevel, external []externalLevel, threshold int) []driftRow {
	out := make([]driftRow, 0)
	for _, ext := range external {
		key := ext.SKU
		if ext.WarehouseID != "" {
			key = ext.SKU + "@" + ext.WarehouseID
		}
		loc, ok := local[key]
		if !ok {
			loc = local[ext.SKU]
		}
		delta := loc.OnHand - ext.OnHand
		if int(math.Abs(float64(delta))) <= threshold {
			continue
		}
		wh := ext.WarehouseID
		if wh == "" {
			wh = loc.WarehouseID
		}
		out = append(out, driftRow{
			SKU:            ext.SKU,
			WarehouseID:    wh,
			LocalOnHand:    loc.OnHand,
			ExternalOnHand: ext.OnHand,
			Delta:          delta,
			Drifted:        true,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return absInt64(out[i].Delta) > absInt64(out[j].Delta)
	})
	return out
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
