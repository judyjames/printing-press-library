package cli

// PATCH: novel-commands — see .printing-press-patches.json for the change-set rationale.

// pp:client-call — `goat` reaches the OpenTable SSR client and the Tock client
// through `internal/source/opentable` and `internal/source/tock`. Dogfood's
// reimplementation_check sibling-import regex matches a single path segment
// after `internal/`, so multi-segment paths under `internal/source/...` aren't
// recognized as a client signal. Documented carve-out per AGENTS.md.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/auth"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/opentable"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/tock"
)

// goatResult is one merged row from a cross-network search.
type goatResult struct {
	Network      string  `json:"network"`
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug,omitempty"`
	Cuisine      string  `json:"cuisine,omitempty"`
	Neighborhood string  `json:"neighborhood,omitempty"`
	Metro        string  `json:"metro,omitempty"`
	Latitude     float64 `json:"latitude,omitempty"`
	Longitude    float64 `json:"longitude,omitempty"`
	URL          string  `json:"url,omitempty"`
	MatchScore   float64 `json:"match_score"`
}

type goatResponse struct {
	Query     string       `json:"query"`
	Results   []goatResult `json:"results"`
	Errors    []string     `json:"errors,omitempty"`
	Sources   []string     `json:"sources_queried"`
	QueriedAt string       `json:"queried_at"`
}

// newGoatCmd is the headline transcendence command: a single query that hits
// OpenTable's Autocomplete and Tock's venue search simultaneously, merges
// results into one ranked list, and returns agent-shaped JSON. This is the
// single command an agent should reach for when asked to find a table.
func newGoatCmd(flags *rootFlags) *cobra.Command {
	var (
		latitude  float64
		longitude float64
		network   string
		limit     int
		party     int
		when      string
	)
	cmd := &cobra.Command{
		Use:     "goat <query>",
		Short:   "Cross-network unified restaurant search (OpenTable + Tock)",
		Long:    "Search OpenTable and Tock simultaneously and return one ranked list. Use this any time an agent or user needs a restaurant search that crosses both reservation networks.",
		Example: "  table-reservation-goat-pp-cli goat 'omakase manhattan' --party 2 --agent",
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		Args: cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			query := strings.Join(args, " ")
			if dryRunOK(flags) {
				return printJSONFiltered(cmd.OutOrStdout(), goatResponse{
					Query: query,
					Results: []goatResult{
						{Network: "opentable", Name: "(dry-run sample)", MatchScore: 1.0},
					},
					Sources:   []string{"opentable", "tock"},
					QueriedAt: time.Now().UTC().Format(time.RFC3339),
				}, flags)
			}
			ctx := cmd.Context()
			session, err := auth.Load()
			if err != nil {
				return fmt.Errorf("loading session: %w", err)
			}
			net := strings.ToLower(network)
			results := []goatResult{}
			errors := []string{}
			sources := []string{}

			if net == "" || net == "opentable" {
				sources = append(sources, "opentable")
				otRes, otErr := goatQueryOpenTable(ctx, session, query, latitude, longitude)
				if otErr != nil {
					errors = append(errors, fmt.Sprintf("opentable: %v", otErr))
				} else {
					results = append(results, otRes...)
				}
			}
			if net == "" || net == "tock" {
				sources = append(sources, "tock")
				tockRes, tockErr := goatQueryTock(ctx, session, query)
				if tockErr != nil {
					errors = append(errors, fmt.Sprintf("tock: %v", tockErr))
				} else {
					results = append(results, tockRes...)
				}
			}
			// Rank: match score descending. Ties broken by name for determinism.
			sort.SliceStable(results, func(i, j int) bool {
				if results[i].MatchScore != results[j].MatchScore {
					return results[i].MatchScore > results[j].MatchScore
				}
				return results[i].Name < results[j].Name
			})
			if limit > 0 && len(results) > limit {
				results = results[:limit]
			}
			out := goatResponse{
				Query:     query,
				Results:   results,
				Errors:    errors,
				Sources:   sources,
				QueriedAt: time.Now().UTC().Format(time.RFC3339),
			}
			return printJSONFiltered(cmd.OutOrStdout(), out, flags)
		},
	}
	cmd.Flags().Float64Var(&latitude, "latitude", 0, "Geo-narrowed search latitude (defaults to 40.7589 / NYC)")
	cmd.Flags().Float64Var(&longitude, "longitude", 0, "Geo-narrowed search longitude (defaults to -73.9851 / NYC)")
	cmd.Flags().StringVar(&network, "network", "", "Restrict to one network (opentable, tock); default queries both")
	cmd.Flags().IntVar(&limit, "limit", 20, "Max merged results to return")
	cmd.Flags().IntVar(&party, "party", 2, "Party size (informational; OT autocomplete does not filter on this)")
	cmd.Flags().StringVar(&when, "when", "", "Time hint for search (e.g., 'fri 7-9pm', 'tonight', 'this weekend'); informational in v1")
	_ = party
	_ = when
	return cmd
}

func goatQueryOpenTable(ctx context.Context, s *auth.Session, query string, lat, lng float64) ([]goatResult, error) {
	c, err := opentable.New(s)
	if err != nil {
		return nil, err
	}
	if lat == 0 && lng == 0 {
		// Default to NYC midtown if no geo provided.
		lat, lng = 40.7589, -73.9851
	}
	// Use the GraphQL Autocomplete endpoint. OpenTable's /s search and
	// /r/<slug> pages both return a 2.5KB SPA shell to non-Chrome clients —
	// only the home page (/) serves real SSR data, and that data is the home
	// view, not search results. The Autocomplete persisted-query is the only
	// reliable path; it bootstraps CSRF from the home page (one cached fetch
	// per process lifetime) and then queries by term + lat/lng.
	results, err := c.Autocomplete(ctx, query, lat, lng)
	if err != nil {
		return nil, err
	}
	out := make([]goatResult, 0, len(results))
	q := strings.ToLower(query)
	for _, r := range results {
		// Score by match quality. Substring of full query → 0.95;
		// matching just the first token → 0.65; otherwise prefix
		// confidence from the autocomplete API → 0.4.
		score := 0.4
		nameLower := strings.ToLower(r.Name)
		if strings.Contains(nameLower, q) {
			score = 0.95
		} else if firstTok := firstToken(q); firstTok != "" && strings.Contains(nameLower, firstTok) {
			score = 0.65
		}
		// OT autocomplete doesn't return urlSlug; use the restaurant
		// profile path keyed by id, which is the stable canonical link.
		url := ""
		if r.ID != "" {
			url = opentable.Origin + "/restaurant/profile/" + r.ID
		}
		out = append(out, goatResult{
			Network:      "opentable",
			ID:           r.ID,
			Name:         r.Name,
			Metro:        r.MetroName,
			Neighborhood: r.NeighborhoodName,
			Latitude:     r.Latitude,
			Longitude:    r.Longitude,
			URL:          url,
			MatchScore:   score,
		})
	}
	return out, nil
}

func firstToken(s string) string {
	for i, r := range s {
		if r == ' ' || r == '\t' {
			return s[:i]
		}
	}
	return s
}

func goatQueryTock(ctx context.Context, s *auth.Session, query string) ([]goatResult, error) {
	// Tock has no public search endpoint. The viable read path is to
	// resolve the query as a venue slug directly (`canlis`, `alinea`,
	// `le-bernardin`) against `/<slug>`. If the SSR Redux state has a
	// `business` slice, the venue exists on Tock. v0.2 will also scrape
	// metro pages (e.g., /seattle) for broader discovery.
	slug := slugify(query)
	if slug == "" {
		return nil, nil
	}
	c, err := tock.New(s)
	if err != nil {
		return nil, err
	}
	detail, err := c.VenueDetail(ctx, slug)
	if err != nil {
		// 404 / no Tock venue under that slug. Don't fail the whole goat
		// call — just contribute zero rows from this source.
		return []goatResult{}, nil
	}
	biz, ok := detail["business"].(map[string]any)
	if !ok || len(biz) == 0 {
		return []goatResult{}, nil
	}
	row := goatResult{
		Network:    "tock",
		MatchScore: 0.95,
		URL:        tock.Origin + "/" + slug,
		Slug:       slug,
	}
	if name, ok := biz["name"].(string); ok {
		row.Name = name
	}
	if id, ok := biz["id"].(float64); ok {
		row.ID = fmt.Sprintf("%d", int(id))
	}
	if city, ok := biz["city"].(string); ok {
		row.Metro = city
	}
	if cuisine, ok := biz["cuisine"].(string); ok {
		row.Cuisine = cuisine
	}
	return []goatResult{row}, nil
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	out := strings.Builder{}
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_':
			if !prevDash && out.Len() > 0 {
				out.WriteRune('-')
				prevDash = true
			}
		}
	}
	res := out.String()
	return strings.TrimSuffix(res, "-")
}

// _ keeps cliutil imported for future limiter wiring.
var _ = cliutil.IsVerifyEnv
