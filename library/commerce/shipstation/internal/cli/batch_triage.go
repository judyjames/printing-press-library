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

type batchTriageRow struct {
	BatchID       string `json:"batch_id"`
	ErrorCode     string `json:"error_code"`
	Count         int    `json:"count"`
	OldestAgeMins int    `json:"oldest_age_minutes"`
	SampleMessage string `json:"sample_message"`
}

func newBatchTriageCmd(flags *rootFlags) *cobra.Command {
	var ageStr, reason string

	cmd := &cobra.Command{
		Use:   "triage",
		Short: "List batches with unresolved errored sub-shipments, grouped by error code and aged.",
		Example: strings.Trim(`
  shipstation-pp-cli batches triage
  shipstation-pp-cli batches triage --age 2h --reason address
  shipstation-pp-cli batches triage --json
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), []batchTriageRow{}, flags)
			}

			var minAge time.Duration
			if ageStr != "" {
				d, err := time.ParseDuration(ageStr)
				if err != nil {
					return usageErr(fmt.Errorf("invalid --age %q: %w", ageStr, err))
				}
				minAge = d
			}

			db, err := store.OpenWithContext(cmd.Context(), defaultDBPath("shipstation-pp-cli"))
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			rows, err := triageErrors(db, minAge, reason)
			if err != nil {
				return fmt.Errorf("scanning errors: %w", err)
			}

			if flags.asJSON || flags.csv || flags.compact || flags.quiet || flags.selectFields != "" || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no errors match the given filters")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-36s %-30s %6s %12s  %s\n",
				"BATCH_ID", "ERROR_CODE", "COUNT", "OLDEST(MIN)", "SAMPLE")
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-36s %-30s %6d %12d  %s\n",
					truncate(r.BatchID, 36), truncate(r.ErrorCode, 30),
					r.Count, r.OldestAgeMins, truncate(r.SampleMessage, 60))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&ageStr, "age", "", "Only include errors older than this duration (e.g. 2h, 24h)")
	cmd.Flags().StringVar(&reason, "reason", "", "Substring to match against error code or message")
	return cmd
}

func triageErrors(db *store.Store, minAge time.Duration, reason string) ([]batchTriageRow, error) {
	q := `
		SELECT e.batches_id, e.data, e.synced_at
		FROM errors e`
	rows, err := db.DB().Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type groupKey struct {
		batchID string
		errCode string
	}
	type group struct {
		count  int
		oldest time.Time
		sample string
	}
	groups := map[groupKey]*group{}

	now := time.Now()
	reasonLower := strings.ToLower(reason)

	for rows.Next() {
		var batchID, dataStr, syncedStr string
		if err := rows.Scan(&batchID, &dataStr, &syncedStr); err != nil {
			return nil, err
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(dataStr), &raw); err != nil {
			continue
		}

		errCode := stringField(raw, "error_code")
		errMsg := stringField(raw, "message")
		if errMsg == "" {
			errMsg = stringField(raw, "error_message")
		}

		// Parse synced_at to compute age (approximation: when we recorded it).
		t, terr := time.Parse("2006-01-02 15:04:05", syncedStr)
		if terr != nil {
			t, terr = time.Parse(time.RFC3339, syncedStr)
		}
		if terr != nil {
			t = now
		}
		age := now.Sub(t)
		if minAge > 0 && age < minAge {
			continue
		}

		if reasonLower != "" {
			if !strings.Contains(strings.ToLower(errCode), reasonLower) &&
				!strings.Contains(strings.ToLower(errMsg), reasonLower) {
				continue
			}
		}

		k := groupKey{batchID: batchID, errCode: errCode}
		g, ok := groups[k]
		if !ok {
			g = &group{oldest: t, sample: errMsg}
			groups[k] = g
		}
		g.count++
		if t.Before(g.oldest) {
			g.oldest = t
		}
		if g.sample == "" && errMsg != "" {
			g.sample = errMsg
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]batchTriageRow, 0, len(groups))
	for k, g := range groups {
		ageMin := int(now.Sub(g.oldest).Minutes())
		out = append(out, batchTriageRow{
			BatchID:       k.batchID,
			ErrorCode:     k.errCode,
			Count:         g.count,
			OldestAgeMins: ageMin,
			SampleMessage: g.sample,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OldestAgeMins != out[j].OldestAgeMins {
			return out[i].OldestAgeMins > out[j].OldestAgeMins
		}
		return out[i].Count > out[j].Count
	})
	return out, nil
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
