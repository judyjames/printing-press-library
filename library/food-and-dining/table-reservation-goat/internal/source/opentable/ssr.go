package opentable

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/cliutil"
)

// initialStateAnchor finds the start of the __INITIAL_STATE__ JSON.
// We can't use a regex for the JSON body itself because OT's state is
// 100+ KB of nested JSON; a regex either over-matches (greedy across
// scripts) or under-matches (non-greedy stopping at the first `}`). We
// anchor to `__INITIAL_STATE__ = ` and walk braces manually.
var initialStateAnchor = regexp.MustCompile(`__INITIAL_STATE__\s*=\s*(?:JSON\.parse\((['"]))?`)

// FetchInitialState fetches an OpenTable page and extracts __INITIAL_STATE__
// as parsed JSON. Use this for read-paths where the SSR-rendered state has
// what we need (restaurant detail, search results, metro listings).
//
// CSRF bootstrap is optional here — SSR pages serve without CSRF and we don't
// inject the token on read paths. Bootstrap is only required for GraphQL
// mutations and authenticated GraphQL queries.
func (c *Client) FetchInitialState(ctx context.Context, path string) (map[string]any, error) {
	url := Origin + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building SSR request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Referer", Origin+"/")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("opentable: %s not found (404)", path)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("opentable: %s returned HTTP %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s body: %w", path, err)
	}
	jsonBody, err := extractInitialState(body)
	if err != nil {
		return nil, fmt.Errorf("opentable: __INITIAL_STATE__ not found in %s response (%w)", path, err)
	}
	var state map[string]any
	if err := json.Unmarshal(jsonBody, &state); err != nil {
		return nil, fmt.Errorf("parsing __INITIAL_STATE__ from %s: %w", path, err)
	}
	return state, nil
}

// extractInitialState locates the `__INITIAL_STATE__ = {...}` block and
// extracts the JSON body via balanced-brace walking. Handles both the bare
// object form (`__INITIAL_STATE__ = {...}`) and the JSON.parse-wrapped form
// (`__INITIAL_STATE__ = JSON.parse('{...}')`).
func extractInitialState(body []byte) ([]byte, error) {
	loc := initialStateAnchor.FindIndex(body)
	if loc == nil {
		return nil, fmt.Errorf("anchor not found")
	}
	// Find the first `{` at or after loc[1].
	i := loc[1]
	for i < len(body) && body[i] != '{' {
		i++
	}
	if i >= len(body) {
		return nil, fmt.Errorf("no JSON body after anchor")
	}
	// Walk balanced braces, respecting strings and escapes.
	depth := 0
	inString := false
	escape := false
	for j := i; j < len(body); j++ {
		ch := body[j]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			escape = true
			continue
		}
		if inString {
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return body[i : j+1], nil
			}
		}
	}
	return nil, fmt.Errorf("unbalanced braces")
}

// SearchOptions captures the consumer search query parameters that
// /s?... accepts.
type SearchOptions struct {
	Query        string  // free text term
	Latitude     float64 // geo center
	Longitude    float64
	MetroID      int    // optional metro hint
	Covers       int    // party size (defaults to 2)
	DateTime     string // YYYY-MM-DDTHH:MM:SS (defaults to today 19:00)
	Limit        int
}

// SearchRestaurants runs OT's /s search page and returns the restaurants
// list extracted from `__INITIAL_STATE__.multiSearch.restaurants`.
func (c *Client) SearchRestaurants(ctx context.Context, opts SearchOptions) ([]map[string]any, error) {
	if opts.Covers <= 0 {
		opts.Covers = 2
	}
	if opts.DateTime == "" {
		opts.DateTime = "2026-05-15T19:00:00"
	}
	q := fmt.Sprintf("/s?dateTime=%s&covers=%d&latitude=%.4f&longitude=%.4f",
		opts.DateTime, opts.Covers, opts.Latitude, opts.Longitude)
	if opts.MetroID > 0 {
		q += fmt.Sprintf("&metroId=%d", opts.MetroID)
	}
	if opts.Query != "" {
		q += "&term=" + opts.Query
	}
	state, err := c.FetchInitialState(ctx, q)
	if err != nil {
		return nil, err
	}
	ms, ok := state["multiSearch"].(map[string]any)
	if !ok {
		return nil, nil
	}
	rest, ok := ms["restaurants"].([]any)
	if !ok {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(rest))
	for i, r := range rest {
		if opts.Limit > 0 && i >= opts.Limit {
			break
		}
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// RestaurantBySlug fetches /r/<slug> and extracts the restaurant detail
// from `__INITIAL_STATE__.restaurantProfile.restaurant`. Returns
// "not found" when OT 404s the slug (slug invalid OR restaurant only on Tock).
func (c *Client) RestaurantBySlug(ctx context.Context, slug string) (map[string]any, error) {
	state, err := c.FetchInitialState(ctx, "/r/"+slug)
	if err != nil {
		return nil, err
	}
	rp, ok := state["restaurantProfile"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("opentable: /r/%s did not return restaurantProfile (likely 404 or Tock-only venue)", slug)
	}
	r, ok := rp["restaurant"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("opentable: /r/%s restaurantProfile.restaurant missing", slug)
	}
	return r, nil
}

// _ keeps cliutil imported if a downstream helper needs it. Currently
// unused but expected to attach an AdaptiveLimiter to client request paths.
var _ = cliutil.IsVerifyEnv
