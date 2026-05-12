// Copyright 2026 james-bongiovanni. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strings"
	"time"
)

// novelDateRange resolves a --week / --from / --to flag set into a
// concrete [start, end] date pair (YYYY-MM-DD strings, inclusive).
// "current" → ISO week containing today.
// "last"    → previous ISO week.
// "YYYY-WW" → that ISO week.
// Empty inputs default to the current ISO week.
func novelDateRange(week, from, to string) (string, string) {
	if from != "" || to != "" {
		if from == "" {
			from = to
		}
		if to == "" {
			to = from
		}
		return from, to
	}

	now := time.Now()
	switch strings.ToLower(week) {
	case "", "current":
		return isoWeekRange(now)
	case "last":
		return isoWeekRange(now.AddDate(0, 0, -7))
	default:
		// Try YYYY-WW
		parts := strings.Split(week, "-")
		if len(parts) == 2 {
			var y, w int
			if _, err := timeSscan(parts[0], &y); err == nil {
				if _, err := timeSscan(parts[1], &w); err == nil && y > 0 && w > 0 {
					t := firstDayOfISOWeek(y, w)
					return isoWeekRange(t)
				}
			}
		}
		return isoWeekRange(now)
	}
}

func timeSscan(s string, out *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadInt
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return n, nil
}

var errBadInt = &novelErr{msg: "not an integer"}

type novelErr struct{ msg string }

func (e *novelErr) Error() string { return e.msg }

func isoWeekRange(t time.Time) (string, string) {
	// Monday–Sunday week
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7 // Sunday → 7 so Monday is day 1
	}
	monday := t.AddDate(0, 0, -(wd - 1))
	sunday := monday.AddDate(0, 0, 6)
	return monday.Format("2006-01-02"), sunday.Format("2006-01-02")
}

func firstDayOfISOWeek(year, week int) time.Time {
	t := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	monday := t.AddDate(0, 0, -(wd - 1))
	return monday.AddDate(0, 0, (week-1)*7)
}

// floatFromString parses common API numeric-string fields ("12.34" → 12.34).
// Returns 0 for empty / unparseable values; novel commands treat zero as
// "no data" already.
func floatFromString(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	dot := false
	intPart := 0.0
	frac := 0.0
	div := 1.0
	for _, c := range s {
		if c == '.' {
			if dot {
				return 0
			}
			dot = true
			continue
		}
		if c < '0' || c > '9' {
			return 0
		}
		if dot {
			div *= 10
			frac = frac + float64(c-'0')/div
		} else {
			intPart = intPart*10 + float64(c-'0')
		}
	}
	v := intPart + frac
	if neg {
		v = -v
	}
	return v
}
