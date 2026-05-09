package auth

// PATCH: cross-network-source-clients — see .printing-press-patches.json for the change-set rationale.

import (
	"context"
	"errors"
	"fmt"
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
