package cli

// PATCH: novel-commands — see .printing-press-patches.json for the change-set rationale.

// pp:client-call — `earliest` calls OpenTable and Tock clients per venue
// through `internal/source/opentable` and `internal/source/tock`. Dogfood's
// reimplementation_check sibling-import regex doesn't match multi-segment
// `internal/source/...` paths. Documented carve-out per AGENTS.md.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/auth"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/opentable"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/tock"
)

type earliestRow struct {
	Venue     string  `json:"venue"`
	Network   string  `json:"network"`
	SlotAt    string  `json:"slot_at,omitempty"`
	Available bool    `json:"available"`
	Reason    string  `json:"reason,omitempty"`
	URL       string  `json:"url,omitempty"`
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
}

type earliestResponse struct {
	Venues    []string      `json:"venues"`
	Party     int           `json:"party"`
	Within    int           `json:"within_days"`
	Results   []earliestRow `json:"results"`
	QueriedAt string        `json:"queried_at"`
}

// newEarliestCmd computes "soonest open slot per venue across both networks"
// for a comma-separated list of restaurants. The crucial cross-network
// affordance: each venue may live on either OpenTable, Tock, or both —
// the command resolves the network heuristically (or via explicit
// network:slug prefix) and queries the right source.
func newEarliestCmd(flags *rootFlags) *cobra.Command {
	var (
		party   int
		within  string
		date    string
		tonight bool
	)
	cmd := &cobra.Command{
		Use:   "earliest <slug1,slug2,...>",
		Short: "Soonest open slot per venue across OpenTable and Tock",
		Long: "Across a comma-separated list of restaurant slugs, return the " +
			"earliest open slot per venue within `--within N days`. Slugs may be " +
			"network-prefixed (`opentable:le-bernardin`, `tock:alinea`) for " +
			"explicit routing, otherwise both networks are tried. Use `--tonight` " +
			"as shorthand for `--date <today> --within 1d`.",
		Example: "  table-reservation-goat-pp-cli earliest 'canlis,spinasse,altura' --party 6 --tonight --agent",
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			venues := splitCSV(args[0])
			if len(venues) == 0 {
				return fmt.Errorf("provide a comma-separated list of restaurant slugs")
			}
			// `--tonight` is shorthand for "today only." Mutually exclusive
			// with `--date` and overrides `--within`.
			if tonight {
				if date != "" {
					return fmt.Errorf("--tonight and --date are mutually exclusive")
				}
				date = time.Now().Format("2006-01-02")
				within = "1d"
			}
			withinDays := parseDays(within)
			if withinDays == 0 {
				withinDays = 14
			}
			if dryRunOK(flags) {
				rows := make([]earliestRow, 0, len(venues))
				for _, v := range venues {
					rows = append(rows, earliestRow{Venue: v, Network: "opentable", Available: false, Reason: "dry-run"})
				}
				return printJSONFiltered(cmd.OutOrStdout(), earliestResponse{
					Venues: venues, Party: party, Within: withinDays, Results: rows,
					QueriedAt: time.Now().UTC().Format(time.RFC3339),
				}, flags)
			}
			session, err := auth.Load()
			if err != nil {
				return fmt.Errorf("loading session: %w", err)
			}
			startDate := date
			if startDate == "" {
				startDate = time.Now().Format("2006-01-02")
			}
			ctx := cmd.Context()
			rows := make([]earliestRow, 0, len(venues))
			for _, v := range venues {
				row := resolveEarliestForVenue(ctx, session, v, party, startDate, withinDays)
				rows = append(rows, row)
			}
			// Available rows first, then alphabetical
			sort.SliceStable(rows, func(i, j int) bool {
				if rows[i].Available != rows[j].Available {
					return rows[i].Available
				}
				if rows[i].Available && rows[j].Available {
					return rows[i].SlotAt < rows[j].SlotAt
				}
				return rows[i].Venue < rows[j].Venue
			})
			return printJSONFiltered(cmd.OutOrStdout(), earliestResponse{
				Venues: venues, Party: party, Within: withinDays, Results: rows,
				QueriedAt: time.Now().UTC().Format(time.RFC3339),
			}, flags)
		},
	}
	cmd.Flags().IntVar(&party, "party", 2, "Party size")
	cmd.Flags().StringVar(&within, "within", "14d", "Search horizon (e.g., '14d', '7d', '30d' or a bare integer of days)")
	cmd.Flags().StringVar(&date, "date", "", "Start date YYYY-MM-DD (defaults to today)")
	cmd.Flags().BoolVar(&tonight, "tonight", false, "Shorthand for --date <today> --within 1d. Mutually exclusive with --date.")
	return cmd
}

// parseDays accepts "14d", "14", "7d" and returns days as int. "" returns 0.
func parseDays(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	s = strings.TrimSuffix(s, "d")
	s = strings.TrimSuffix(s, "D")
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseNetworkSlug(input string) (network, slug string) {
	if i := strings.Index(input, ":"); i > 0 {
		net := strings.ToLower(input[:i])
		if net == "opentable" || net == "tock" {
			return net, input[i+1:]
		}
	}
	return "", input
}

func resolveEarliestForVenue(ctx context.Context, s *auth.Session, venue string, party int, date string, within int) earliestRow {
	network, slug := parseNetworkSlug(venue)
	row := earliestRow{Venue: venue}

	tryOT := network == "" || network == "opentable"
	tryTock := network == "" || network == "tock"

	// Try Tock first because it has working availability via SSR
	// `calendar.offerings`. Many venues (Canlis, Alinea, Atomix) exist on
	// both networks; preferring Tock means the user gets a real
	// `Available=true|false` answer rather than the OT-side honest no-op.
	if tryTock {
		c, err := tock.New(s)
		if err == nil {
			detail, err := c.VenueAvailability(ctx, slug, date, party, "")
			if err == nil {
				row.Network = "tock"
				row.URL = tock.Origin + "/" + slug
				row.Available = false
				if cal, ok := detail["calendar"].(map[string]any); ok {
					if offerings, ok := cal["offerings"].(map[string]any); ok {
						if exp, ok := offerings["experience"].([]any); ok && len(exp) > 0 {
							row.Available = true
							row.Reason = fmt.Sprintf("found %d experience offerings", len(exp))
						}
					}
				}
				if !row.Available && row.Reason == "" {
					row.Reason = "no offerings returned by Tock SSR for the requested date"
				}
				return row
			}
			row.Reason = fmt.Sprintf("tock %s: %v", slug, err)
		}
	}
	if tryOT {
		c, err := opentable.New(s)
		if err == nil {
			row.Network = "opentable"
			// Resolve slug → restaurant ID via Autocomplete. The OT
			// `RestaurantsAvailability` GraphQL takes a numeric
			// restaurantId, not a slug. Slug-format queries
			// (`le-bernardin`) are converted to spaced names.
			restID, restName, _, rerr := c.RestaurantIDFromQuery(ctx, slug, 0, 0)
			if rerr != nil {
				row.Available = false
				row.Reason = fmt.Sprintf("opentable: could not resolve %q (%v)", slug, rerr)
				return row
			}
			row.URL = fmt.Sprintf("%s/restaurant/profile/%d", opentable.Origin, restID)
			// Call RestaurantsAvailability for `within` days
			// starting from `date`, with the requested party size
			// and a default 19:00 anchor time + 2.5h forward window.
			avail, aerr := c.RestaurantsAvailability(ctx, []int{restID}, date, "19:00", party, within, 150, 5)
			if aerr != nil {
				row.Available = false
				row.Reason = fmt.Sprintf("opentable %s (id=%d): %v", restName, restID, aerr)
				return row
			}
			// Find the earliest slot with isAvailable=true across all
			// returned days.
			var earliestSlotAt string
			for _, ra := range avail {
				if ra.RestaurantID != restID {
					continue
				}
				for _, d := range ra.AvailabilityDays {
					for _, s := range d.Slots {
						if !s.IsAvailable {
							continue
						}
						hh := 19 + s.TimeOffsetMinutes/60
						mm := s.TimeOffsetMinutes % 60
						if mm < 0 {
							hh -= 1
							mm += 60
						}
						slot := fmt.Sprintf("%sT%02d:%02d", d.Date, hh, mm)
						if earliestSlotAt == "" || slot < earliestSlotAt {
							earliestSlotAt = slot
						}
					}
				}
			}
			if earliestSlotAt != "" {
				row.Available = true
				row.SlotAt = earliestSlotAt
				row.Reason = fmt.Sprintf("opentable %s: earliest slot at %s", restName, earliestSlotAt)
			} else {
				row.Available = false
				row.Reason = fmt.Sprintf("opentable %s: no open slots in %d-day window for party=%d", restName, within, party)
			}
			return row
		}
	}
	if row.Network == "" {
		row.Network = "unknown"
		if row.Reason == "" {
			row.Reason = "could not resolve venue on OpenTable or Tock"
		}
	}
	return row
}
