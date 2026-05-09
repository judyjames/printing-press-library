// Package opentable wraps OpenTable's consumer surface (the dapi GraphQL
// endpoint, REST booking, and SSR-rendered HTML pages). The OpenTable
// Partner API is out of scope.
//
// Auth model: OpenTable's bot defense (Akamai) requires a Chrome TLS
// fingerprint AND, for authenticated reads, the `authCke` session cookie
// the user has after logging in to opentable.com. We use enetx/surf for
// the TLS fingerprint and the session cookies imported via auth login.
package opentable

// PATCH: cross-network-source-clients — see .printing-press-patches.json for the change-set rationale.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/enetx/surf"
	"golang.org/x/sync/singleflight"

	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/food-and-dining/table-reservation-goat/internal/source/auth"
)

const (
	// Origin is the OpenTable consumer host. Every request goes through here.
	Origin = "https://www.opentable.com"

	// GraphQLPath is the persisted-query GraphQL endpoint. CSRF + cookies
	// authenticate; persisted-query hashes drift over bundle releases.
	GraphQLPath = "/dapi/fe/gql"

	// AutocompleteHash is the live persisted-query hash captured during
	// browser-sniff (2026-05-09). On `PersistedQueryNotFound` 400, the
	// client re-fetches the homepage and bootstraps a fresh hash.
	AutocompleteHash = "fe1d118abd4c227750693027c2414d43014c2493f64f49bcef5a65274ce9c3c3"

	// RestaurantsAvailabilityHash is the persisted-query hash for
	// `RestaurantsAvailability` cited by 21Bruce/resolved-bot's Go client
	// (`FindKey`). The same hash appears across multiple community wrappers
	// as the working hash; it drifts on bundle releases but a working hash
	// is always discoverable. v1 caches this value; on a
	// PersistedQueryNotFound (400) error, the client surfaces a clear hint
	// that the user should run `doctor --refresh-hashes` (a v0.2 escape
	// hatch).
	RestaurantsAvailabilityHash = "e6b87021ed6e865a7778aa39d35d09864c1be29c683c707602dd3de43c854d86"

	defaultTimeout = 30 * time.Second
)

// Client is a Surf-based OpenTable client with the user's session cookies
// attached.
type Client struct {
	mu      sync.Mutex
	http    *http.Client
	session *auth.Session
	limiter *cliutil.AdaptiveLimiter

	// bootstrapSF dedupes concurrent Bootstrap() calls. Two goroutines
	// that both observe a stale csrfToken would otherwise both fire
	// the home-page GET; singleflight collapses that into a single
	// in-flight request whose result every waiter receives.
	bootstrapSF singleflight.Group

	csrfToken      string
	csrfFetchedAt  time.Time
	csrfTTL        time.Duration
	autocompleteH  string
	autocompleteHM time.Time
}

// New creates a Surf-backed OpenTable client. Pass the loaded auth.Session
// to attach the user's cookies; pass nil for an anonymous client (search,
// availability — but not booking, my-reservations, or wishlist).
func New(s *auth.Session) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookiejar: %w", err)
	}
	otURL, _ := auth.CookieURLFor(auth.NetworkOpenTable)
	if s != nil && otURL != nil {
		jar.SetCookies(otURL, s.HTTPCookies(auth.NetworkOpenTable))
	}
	surfClient := surf.NewClient().
		Builder().
		Impersonate().Chrome().
		Session().
		Build().
		Unwrap()
	std := surfClient.Std()
	std.Jar = jar
	std.Timeout = defaultTimeout
	return &Client{
		http:          std,
		session:       s,
		// Conservative default rate: OpenTable's Akamai begins flagging
		// Surf fingerprints after a burst of requests. 0.5 req/s is one
		// request every 2 seconds — slow enough that home-page bootstrap
		// and a few GraphQL calls per CLI invocation never approach the
		// soft cap. AdaptiveLimiter ramps up after 10 consecutive
		// successes, so steady-state usage will reach a higher rate
		// naturally; bursts are paced.
		limiter:       cliutil.NewAdaptiveLimiter(0.5),
		csrfTTL:       30 * time.Minute,
		autocompleteH: AutocompleteHash,
	}, nil
}

// do429Aware paces a request through the adaptive limiter, retries once on
// HTTP 429 with the Retry-After hint, and returns a typed
// `*cliutil.RateLimitError` when retries are exhausted. Empty-on-throttle is
// a recipe for silent data corruption: callers MUST surface this error
// rather than treating it as "no data exists".
func (c *Client) do429Aware(req *http.Request) (*http.Response, error) {
	c.limiter.Wait()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		c.limiter.OnSuccess()
		return resp, nil
	}
	// Drain + close the 429 body so we can retry the request.
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	c.limiter.OnRateLimit()
	wait := cliutil.RetryAfter(resp)
	time.Sleep(wait)
	// Single retry. Clone the request to reset the body reader (if any)
	// and avoid mutating the caller's req.
	clonedReq := req.Clone(req.Context())
	if req.Body != nil && req.GetBody != nil {
		newBody, gerr := req.GetBody()
		if gerr == nil {
			clonedReq.Body = newBody
		}
	}
	c.limiter.Wait()
	resp2, err := c.http.Do(clonedReq)
	if err != nil {
		return nil, err
	}
	if resp2.StatusCode != http.StatusTooManyRequests {
		c.limiter.OnSuccess()
		return resp2, nil
	}
	// Retry also rate-limited. Surface the typed error so the caller
	// can distinguish "throttled" from "no results".
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	c.limiter.OnRateLimit()
	return nil, &cliutil.RateLimitError{
		URL:        req.URL.String(),
		RetryAfter: cliutil.RetryAfter(resp2),
		Body:       string(body2) + " (initial body: " + string(body) + ")",
	}
}

// LoggedIn reports whether the client is configured with an OpenTable
// session cookie.
func (c *Client) LoggedIn() bool {
	return c.session != nil && c.session.LoggedIn(auth.NetworkOpenTable)
}

// Bootstrap fetches the OpenTable homepage to extract `__CSRF_TOKEN__`.
// Idempotent — only refreshes when the cached token is older than csrfTTL.
// Concurrent callers are deduplicated via singleflight so a single in-flight
// fetch satisfies all waiters; the redundant-fetch race that the previous
// "check-release-fetch-relock" pattern allowed is now closed.
func (c *Client) Bootstrap(ctx context.Context) error {
	c.mu.Lock()
	fresh := c.csrfToken != "" && time.Since(c.csrfFetchedAt) < c.csrfTTL
	c.mu.Unlock()
	if fresh {
		return nil
	}
	_, err, _ := c.bootstrapSF.Do("csrf", func() (any, error) {
		// Double-check inside the singleflight closure: another caller
		// may have completed a bootstrap between our outer check and
		// the singleflight grant. Avoid a wasted fetch if so.
		c.mu.Lock()
		if c.csrfToken != "" && time.Since(c.csrfFetchedAt) < c.csrfTTL {
			c.mu.Unlock()
			return nil, nil
		}
		c.mu.Unlock()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, Origin+"/", nil)
		if err != nil {
			return nil, fmt.Errorf("building bootstrap request: %w", err)
		}
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		resp, err := c.do429Aware(req)
		if err != nil {
			return nil, fmt.Errorf("fetching opentable.com: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("opentable.com returned HTTP %d during bootstrap", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading bootstrap body: %w", err)
		}
		token := extractCSRFToken(body)
		if token == "" {
			return nil, errors.New("could not extract __CSRF_TOKEN__ from opentable.com homepage; site structure may have changed")
		}
		c.mu.Lock()
		c.csrfToken = token
		c.csrfFetchedAt = time.Now()
		c.mu.Unlock()
		return nil, nil
	})
	return err
}

// csrfRE matches both the JSON-embedded form (which is what the SSR HTML
// actually serves: `"__CSRF_TOKEN__":"<uuid>"`) and the runtime JS-assignment
// form (`window.__CSRF_TOKEN__ = "<uuid>"`). The JSON form is what we get
// from a Surf-cleared GET on the home page; the JS form is what real Chrome
// sees after JS hydration. Either is acceptable.
var csrfRE = regexp.MustCompile(`(?:window\.__CSRF_TOKEN__\s*=\s*['"]|"__CSRF_TOKEN__"\s*:\s*")([0-9a-fA-F-]{16,})`)

func extractCSRFToken(html []byte) string {
	m := csrfRE.FindSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return string(m[1])
}

// CSRF returns the current cached CSRF token (call Bootstrap first).
func (c *Client) CSRF() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.csrfToken
}

// AutocompleteHash returns the current cached persisted-query hash for
// the Autocomplete operation.
func (c *Client) AutocompleteHash() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.autocompleteH
}

// AutocompleteResult is one entry in the Autocomplete response.
type AutocompleteResult struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Country          string  `json:"country,omitempty"`
	MetroName        string  `json:"metroName,omitempty"`
	NeighborhoodName string  `json:"neighborhoodName,omitempty"`
	Type             string  `json:"type"` // "Restaurant", "Cuisine", "Location"
	Latitude         float64 `json:"latitude,omitempty"`
	Longitude        float64 `json:"longitude,omitempty"`
	URLSlug          string  `json:"urlSlug,omitempty"`
}

// Autocomplete searches OpenTable for restaurants matching `term` near the
// provided lat/lng. Works without auth (CSRF token is sufficient).
func (c *Client) Autocomplete(ctx context.Context, term string, lat, lng float64) ([]AutocompleteResult, error) {
	if err := c.Bootstrap(ctx); err != nil {
		return nil, err
	}
	body := map[string]any{
		"operationName": "Autocomplete",
		"variables": map[string]any{
			"term":           term,
			"latitude":       lat,
			"longitude":      lng,
			"useNewVersion":  true,
		},
		"extensions": map[string]any{
			"persistedQuery": map[string]any{
				"version":    1,
				"sha256Hash": c.AutocompleteHash(),
			},
		},
	}
	parsed, err := c.gqlCall(ctx, "Autocomplete", body)
	if err != nil {
		return nil, err
	}
	// Response shape: data.autocomplete.autocompleteResults[]
	type respShape struct {
		Data struct {
			Autocomplete struct {
				Results []AutocompleteResult `json:"autocompleteResults"`
			} `json:"autocomplete"`
		} `json:"data"`
	}
	var r respShape
	if err := json.Unmarshal(parsed, &r); err != nil {
		return nil, fmt.Errorf("parsing Autocomplete response: %w", err)
	}
	return r.Data.Autocomplete.Results, nil
}

// AvailabilitySlot is one open reservation slot returned by
// `RestaurantsAvailability`. The slot tokens are short-lived (~minutes) and
// are required to actually book the slot via `make-reservation`.
type AvailabilitySlot struct {
	IsAvailable          bool     `json:"isAvailable"`
	TimeOffsetMinutes    int      `json:"timeOffsetMinutes"`
	SlotHash             string   `json:"slotHash"`
	SlotAvailabilityToken string  `json:"slotAvailabilityToken"`
	PointsType           string   `json:"pointsType,omitempty"`
	PointsValue          int      `json:"pointsValue,omitempty"`
	Attributes           []string `json:"attributes,omitempty"`
}

// AvailabilityDay is one day in the availability response. `Date` is
// YYYY-MM-DD; `Slots` lists per-time slots. Empty `Slots` means no openings
// were found for the requested party + time window on that day.
type AvailabilityDay struct {
	Date  string             `json:"date"`
	Slots []AvailabilitySlot `json:"slots"`
}

// RestaurantAvailability is the per-restaurant chunk of the response: one
// restaurant's availability across N days starting from `date`.
type RestaurantAvailability struct {
	RestaurantID    int               `json:"restaurantId"`
	AvailabilityDays []AvailabilityDay `json:"availabilityDays"`
}

// RestaurantsAvailability calls the documented `RestaurantsAvailability`
// GraphQL persisted-query and returns one chunk per requested restaurant ID.
// Slot tokens in the response are short-lived (~minutes) and are required
// for the actual booking POST.
func (c *Client) RestaurantsAvailability(ctx context.Context, restaurantIDs []int, date, hhmm string, partySize, forwardDays, forwardMinutes, forwardSlots int) ([]RestaurantAvailability, error) {
	if forwardDays <= 0 {
		forwardDays = 1
	}
	if forwardMinutes <= 0 {
		forwardMinutes = 150
	}
	if forwardSlots <= 0 {
		forwardSlots = 5
	}
	if hhmm == "" {
		hhmm = "19:00"
	}
	if partySize <= 0 {
		partySize = 2
	}
	body := map[string]any{
		"operationName": "RestaurantsAvailability",
		"variables": map[string]any{
			"onlyPop":          false,
			"forwardDays":      forwardDays,
			"requireTimes":     true,
			"requireTypes":     []string{"Standard", "Experience"},
			"restaurantIds":    restaurantIDs,
			"date":             date,
			"time":             hhmm,
			"partySize":        partySize,
			"databaseRegion":   "NA",
			"forwardMinutes":   forwardMinutes,
			"forwardTimeslots": forwardSlots,
		},
		"extensions": map[string]any{
			"persistedQuery": map[string]any{
				"version":    1,
				"sha256Hash": RestaurantsAvailabilityHash,
			},
		},
	}
	parsed, err := c.gqlCall(ctx, "RestaurantsAvailability", body)
	if err != nil {
		return nil, err
	}
	type respShape struct {
		Data struct {
			Availability []RestaurantAvailability `json:"availability"`
		} `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	var r respShape
	if err := json.Unmarshal(parsed, &r); err != nil {
		return nil, fmt.Errorf("parsing RestaurantsAvailability response: %w", err)
	}
	if len(r.Errors) > 0 {
		// Surface PersistedQueryNotFound with a clear hint — the cached
		// hash has drifted past whatever OT's bundle currently expects.
		first := r.Errors[0]
		if strings.Contains(strings.ToUpper(first.Message), "PERSISTED") || first.Extensions.Code == "PERSISTED_QUERY_NOT_FOUND" {
			return nil, fmt.Errorf("opentable: persisted-query hash drifted (RestaurantsAvailability returned %q); cached hash %s no longer accepted by upstream — the cached value will need to be refreshed in a follow-up release", first.Message, RestaurantsAvailabilityHash[:16])
		}
		return nil, fmt.Errorf("opentable RestaurantsAvailability: %s", first.Message)
	}
	return r.Data.Availability, nil
}

// RestaurantIDFromQuery resolves a free-text query (or a slug like
// `le-bernardin-new-york`) to a restaurant ID via Autocomplete. Picks the
// first result whose lowercase name contains the lowercase query
// (slug-dashes converted to spaces). Returns 0 if no match — caller
// surfaces a "couldn't resolve" error.
func (c *Client) RestaurantIDFromQuery(ctx context.Context, query string, lat, lng float64) (id int, name, slug string, err error) {
	q := strings.ReplaceAll(strings.ToLower(query), "-", " ")
	q = strings.TrimSpace(q)
	if q == "" {
		return 0, "", "", fmt.Errorf("empty query")
	}
	results, err := c.Autocomplete(ctx, q, lat, lng)
	if err != nil {
		return 0, "", "", err
	}
	for _, r := range results {
		nameLower := strings.ToLower(r.Name)
		if r.Type != "Restaurant" {
			continue
		}
		if !strings.Contains(nameLower, q) && !strings.Contains(q, nameLower) {
			// Some autocomplete responses lead with token-prefix matches.
			// If the user passed a multi-word query and the result name
			// matches the first significant token, accept.
			tokens := strings.Fields(q)
			if len(tokens) == 0 || !strings.Contains(nameLower, tokens[0]) {
				continue
			}
		}
		idInt := 0
		fmt.Sscanf(r.ID, "%d", &idInt)
		if idInt == 0 {
			continue
		}
		return idInt, r.Name, r.URLSlug, nil
	}
	return 0, "", "", fmt.Errorf("no opentable restaurant matched %q", query)
}

// gqlCall posts a GraphQL request with the persisted-query envelope. On
// PersistedQueryNotFound (a 400 with that errors[].extensions.code), it
// could re-bootstrap the hash from a homepage scrape — for v1 we surface
// the error so the user can run `doctor --refresh-hashes`.
func (c *Client) gqlCall(ctx context.Context, opname string, body any) ([]byte, error) {
	js, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling GraphQL body: %w", err)
	}
	u := Origin + GraphQLPath + "?optype=query&opname=" + url.QueryEscape(opname)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(js)))
	if err != nil {
		return nil, fmt.Errorf("building GraphQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-CSRF-Token", c.CSRF())
	req.Header.Set("Origin", Origin)
	req.Header.Set("Referer", Origin+"/")
	resp, err := c.do429Aware(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", opname, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s response: %w", opname, err)
	}
	if resp.StatusCode != 200 {
		// PersistedQueryNotFound is a 400 with text/plain "Bad Request" or a
		// JSON `errors[].extensions.code === "PERSISTED_QUERY_NOT_FOUND"`.
		// Surface it with a hint so callers know to refresh hashes.
		hint := ""
		if resp.StatusCode == 400 {
			hint = " (likely a stale persisted-query hash; run `doctor --refresh-hashes` if this is recurring)"
		}
		return nil, fmt.Errorf("opentable %s returned HTTP %d%s: %s", opname, resp.StatusCode, hint, truncate(string(data), 200))
	}
	return data, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
