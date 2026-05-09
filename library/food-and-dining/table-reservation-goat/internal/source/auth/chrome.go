package auth

// PATCH: cross-network-source-clients — see .printing-press-patches.json for the change-set rationale.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/chrome"
)

// shortErr trims a kooky multi-error to its first line to keep notes scannable.
func shortErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.Index(s, "\n"); i > 0 {
		return s[:i] + " (and others)"
	}
	return s
}

// ImportChromeResult reports how many cookies were imported per network.
type ImportChromeResult struct {
	OpenTableImported int
	TockImported      int
	OpenTableSkipped  int
	TockSkipped       int
	Notes             []string
}

// ImportFromChrome reads cookies from local Chrome (and chrome-family)
// stores via kooky, filters for opentable.com and exploretock.com, and
// returns them in our on-disk shape. macOS Chrome encrypts cookies with a
// key in the system keychain, so the user may be prompted by macOS to
// authorize keychain access.
func ImportFromChrome(ctx context.Context) (otCookies, tockCookies []Cookie, result *ImportChromeResult, err error) {
	result = &ImportChromeResult{}
	// kooky returns a non-nil error when ANY cookie store fails to open,
	// even when other stores returned cookies successfully. The errors from
	// missing Chrome 96+ Network/Cookies paths or absent Chrome Canary are
	// expected and non-fatal — we keep the cookies that did read and surface
	// the error text as a note.
	otRaw, otErr := kooky.ReadCookies(ctx, kooky.DomainHasSuffix("opentable.com"))
	tockRaw, tockErr := kooky.ReadCookies(ctx, kooky.DomainHasSuffix("exploretock.com"))
	if otErr != nil && len(otRaw) == 0 && tockErr != nil && len(tockRaw) == 0 {
		return nil, nil, result, fmt.Errorf("reading chrome cookies (ot=%v, tock=%v); is Chrome installed and have you signed in to opentable.com / exploretock.com?", otErr, tockErr)
	}
	if otErr != nil && len(otRaw) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("OpenTable: read %d cookies; some stores failed (non-fatal): %s", len(otRaw), shortErr(otErr)))
	} else if otErr != nil {
		result.Notes = append(result.Notes, "OpenTable cookie read failed: "+shortErr(otErr))
	}
	if tockErr != nil && len(tockRaw) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("Tock: read %d cookies; some stores failed (non-fatal): %s", len(tockRaw), shortErr(tockErr)))
	} else if tockErr != nil {
		result.Notes = append(result.Notes, "Tock cookie read failed: "+shortErr(tockErr))
	}
	now := time.Now()
	convert := func(in kooky.Cookies, network string) []Cookie {
		var out []Cookie
		for _, c := range in {
			if c == nil {
				continue
			}
			if !c.Expires.IsZero() && c.Expires.Before(now) {
				if network == NetworkOpenTable {
					result.OpenTableSkipped++
				} else {
					result.TockSkipped++
				}
				continue
			}
			out = append(out, Cookie{
				Name:    c.Name,
				Value:   c.Value,
				Domain:  c.Domain,
				Path:    c.Path,
				Expires: c.Expires,
			})
			if network == NetworkOpenTable {
				result.OpenTableImported++
			} else {
				result.TockImported++
			}
		}
		return out
	}
	otCookies = convert(otRaw, NetworkOpenTable)
	tockCookies = convert(tockRaw, NetworkTock)
	if len(otCookies) == 0 && len(tockCookies) == 0 {
		return nil, nil, result, errors.New("no usable opentable.com or exploretock.com cookies found in Chrome; sign in to both sites in Chrome and re-run")
	}
	return otCookies, tockCookies, result, nil
}

// akamaiCookieNames are Akamai's anti-bot cookies. They're short-lived
// (`bm_sz` ~30min, `ftc` ~30min, `_abck` rotates frequently) and Chrome
// refreshes them automatically as the user browses opentable.com. The
// snapshot saved by `auth login --chrome` goes stale within the hour;
// re-reading the Chrome jar at every client construction keeps Akamai
// satisfied without forcing the user to re-run login.
var akamaiCookieNames = map[string]bool{
	"_abck":   true,
	"bm_sz":   true,
	"bm_sv":   true,
	"bm_s":    true,
	"bm_so":   true,
	"bm_lso":  true,
	"bm_mi":   true,
	"ak_bmsc": true,
	"ftc":     true,
}

// akamaiCacheTTL bounds how long a kooky-read snapshot is reused between
// invocations. The Akamai cookies themselves rotate every ~30min, so a
// shorter TTL here keeps us within Chrome's freshness; a longer TTL would
// risk re-walking back into stale-cookie 403s. 10 minutes is the
// compromise: well within rotation, but short enough to honor a
// just-finished Chrome browse.
const akamaiCacheTTL = 10 * time.Minute

// akamaiReadTimeout caps how long kooky may block. macOS routes Chrome's
// cookie decryption through the keychain — the very first read after a
// rebuild prompts the user to click "Always Allow." Until they do, the
// read hangs. 10s gives the user a reasonable window without freezing the
// CLI indefinitely. Override with TABLE_RESERVATION_GOAT_AKAMAI_TIMEOUT.
var akamaiReadTimeout = func() time.Duration {
	if v := os.Getenv("TABLE_RESERVATION_GOAT_AKAMAI_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 10 * time.Second
}()

// akamaiCachePath returns the on-disk cache for fresh Akamai cookies, keyed
// by domain suffix. Lives next to the cooldown file under the standard
// XDG-style cache directory.
func akamaiCachePath(domainSuffix string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	safe := strings.ReplaceAll(domainSuffix, "/", "_")
	return filepath.Join(dir, "table-reservation-goat-pp-cli", "akamai-"+safe+".json"), nil
}

type akamaiCacheFile struct {
	FetchedAt time.Time `json:"fetched_at"`
	// TimedOut is true when the kooky read hit our deadline without
	// returning. Cached as a "negative" entry so subsequent invocations
	// don't pay the full timeout; recovery is `auth login --chrome` which
	// overwrites the cache.
	TimedOut bool     `json:"timed_out,omitempty"`
	Cookies  []Cookie `json:"cookies,omitempty"`
}

// loadAkamaiCacheRaw returns the cache file directly so callers can
// distinguish "no fresh cookies in Chrome" (positive cache hit, empty)
// from "kooky was blocked, don't retry" (negative cache hit) from
// "no cache exists, must read kooky."
func loadAkamaiCacheRaw(domainSuffix string) *akamaiCacheFile {
	path, err := akamaiCachePath(domainSuffix)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cf akamaiCacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil
	}
	if time.Since(cf.FetchedAt) > akamaiCacheTTL {
		return nil
	}
	now := time.Now()
	fresh := cf.Cookies[:0]
	for _, c := range cf.Cookies {
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			continue
		}
		fresh = append(fresh, c)
	}
	cf.Cookies = fresh
	return &cf
}

func saveAkamaiCache(domainSuffix string, cf akamaiCacheFile) {
	path, err := akamaiCachePath(domainSuffix)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	cf.FetchedAt = time.Now()
	data, err := json.Marshal(cf)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// ClearAkamaiCache removes the cache for a domain suffix so the next
// RefreshAkamaiCookies call hits Chrome directly. Called by `auth login
// --chrome` so a deliberate refresh always re-walks the keychain.
func ClearAkamaiCache(domainSuffix string) {
	if path, err := akamaiCachePath(domainSuffix); err == nil {
		_ = os.Remove(path)
	}
}

// RefreshAkamaiCookies returns the live Akamai bot-defense cookies for the
// given domain suffix. The result is cached on disk for ~10 minutes; cache
// hits skip the kooky/keychain walk entirely. On a cache miss it asks
// kooky with a 10s deadline so a missing keychain authorization doesn't
// hang the CLI indefinitely. Returns nil when Chrome is unreachable or the
// keychain prompt times out — callers fall back to whatever's in the
// session jar.
//
// kooky on macOS routes Chrome's cookie decryption through the keychain,
// which can block on a user-facing dialog the first time after a rebuild.
// 10s gives the user a reasonable window to click "Always Allow"; once
// they do, the cache covers the next ten minutes' worth of CLI calls.
func RefreshAkamaiCookies(ctx context.Context, domainSuffix string) []Cookie {
	if cf := loadAkamaiCacheRaw(domainSuffix); cf != nil {
		// A negative cache entry (timed out within the last TTL) means
		// "don't retry — the user needs to run `auth login --chrome` to
		// approve keychain access." Returning nil immediately keeps each
		// CLI invocation snappy instead of paying the full timeout every
		// command.
		if cf.TimedOut {
			return nil
		}
		return cf.Cookies
	}
	rctx, cancel := context.WithTimeout(ctx, akamaiReadTimeout)
	defer cancel()
	ch := make(chan []Cookie, 1)
	go func() {
		raw, _ := kooky.ReadCookies(rctx, kooky.DomainHasSuffix(domainSuffix))
		out := make([]Cookie, 0, len(raw))
		now := time.Now()
		for _, c := range raw {
			if c == nil {
				continue
			}
			if !akamaiCookieNames[c.Name] {
				continue
			}
			if !c.Expires.IsZero() && c.Expires.Before(now) {
				continue
			}
			out = append(out, Cookie{
				Name:    c.Name,
				Value:   c.Value,
				Domain:  c.Domain,
				Path:    c.Path,
				Expires: c.Expires,
			})
		}
		ch <- out
	}()
	select {
	case r := <-ch:
		saveAkamaiCache(domainSuffix, akamaiCacheFile{Cookies: r})
		return r
	case <-rctx.Done():
		// Persist a negative cache so we don't pay 10s on every
		// subsequent command for the same TTL window.
		saveAkamaiCache(domainSuffix, akamaiCacheFile{TimedOut: true})
		return nil
	}
}
