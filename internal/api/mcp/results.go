package mcp

import (
	"context"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
	"github.com/pete-woods/nhs-mcp/internal/record"
)

const (
	defaultResultsLimit = 50
	maxResultsLimit     = 500
	maxNamesLimit       = 1000
)

func (a *api) addResultTools(s *sdk.Server) {
	sdk.AddTool(s, &sdk.Tool{
		Name: "list_test_results",
		Description: `List the user's GP test results, newest first: id, date, name, how many values it has, and which values are outside their reference range.
Filter by since/until (YYYY-MM-DD), name (part of a test or measurement name, any case), and out_of_range_only. limit defaults to 50, at most 500; truncated says there were more.
Pass an id to get_test_result for the values.`,
		Annotations: &sdk.ToolAnnotations{Title: "List test results", ReadOnlyHint: true},
	}, a.listResults)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_test_result",
		Description: `Get one test result from list_test_results by id: every value with its units, reference range, a low/high flag, and any notes from the lab or GP.`,
		Annotations: &sdk.ToolAnnotations{Title: "Get test result", ReadOnlyHint: true},
	}, a.getResult)

	sdk.AddTool(s, &sdk.Tool{
		Name: "get_test_trend",
		Description: `Follow one measurement over time, oldest first, across every result it appears in, e.g. "Serum ferritin" or "Haemoglobin concentration".
An exact name (any case) is used if there is one, otherwise every measurement whose name contains it; measurements lists the names matched. list_test_names has the exact names. since (YYYY-MM-DD) limits how far back.`,
		Annotations: &sdk.ToolAnnotations{Title: "Get test trend", ReadOnlyHint: true},
	}, a.getTrend)

	sdk.AddTool(s, &sdk.Tool{
		Name: "list_test_names",
		Description: `List the names of the user's tests (e.g. "Full blood count - FBC") and of the measurements in them (e.g. "Serum sodium"), with how many times each appears and the latest date, most common first.
Use the measurement names with get_test_trend, and either with list_test_results' name filter.`,
		Annotations: &sdk.ToolAnnotations{Title: "List test names", ReadOnlyHint: true},
	}, a.listNames)
}

type listResultsInput struct {
	Since          string `json:"since,omitempty" jsonschema:"Only results on or after this date, YYYY-MM-DD."`
	Until          string `json:"until,omitempty" jsonschema:"Only results on or before this date, YYYY-MM-DD."`
	Name           string `json:"name,omitempty" jsonschema:"Only results whose test or measurement names contain this, in any case."`
	OutOfRangeOnly bool   `json:"out_of_range_only,omitempty" jsonschema:"Only results with a value outside its reference range."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum results to return. Defaults to 50, at most 500."`
}

type resultSummary struct {
	ID         string   `json:"id"`
	Date       string   `json:"date"`
	Name       string   `json:"name"`
	Values     int      `json:"values"`
	OutOfRange []string `json:"out_of_range,omitempty" jsonschema:"Names of the values outside their reference range."`
}

type listResultsOutput struct {
	Results   []resultSummary `json:"results"`
	Matched   int             `json:"matched"`
	Truncated bool            `json:"truncated"`
	Warning   string          `json:"warning,omitempty"`
}

func (a *api) listResults(ctx context.Context, _ *sdk.CallToolRequest, in listResultsInput) (*sdk.CallToolResult, *listResultsOutput, error) {
	since, err := parseDate("since", in.Since)
	if err != nil {
		return nil, nil, err
	}
	until, err := parseDate("until", in.Until)
	if err != nil {
		return nil, nil, err
	}
	if !until.IsZero() {
		until = until.AddDate(0, 0, 1) // until is inclusive
	}
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}

	found := record.Find(r, record.Filter{Since: since, Until: until, Name: in.Name, OutOfRange: in.OutOfRangeOnly})
	shown, truncated := clip(found, in.Limit, defaultResultsLimit, maxResultsLimit)
	out := &listResultsOutput{
		Results:   make([]resultSummary, 0, len(shown)),
		Matched:   len(found),
		Truncated: truncated,
		Warning:   resultsWarning(r),
	}
	for _, tr := range shown {
		out.Results = append(out.Results, resultSummary{
			ID:         tr.ID,
			Date:       tr.Date.String(),
			Name:       tr.Term,
			Values:     len(tr.Values),
			OutOfRange: record.OutOfRange(tr),
		})
	}
	return nil, out, nil
}

type getResultInput struct {
	ID string `json:"id" jsonschema:"A result id from list_test_results."`
}

type measurement struct {
	Name      string   `json:"name"`
	Value     string   `json:"value" jsonschema:"As the GP system shows it, e.g. \"140 mmol/L\" or \"NEGATIVE\"."`
	Numeric   *float64 `json:"numeric,omitempty"`
	Operator  string   `json:"operator,omitempty" jsonschema:"\"<\" or \">\" when the value is only known to be below or above numeric."`
	Units     string   `json:"units,omitempty"`
	RangeLow  string   `json:"range_low,omitempty"`
	RangeHigh string   `json:"range_high,omitempty"`
	Flag      string   `json:"flag,omitempty" jsonschema:"\"low\" or \"high\" when outside the reference range."`
	Notes     []string `json:"notes,omitempty"`
}

type getResultOutput struct {
	ID     string        `json:"id"`
	Date   string        `json:"date"`
	Name   string        `json:"name"`
	Notes  []string      `json:"notes,omitempty" jsonschema:"Comments on the whole result, e.g. from the GP who reviewed it."`
	Values []measurement `json:"values"`
}

func (a *api) getResult(ctx context.Context, _ *sdk.CallToolRequest, in getResultInput) (*sdk.CallToolResult, *getResultOutput, error) {
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	tr, ok := record.ByID(r, in.ID)
	if !ok {
		return nil, nil, fmt.Errorf("no test result with id %q: use an id from list_test_results", in.ID)
	}
	out := &getResultOutput{
		ID:     tr.ID,
		Date:   tr.Date.String(),
		Name:   tr.Term,
		Notes:  notes(tr.AssociatedTexts),
		Values: make([]measurement, 0, len(tr.Values)),
	}
	for _, v := range tr.Values {
		out.Values = append(out.Values, toMeasurement(v))
	}
	return nil, out, nil
}

type getTrendInput struct {
	Name  string `json:"name" jsonschema:"A measurement name, e.g. \"Serum ferritin\". See list_test_names."`
	Since string `json:"since,omitempty" jsonschema:"Only measurements on or after this date, YYYY-MM-DD."`
}

type trendPoint struct {
	Date     string `json:"date"`
	ResultID string `json:"result_id"`
	measurement
}

type getTrendOutput struct {
	Measurements []string     `json:"measurements" jsonschema:"The measurement names matched."`
	Points       []trendPoint `json:"points"`
}

func (a *api) getTrend(ctx context.Context, _ *sdk.CallToolRequest, in getTrendInput) (*sdk.CallToolResult, *getTrendOutput, error) {
	if in.Name == "" {
		return nil, nil, fmt.Errorf("name is required: see list_test_names")
	}
	since, err := parseDate("since", in.Since)
	if err != nil {
		return nil, nil, err
	}
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	points, names := record.Trend(r, in.Name)
	out := &getTrendOutput{Measurements: names, Points: []trendPoint{}}
	if names == nil {
		out.Measurements = []string{}
	}
	for _, p := range points {
		if !since.IsZero() && p.Date.Time().Before(since) {
			continue
		}
		out.Points = append(out.Points, trendPoint{Date: p.Date.String(), ResultID: p.ResultID, measurement: toMeasurement(p.Value)})
	}
	return nil, out, nil
}

type testName struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Latest string `json:"latest"`
}

type listNamesOutput struct {
	Tests        []testName `json:"tests"`
	Measurements []testName `json:"measurements"`
}

func (a *api) listNames(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, *listNamesOutput, error) {
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	tests, measurements := record.Names(r)
	return nil, &listNamesOutput{Tests: toNames(tests), Measurements: toNames(measurements)}, nil
}

func toMeasurement(v nhsapp.TestResultValue) measurement {
	return measurement{
		Name:      v.Term,
		Value:     v.DisplayValue,
		Numeric:   v.NumericValue,
		Operator:  v.NumericOperator,
		Units:     v.Units,
		RangeLow:  v.Range.Minimum,
		RangeHigh: v.Range.Maximum,
		Flag:      string(record.FlagOf(v)),
		Notes:     notes(v.AssociatedTexts),
	}
}

func toNames(names []record.Name) []testName {
	names, _ = clip(names, maxNamesLimit, maxNamesLimit, maxNamesLimit)
	out := make([]testName, 0, len(names))
	for _, n := range names {
		out = append(out, testName{Name: n.Name, Count: n.Count, Latest: n.Latest.String()})
	}
	return out
}

func notes(texts []nhsapp.AssociatedText) []string {
	var out []string
	for _, t := range texts {
		if t.Text != "" {
			out = append(out, t.Text)
		}
	}
	return out
}

func resultsWarning(r nhsapp.Record) string {
	if w := sectionWarning("test results", r.TestResults.SectionStatus); w != "" {
		return w
	}
	if !r.TestResults.HasAllHistoricResults && len(r.HistoricTestResults.Data) == 0 {
		return "Older test results may be missing: the GP system didn't return all historic results."
	}
	return ""
}
