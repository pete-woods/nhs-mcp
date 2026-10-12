// Package record answers questions about a GP record: which test results
// match, which values fall outside their reference range, and how one
// measurement has changed over time.
package record

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
)

// Flag says where a value sits against its reference range.
type Flag string

// The flags: in range (or can't tell), below, and above.
const (
	FlagNone Flag = ""
	FlagLow  Flag = "low"
	FlagHigh Flag = "high"
)

// Filter selects test results. Zero fields match everything.
type Filter struct {
	Since, Until time.Time
	// Name matches, case-insensitively, part of the result's name or the name
	// of any value in it.
	Name string
	// OutOfRange keeps only results with a value outside its range.
	OutOfRange bool
}

// Results are a record's test results, including historic ones, newest first.
func Results(r nhsapp.Record) []nhsapp.TestResult {
	all := slices.Concat(r.TestResults.Data, r.HistoricTestResults.Data)
	slices.SortStableFunc(all, func(a, b nhsapp.TestResult) int {
		return b.Date.Time().Compare(a.Date.Time())
	})
	return all
}

// Find returns the results matching f, newest first.
func Find(r nhsapp.Record, f Filter) []nhsapp.TestResult {
	name := strings.ToLower(f.Name)
	var found []nhsapp.TestResult
	for _, tr := range Results(r) {
		at := tr.Date.Time()
		if !f.Since.IsZero() && at.Before(f.Since) {
			continue
		}
		if !f.Until.IsZero() && !at.Before(f.Until) {
			continue
		}
		if name != "" && !matchesName(tr, name) {
			continue
		}
		if f.OutOfRange && len(OutOfRange(tr)) == 0 {
			continue
		}
		found = append(found, tr)
	}
	return found
}

// ByID finds one test result.
func ByID(r nhsapp.Record, id string) (nhsapp.TestResult, bool) {
	for _, tr := range Results(r) {
		if tr.ID == id {
			return tr, true
		}
	}
	return nhsapp.TestResult{}, false
}

// OutOfRange names the values in a result that fall outside their range.
func OutOfRange(tr nhsapp.TestResult) []string {
	var names []string
	for _, v := range tr.Values {
		if FlagOf(v) != FlagNone {
			names = append(names, v.Term)
		}
	}
	return names
}

// FlagOf compares a value with its reference range. Values reported only as
// above or below a limit (e.g. "< 1") are flagged only when that's enough to
// tell, and values with no number or no range are never flagged.
func FlagOf(v nhsapp.TestResultValue) Flag {
	if v.NumericValue == nil {
		return FlagNone
	}
	n := *v.NumericValue
	low, hasLow := parse(v.Range.Minimum)
	high, hasHigh := parse(v.Range.Maximum)
	switch v.NumericOperator {
	case "<", "<=":
		if hasLow && n <= low {
			return FlagLow
		}
	case ">", ">=":
		if hasHigh && n >= high {
			return FlagHigh
		}
	default:
		if hasLow && n < low {
			return FlagLow
		}
		if hasHigh && n > high {
			return FlagHigh
		}
	}
	return FlagNone
}

// Point is one measurement of something over time.
type Point struct {
	ResultID string
	Date     nhsapp.Date
	Value    nhsapp.TestResultValue
}

// Trend returns every measurement named name, oldest first. An exact,
// case-insensitive name match wins; failing that, measurements whose names
// contain name are used. It also returns the measurement names it matched.
func Trend(r nhsapp.Record, name string) ([]Point, []string) {
	want := strings.ToLower(name)
	exact := func(term string) bool { return strings.ToLower(term) == want }
	partial := func(term string) bool { return strings.Contains(strings.ToLower(term), want) }

	for _, match := range []func(string) bool{exact, partial} {
		var points []Point
		names := map[string]bool{}
		for _, tr := range Results(r) {
			for _, v := range tr.Values {
				if match(v.Term) {
					points = append(points, Point{ResultID: tr.ID, Date: tr.Date, Value: v})
					names[v.Term] = true
				}
			}
		}
		if len(points) > 0 {
			slices.Reverse(points)
			return points, slices.Sorted(maps.Keys(names))
		}
	}
	return nil, nil
}

// Name is a test or measurement name, with how often and how recently it
// appears.
type Name struct {
	Name   string
	Count  int
	Latest nhsapp.Date
}

// Names lists the names of results (e.g. "Full blood count - FBC") and of the
// measurements in them (e.g. "Haemoglobin concentration"), most common first.
func Names(r nhsapp.Record) (tests, measurements []Name) {
	testNames := map[string]*Name{}
	valueNames := map[string]*Name{}
	for _, tr := range Results(r) {
		count(testNames, tr.Term, tr.Date)
		for _, v := range tr.Values {
			count(valueNames, v.Term, tr.Date)
		}
	}
	return sortNames(testNames), sortNames(valueNames)
}

func matchesName(tr nhsapp.TestResult, lowerName string) bool {
	if strings.Contains(strings.ToLower(tr.Term), lowerName) {
		return true
	}
	return slices.ContainsFunc(tr.Values, func(v nhsapp.TestResultValue) bool {
		return strings.Contains(strings.ToLower(v.Term), lowerName)
	})
}

func parse(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
}

// count tallies name, relying on results arriving newest first.
func count(names map[string]*Name, name string, at nhsapp.Date) {
	if name == "" {
		return
	}
	n, ok := names[name]
	if !ok {
		n = &Name{Name: name, Latest: at}
		names[name] = n
	}
	n.Count++
}

func sortNames(names map[string]*Name) []Name {
	out := make([]Name, 0, len(names))
	for _, n := range names {
		out = append(out, *n)
	}
	slices.SortFunc(out, func(a, b Name) int {
		return cmp.Or(b.Count-a.Count, strings.Compare(a.Name, b.Name))
	})
	return out
}
