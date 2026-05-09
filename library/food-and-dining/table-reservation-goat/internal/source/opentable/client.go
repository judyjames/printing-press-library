// Package opentable wraps OpenTable's consumer surface (the dapi GraphQL
// endpoint, REST booking, and SSR-rendered HTML pages). The OpenTable
// Partner API is out of scope.
//
// Auth model: OpenTable's bot defense (Akamai) requires a Chrome TLS
// fingerprint AND, for authenticated reads, the `authCke` session cookie
// the user has after logging in to opentable.com. We use enetx/surf for
// the TLS fingerprint and the session cookies imported via auth login.
package opentable

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

	defaultTimeout = 30 * time.Second
)

// Client is a Surf-based OpenTable client with the user's session cookies
// attached.
type Client struct {
	mu      sync.Mutex
	http    *http.Client
	session *auth.Session

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
		csrfTTL:       30 * time.Minute,
		autocompleteH: AutocompleteHash,
	}, nil
}

// LoggedIn reports whether the client is configured with an OpenTable
// session cookie.
func (c *Client) LoggedIn() bool {
	return c.session != nil && c.session.LoggedIn(auth.NetworkOpenTable)
}

// Bootstrap fetches the OpenTable homepage to extract `__CSRF_TOKEN__`.
// Idempotent — only refreshes when the cached token is older than csrfTTL.
func (c *Client) Bootstrap(ctx context.Context) error {
	c.mu.Lock()
	if c.csrfToken != "" && time.Since(c.csrfFetchedAt) < c.csrfTTL {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Origin+"/", nil)
	if err != nil {
		return fmt.Errorf("building bootstrap request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("fetching opentable.com: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("opentable.com returned HTTP %d during bootstrap", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading bootstrap body: %w", err)
	}
	token := extractCSRFToken(body)
	if token == "" {
		return errors.New("could not extract __CSRF_TOKEN__ from opentable.com homepage; site structure may have changed")
	}
	c.mu.Lock()
	c.csrfToken = token
	c.csrfFetchedAt = time.Now()
	c.mu.Unlock()
	return nil
}

var csrfRE = regexp.MustCompile(`window\.__CSRF_TOKEN__\s*=\s*['"]([0-9a-fA-F-]{16,})['"]`)

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
	resp, err := c.http.Do(req)
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
