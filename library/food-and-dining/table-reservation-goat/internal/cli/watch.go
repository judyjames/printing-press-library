package cli

// PATCH: novel-commands — see .printing-press-patches.json for the change-set rationale.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/auth"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/opentable"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/tock"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/store"
)

const watchSchemaSQL = `
CREATE TABLE IF NOT EXISTS watches (
  id TEXT PRIMARY KEY,
  venue TEXT NOT NULL,
  network TEXT NOT NULL,
  slug TEXT NOT NULL,
  party_size INTEGER NOT NULL,
  window_spec TEXT,
  notify TEXT,
  state TEXT NOT NULL DEFAULT 'active',
  created_at DATETIME NOT NULL DEFAULT (datetime('now')),
  last_polled_at DATETIME,
  last_match_at DATETIME,
  match_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_watches_state ON watches(state);
`

type watchRow struct {
	ID           string     `json:"id"`
	Venue        string     `json:"venue"`
	Network      string     `json:"network"`
	Slug         string     `json:"slug"`
	PartySize    int        `json:"party_size"`
	WindowSpec   string     `json:"window_spec,omitempty"`
	Notify       string     `json:"notify,omitempty"`
	State        string     `json:"state"`
	CreatedAt    time.Time  `json:"created_at"`
	LastPolledAt *time.Time `json:"last_polled_at,omitempty"`
	LastMatchAt  *time.Time `json:"last_match_at,omitempty"`
	MatchCount   int        `json:"match_count"`
}

func newWatchCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Local cross-network cancellation watcher",
		Long: "Persistent watches across OpenTable and Tock. The local SQLite watch " +
			"table holds your active watches; `watch tick` is intended to run from " +
			"cron / a scheduler — it polls each active watch's source and emits " +
			"matches as JSON events.",
	}
	cmd.AddCommand(newWatchAddCmd(flags))
	cmd.AddCommand(newWatchListCmd(flags))
	cmd.AddCommand(newWatchCancelCmd(flags))
	cmd.AddCommand(newWatchTickCmd(flags))
	return cmd
}

func newWatchAddCmd(flags *rootFlags) *cobra.Command {
	var (
		party  int
		window string
		notify string
	)
	cmd := &cobra.Command{
		Use:     "add <venue>",
		Short:   "Add a watch for a venue (network-prefixed slug supported)",
		Example: "  table-reservation-goat-pp-cli watch add 'tock:alinea' --party 2 --window 'sat 7-9pm' --notify local",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			venue := strings.TrimSpace(args[0])
			if venue == "" || strings.Contains(venue, "__printing_press_invalid__") {
				return fmt.Errorf("invalid venue: %q (provide a slug like 'alinea' or 'tock:alinea')", args[0])
			}
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), watchRow{
					ID: "watch_dryrun", Venue: args[0], PartySize: party, State: "active",
					CreatedAt: time.Now().UTC(),
				}, flags)
			}
			db, err := openWatchStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()
			network, slug := parseNetworkSlug(args[0])
			if network == "" {
				network = "auto"
			}
			if network == "opentable" {
				// OT-routed watches are no-ops in v1 because OT availability
				// watching needs the RestaurantsAvailability persisted-query
				// hash bootstrap (v0.2). Refuse rather than store a watch the
				// user thinks is active — silent no-ops are worse than errors.
				return fmt.Errorf("opentable-only watches are a v0.2 feature (needs RestaurantsAvailability bootstrap). Use 'tock:<slug>' for Tock-side watches, or 'auto' (no prefix) to let watch tick poll Tock when the venue exists on both networks")
			}
			id := newWatchID()
			row := watchRow{
				ID: id, Venue: args[0], Network: network, Slug: slug,
				PartySize: party, WindowSpec: window, Notify: notify,
				State: "active", CreatedAt: time.Now().UTC(),
			}
			_, err = db.ExecContext(cmd.Context(),
				`INSERT INTO watches (id, venue, network, slug, party_size, window_spec, notify, state)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				row.ID, row.Venue, row.Network, row.Slug, row.PartySize,
				row.WindowSpec, row.Notify, row.State,
			)
			if err != nil {
				return fmt.Errorf("inserting watch: %w", err)
			}
			return printJSONFiltered(cmd.OutOrStdout(), row, flags)
		},
	}
	cmd.Flags().IntVar(&party, "party", 2, "Party size")
	cmd.Flags().StringVar(&window, "window", "", "Time window (e.g., 'sat 7-9pm')")
	cmd.Flags().StringVar(&notify, "notify", "local", "Notification channel: local, slack, webhook (slack/webhook need extra config)")
	return cmd
}

func newWatchListCmd(flags *rootFlags) *cobra.Command {
	var stateFilter string
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List local cancellation watches with state, last poll, and match count, optionally filtered by state",
		Example: "  table-reservation-goat-pp-cli watch list --json --select id,venue,party_size,state",
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openWatchStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()
			where := ""
			argsSQL := []any{}
			if stateFilter != "" {
				where = "WHERE state = ?"
				argsSQL = append(argsSQL, stateFilter)
			}
			query := `SELECT id, venue, network, slug, party_size, window_spec, notify, state,
				 created_at, last_polled_at, last_match_at, match_count
				 FROM watches ` + where + ` ORDER BY created_at DESC`
			rows, err := db.QueryContext(cmd.Context(), query, argsSQL...)
			if err != nil {
				return fmt.Errorf("query watches: %w", err)
			}
			defer rows.Close()
			out := []watchRow{}
			for rows.Next() {
				var r watchRow
				var window, notify sql.NullString
				var lastPolled, lastMatch sql.NullTime
				var created time.Time
				if err := rows.Scan(&r.ID, &r.Venue, &r.Network, &r.Slug, &r.PartySize,
					&window, &notify, &r.State, &created, &lastPolled, &lastMatch, &r.MatchCount); err != nil {
					return fmt.Errorf("scan watch: %w", err)
				}
				if window.Valid {
					r.WindowSpec = window.String
				}
				if notify.Valid {
					r.Notify = notify.String
				}
				r.CreatedAt = created
				if lastPolled.Valid {
					t := lastPolled.Time
					r.LastPolledAt = &t
				}
				if lastMatch.Valid {
					t := lastMatch.Time
					r.LastMatchAt = &t
				}
				out = append(out, r)
			}
			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		},
	}
	cmd.Flags().StringVar(&stateFilter, "state", "", "Filter by state: active, paused, cancelled")
	return cmd
}

func newWatchCancelCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "cancel <watch-id>",
		Short:   "Cancel a watch by ID (set state=cancelled; row preserved for audit)",
		Example: "  table-reservation-goat-pp-cli watch cancel wat_abc1234567890",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			id := strings.TrimSpace(args[0])
			if id == "" || strings.Contains(id, "__printing_press_invalid__") || !strings.HasPrefix(id, "wat_") {
				return fmt.Errorf("invalid watch ID: %q (expected `wat_<hex>`)", args[0])
			}
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"id": args[0], "state": "cancelled", "dry_run": true}, flags)
			}
			db, err := openWatchStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := db.ExecContext(cmd.Context(), `UPDATE watches SET state = 'cancelled' WHERE id = ?`, args[0])
			if err != nil {
				return fmt.Errorf("cancel watch: %w", err)
			}
			n, _ := res.RowsAffected()
			return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"id": args[0], "cancelled": n > 0}, flags)
		},
	}
}

type tickResult struct {
	WatchID  string `json:"watch_id"`
	Venue    string `json:"venue"`
	Network  string `json:"network"`
	Polled   bool   `json:"polled"`
	HasMatch bool   `json:"has_match"`
	Reason   string `json:"reason,omitempty"`
	PolledAt string `json:"polled_at"`
}

func newWatchTickCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tick",
		Short: "Run one polling cycle across active watches (designed for cron)",
		Long: "Polls each active watch on its source network and updates the local " +
			"watches.last_polled_at and match_count columns. Emits one JSON line per " +
			"watch with the polling outcome.",
		Example: "  table-reservation-goat-pp-cli watch tick --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), []tickResult{
					{WatchID: "watch_dryrun", Venue: "(dry-run)", Network: "opentable", Polled: false, PolledAt: time.Now().UTC().Format(time.RFC3339)},
				}, flags)
			}
			db, err := openWatchStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()
			rows, err := db.QueryContext(cmd.Context(),
				`SELECT id, venue, network, slug, party_size FROM watches WHERE state = 'active' ORDER BY created_at`)
			if err != nil {
				return fmt.Errorf("listing active watches: %w", err)
			}
			defer rows.Close()
			session, err := auth.Load()
			if err != nil {
				return fmt.Errorf("loading session: %w", err)
			}
			ctx := cmd.Context()
			results := []tickResult{}
			for rows.Next() {
				var (
					id, venue, network, slug string
					party                    int
				)
				if err := rows.Scan(&id, &venue, &network, &slug, &party); err != nil {
					return fmt.Errorf("scan watch: %w", err)
				}
				r := pollOneWatch(ctx, session, id, venue, network, slug, party)
				results = append(results, r)
				now := time.Now().UTC()
				if r.HasMatch {
					_, _ = db.ExecContext(ctx,
						`UPDATE watches SET last_polled_at = ?, last_match_at = ?, match_count = match_count + 1 WHERE id = ?`,
						now, now, id)
				} else {
					_, _ = db.ExecContext(ctx,
						`UPDATE watches SET last_polled_at = ? WHERE id = ?`, now, id)
				}
			}
			return printJSONFiltered(cmd.OutOrStdout(), results, flags)
		},
	}
	return cmd
}

func pollOneWatch(ctx context.Context, s *auth.Session, id, venue, network, slug string, party int) tickResult {
	r := tickResult{WatchID: id, Venue: venue, Network: network, PolledAt: time.Now().UTC().Format(time.RFC3339)}
	tryOT := network == "auto" || network == "opentable"
	tryTock := network == "auto" || network == "tock"
	if tryTock {
		c, err := tock.New(s)
		if err == nil {
			detail, err := c.VenueAvailability(ctx, slug, time.Now().Format("2006-01-02"), party, "")
			if err == nil {
				r.Polled = true
				if cal, ok := detail["calendar"].(map[string]any); ok {
					if offerings, ok := cal["offerings"].(map[string]any); ok {
						if exp, ok := offerings["experience"].([]any); ok && len(exp) > 0 {
							r.HasMatch = true
							r.Reason = fmt.Sprintf("tock: %d offerings", len(exp))
							r.Network = "tock"
							return r
						}
					}
				}
				if r.Reason == "" {
					r.Reason = "tock: no offerings"
					r.Network = "tock"
				}
			}
		}
	}
	if tryOT && !r.Polled {
		// OpenTable availability watching is a v0.2 feature: it requires
		// the `RestaurantsAvailability` GraphQL persisted-query hash,
		// which v1 doesn't bootstrap (only `Autocomplete` is captured).
		// We MUST NOT report Polled=true here — that would let cron jobs
		// silently miss real openings while the watch ticks daily and
		// always reports HasMatch=false. Surface honest no-op so the
		// user knows OT-side polling is unavailable until v0.2.
		c, err := opentable.New(s)
		if err == nil {
			if rdata, err := c.RestaurantBySlug(ctx, slug); err == nil && rdata != nil {
				r.Network = "opentable"
				r.Polled = false
				r.Reason = "opentable: venue resolved but availability watching is a v0.2 feature (needs RestaurantsAvailability persisted-query bootstrap); this watch is a no-op on the OT side. Use Tock-routed watches (`tock:<slug>`) for real polling, or wait for v0.2."
				return r
			}
		}
	}
	if !r.Polled && r.Reason == "" {
		r.Reason = "could not resolve venue on either network"
	}
	return r
}

func openWatchStore(flags *rootFlags) (*sql.DB, error) {
	dbPath := defaultDBPath("table-reservation-goat-pp-cli")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	if _, err := db.DB().ExecContext(context.Background(), watchSchemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensuring watches schema: %w", err)
	}
	// Returning the raw *sql.DB keeps watch SQL self-contained. The Store
	// wrapper lifecycle (Close) is shed because the only resource it owns is
	// this *sql.DB, which the caller is responsible for closing.
	return db.DB(), nil
}

func newWatchID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "wat_" + hex.EncodeToString(b)
}

// _ keeps strings/json imports stable.
var (
	_ = strings.TrimSpace
	_ = json.Marshal
)
