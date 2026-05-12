// Copyright 2026 james-bongiovanni. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/commerce/shipstation/internal/store"

	"github.com/spf13/cobra"
)

func newRateHistoryCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "rate-history",
		Short:       "Replay persisted rate quotes to compare carriers across a date range.",
		Annotations: map[string]string{"mcp:read-only": "true"},
	}
	cmd.AddCommand(newRateHistoryCompareCmd(flags))
	return cmd
}

type rateCompareRow struct {
	ShipDate       string  `json:"ship_date"`
	CarrierCode    string  `json:"carrier_code"`
	ServiceCode    string  `json:"service_code"`
	ShippingAmount float64 `json:"shipping_amount"`
	AltCarrier     string  `json:"alt_carrier"`
	AltService     string  `json:"alt_service"`
	AltAmount      float64 `json:"alt_amount"`
	Delta          float64 `json:"delta"`
	DeltaPct       float64 `json:"delta_pct"`
	PackageType    string  `json:"package_type,omitempty"`
	Zone           int     `json:"zone,omitempty"`
}

func newRateHistoryCompareCmd(flags *rootFlags) *cobra.Command {
	var fromDate, toDate, carrier, vsCarrier string

	cmd := &cobra.Command{
		Use:   "compare",
		Short: "Compare a carrier's stored rates against another carrier's quotes for the same shipments.",
		Example: strings.Trim(`
  shipstation-pp-cli rate-history compare --from 2026-04-01 --to 2026-04-30 --carrier ups_ground --vs fedex_home_delivery
  shipstation-pp-cli rate-history compare --from 2026-04-01 --to 2026-04-30 --carrier ups_ground --vs fedex_home_delivery --json
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), []rateCompareRow{}, flags)
			}

			if carrier == "" || vsCarrier == "" || fromDate == "" || toDate == "" {
				return cmd.Help()
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("shipstation-pp-cli"))
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			rows, err := queryRateCompare(db.DB(), fromDate, toDate, carrier, vsCarrier)
			if err != nil {
				return fmt.Errorf("querying rates: %w", err)
			}

			if len(rows) == 0 {
				fmt.Fprintln(os.Stderr, "no rates stored for this range — run 'rates calculate' or 'rates estimate' to populate")
				return printJSONFiltered(cmd.OutOrStdout(), []rateCompareRow{}, flags)
			}

			if flags.asJSON || flags.csv || flags.compact || flags.quiet || flags.selectFields != "" || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-22s %-22s %10s %10s %10s %8s\n",
				"SHIP_DATE", "CARRIER/SVC", "ALT/SVC", "AMOUNT", "ALT", "DELTA", "DELTA%")
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-22s %-22s %10.2f %10.2f %10.2f %7.1f%%\n",
					r.ShipDate,
					truncate(r.CarrierCode+"/"+r.ServiceCode, 22),
					truncate(r.AltCarrier+"/"+r.AltService, 22),
					r.ShippingAmount, r.AltAmount, r.Delta, r.DeltaPct)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&fromDate, "from", "", "Start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&toDate, "to", "", "End date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&carrier, "carrier", "", "Primary carrier code (the one you actually shipped with)")
	cmd.Flags().StringVar(&vsCarrier, "vs", "", "Comparison carrier code")
	return cmd
}

func queryRateCompare(db *sql.DB, fromDate, toDate, carrier, vs string) ([]rateCompareRow, error) {
	// Pull primary carrier rates first.
	primaryQ := `
		SELECT COALESCE(ship_date, ''), COALESCE(carrier_code, ''), COALESCE(service_code, ''),
		       COALESCE(shipping_amount, ''), COALESCE(package_type, ''), COALESCE(zone, 0)
		FROM rates
		WHERE carrier_code = ?
		  AND ship_date IS NOT NULL
		  AND date(ship_date) BETWEEN date(?) AND date(?)
		ORDER BY ship_date ASC`
	rows, err := db.Query(primaryQ, carrier, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var primaries []rateCompareRow
	for rows.Next() {
		var sd, cc, sc, amt, pkg string
		var zone int
		if err := rows.Scan(&sd, &cc, &sc, &amt, &pkg, &zone); err != nil {
			return nil, err
		}
		// Normalize ship_date to YYYY-MM-DD slice.
		if len(sd) >= 10 {
			sd = sd[:10]
		}
		primaries = append(primaries, rateCompareRow{
			ShipDate:       sd,
			CarrierCode:    cc,
			ServiceCode:    sc,
			ShippingAmount: floatFromString(amt),
			PackageType:    pkg,
			Zone:           zone,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(primaries) == 0 {
		return nil, nil
	}

	// For each primary, find the best alternate quote on the same ship_date,
	// preferring same package_type then same zone.
	altQ := `
		SELECT COALESCE(service_code, ''), COALESCE(shipping_amount, ''),
		       COALESCE(package_type, ''), COALESCE(zone, 0)
		FROM rates
		WHERE carrier_code = ?
		  AND ship_date IS NOT NULL
		  AND date(ship_date) = date(?)
		ORDER BY
		  CASE WHEN package_type = ? THEN 0 ELSE 1 END,
		  CASE WHEN zone = ? THEN 0 ELSE 1 END
		LIMIT 1`

	out := make([]rateCompareRow, 0, len(primaries))
	for _, p := range primaries {
		var altSc, altAmt, altPkg string
		var altZone int
		err := db.QueryRow(altQ, vs, p.ShipDate, p.PackageType, p.Zone).Scan(&altSc, &altAmt, &altPkg, &altZone)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		altAmount := floatFromString(altAmt)
		row := p
		row.AltCarrier = vs
		row.AltService = altSc
		row.AltAmount = altAmount
		row.Delta = altAmount - p.ShippingAmount
		if p.ShippingAmount > 0 {
			row.DeltaPct = (row.Delta / p.ShippingAmount) * 100
		}
		out = append(out, row)
	}

	return out, nil
}
