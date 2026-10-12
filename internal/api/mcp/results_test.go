package mcp_test

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/testing/mcptest"
)

func TestListTestResults(t *testing.T) {
	fix := loggedIn(t)

	t.Run("newest first", func(t *testing.T) {
		got := mcptest.Call[listResults](t, fix.cs, "list_test_results", nil)
		assert.Check(t, cmp.DeepEqual(ids(got.Results), []string{
			"result-fbc-2024", "result-ferritin-2024", "result-ferritin-2023",
			"result-renal-2022", "result-crp-2022", "result-urine-2010",
		}))
		assert.Check(t, cmp.Equal(got.Matched, 6))
		assert.Check(t, !got.Truncated)
		assert.Check(t, cmp.Equal(got.Warning, ""))
	})

	t.Run("summaries", func(t *testing.T) {
		got := mcptest.Call[listResults](t, fix.cs, "list_test_results", nil)
		assert.Check(t, cmp.DeepEqual(got.Results[0], resultSummary{
			ID:         "result-fbc-2024",
			Date:       "2024-03-14",
			Name:       "Full blood count - FBC",
			Values:     2,
			OutOfRange: []string{"Haemoglobin concentration"},
		}))
		assert.Check(t, cmp.Equal(got.Results[5].Date, "2010"), "dates keep only their known precision")
	})

	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{
			name: "by test name, any case",
			args: map[string]any{"name": "FERRITIN"},
			want: []string{"result-ferritin-2024", "result-ferritin-2023"},
		},
		{
			name: "by measurement name",
			args: map[string]any{"name": "sodium"},
			want: []string{"result-renal-2022"},
		},
		{
			name: "ignoring punctuation",
			args: map[string]any{"name": "c-reactive protein"},
			want: []string{"result-crp-2022"},
		},
		{
			name: "by a common name",
			args: map[string]any{"name": "Kidney"},
			want: []string{"result-renal-2022"},
		},
		{
			name: "out of range only",
			args: map[string]any{"out_of_range_only": true},
			want: []string{"result-fbc-2024", "result-ferritin-2023"},
		},
		{
			name: "since and until are inclusive",
			args: map[string]any{"since": "2022-01-10", "until": "2023-06-01"},
			want: []string{"result-ferritin-2023", "result-renal-2022", "result-crp-2022"},
		},
		{
			name: "nothing matches",
			args: map[string]any{"name": "cholesterol"},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mcptest.Call[listResults](t, fix.cs, "list_test_results", tt.args)
			assert.Check(t, cmp.DeepEqual(ids(got.Results), tt.want))
		})
	}

	t.Run("says why nothing matched", func(t *testing.T) {
		got := mcptest.Call[listResults](t, fix.cs, "list_test_results", map[string]any{"name": "cholesterol"})
		assert.Check(t, cmp.Contains(got.Note, `No test or measurement in the searched results is named "cholesterol"`))

		got = mcptest.Call[listResults](t, fix.cs, "list_test_results", map[string]any{"name": "ferritin"})
		assert.Check(t, cmp.Equal(got.Note, ""))
	})

	t.Run("limit truncates", func(t *testing.T) {
		got := mcptest.Call[listResults](t, fix.cs, "list_test_results", map[string]any{"limit": 2})
		assert.Check(t, cmp.Len(got.Results, 2))
		assert.Check(t, cmp.Equal(got.Matched, 6))
		assert.Check(t, got.Truncated)
	})

	t.Run("bad dates", func(t *testing.T) {
		msg := mcptest.Error(t, fix.cs, "list_test_results", map[string]any{"since": "last year"})
		assert.Check(t, cmp.Contains(msg, "since must be a date like 2024-01-31"))
	})
}

func TestGetTestResult(t *testing.T) {
	fix := loggedIn(t)

	t.Run("values with flags", func(t *testing.T) {
		got := mcptest.Call[result](t, fix.cs, "get_test_result", map[string]any{"id": "result-fbc-2024"})
		assert.Check(t, cmp.DeepEqual(got, result{
			ID:   "result-fbc-2024",
			Date: "2024-03-14",
			Name: "Full blood count - FBC",
			Values: []measurement{
				{Name: "Haemoglobin concentration", Value: "112 g/L", Numeric: ptr(112.0), Units: "g/L", RangeLow: "115", RangeHigh: "165", Flag: "low"},
				{Name: "White blood cell count", Value: "6.1 10*9/L", Numeric: ptr(6.1), Units: "10*9/L", RangeLow: "4", RangeHigh: "11"},
			},
		}))
	})

	t.Run("notes", func(t *testing.T) {
		got := mcptest.Call[result](t, fix.cs, "get_test_result", map[string]any{"id": "result-ferritin-2023"})
		assert.Check(t, cmp.DeepEqual(got.Notes, []string{"(dr.example) - Abnormal - start iron"}))

		got = mcptest.Call[result](t, fix.cs, "get_test_result", map[string]any{"id": "result-ferritin-2024"})
		assert.Assert(t, cmp.Len(got.Values, 1))
		assert.Check(t, cmp.DeepEqual(got.Values[0].Notes, []string{"Assay changed March 2024"}))
	})

	t.Run("values only known to be above or below a limit", func(t *testing.T) {
		got := mcptest.Call[result](t, fix.cs, "get_test_result", map[string]any{"id": "result-crp-2022"})
		assert.Assert(t, cmp.Len(got.Values, 1))
		assert.Check(t, cmp.Equal(got.Values[0].Operator, "<"))
		assert.Check(t, cmp.Equal(got.Values[0].Flag, ""))
	})

	t.Run("unknown id", func(t *testing.T) {
		msg := mcptest.Error(t, fix.cs, "get_test_result", map[string]any{"id": "nope"})
		assert.Check(t, cmp.Contains(msg, `no test result with id "nope"`))
	})
}

func TestGetTestTrend(t *testing.T) {
	fix := loggedIn(t)

	t.Run("oldest first", func(t *testing.T) {
		got := mcptest.Call[trend](t, fix.cs, "get_test_trend", map[string]any{"name": "serum ferritin"})
		assert.Check(t, cmp.DeepEqual(got.Measurements, []string{"Serum ferritin"}))
		assert.Assert(t, cmp.Len(got.Points, 2))
		assert.Check(t, cmp.Equal(got.Points[0].Date, "2023-06-01"))
		assert.Check(t, cmp.Equal(got.Points[0].ResultID, "result-ferritin-2023"))
		assert.Check(t, cmp.Equal(got.Points[0].Flag, "low"))
		assert.Check(t, cmp.Equal(got.Points[1].Value, "22 ug/L"))
		assert.Check(t, cmp.Equal(got.Points[1].Flag, ""))
	})

	t.Run("partial names", func(t *testing.T) {
		got := mcptest.Call[trend](t, fix.cs, "get_test_trend", map[string]any{"name": "Serum"})
		assert.Check(t, cmp.DeepEqual(got.Measurements, []string{"Serum C reactive protein level", "Serum ferritin", "Serum sodium"}))
		assert.Check(t, cmp.Len(got.Points, 4))
	})

	t.Run("common names", func(t *testing.T) {
		got := mcptest.Call[trend](t, fix.cs, "get_test_trend", map[string]any{"name": "kidney"})
		assert.Check(t, cmp.DeepEqual(got.Measurements, []string{"eGFR using creatinine (CKD-EPI)"}))
		assert.Check(t, cmp.Len(got.Points, 1))
	})

	t.Run("since", func(t *testing.T) {
		got := mcptest.Call[trend](t, fix.cs, "get_test_trend", map[string]any{"name": "serum ferritin", "since": "2024-01-01"})
		assert.Check(t, cmp.Len(got.Points, 1))
	})

	t.Run("no match", func(t *testing.T) {
		got := mcptest.Call[trend](t, fix.cs, "get_test_trend", map[string]any{"name": "cholesterol"})
		assert.Check(t, cmp.DeepEqual(got.Measurements, []string{}))
		assert.Check(t, cmp.DeepEqual(got.Points, []trendPoint{}))
		assert.Check(t, cmp.Contains(got.Note, "list_test_names lists every name"))
	})

	t.Run("name is required", func(t *testing.T) {
		assert.Check(t, cmp.Contains(mcptest.Error(t, fix.cs, "get_test_trend", nil), `missing properties: ["name"]`))
		assert.Check(t, cmp.Contains(mcptest.Error(t, fix.cs, "get_test_trend", map[string]any{"name": ""}), "name is required"))
	})
}

func TestListTestNames(t *testing.T) {
	fix := loggedIn(t)

	got := mcptest.Call[names](t, fix.cs, "list_test_names", nil)
	assert.Assert(t, cmp.Len(got.Tests, 5))
	assert.Check(t, cmp.DeepEqual(got.Tests[0], name{Name: "Serum ferritin", Count: 2, Latest: "2024-03-14"}))
	assert.Check(t, cmp.Len(got.Measurements, 7))
}

type resultSummary struct {
	ID         string   `json:"id"`
	Date       string   `json:"date"`
	Name       string   `json:"name"`
	Values     int      `json:"values"`
	OutOfRange []string `json:"out_of_range"`
}

type listResults struct {
	Results   []resultSummary `json:"results"`
	Matched   int             `json:"matched"`
	Truncated bool            `json:"truncated"`
	Warning   string          `json:"warning"`
	Note      string          `json:"note"`
}

type measurement struct {
	Name      string   `json:"name"`
	Value     string   `json:"value"`
	Numeric   *float64 `json:"numeric"`
	Operator  string   `json:"operator"`
	Units     string   `json:"units"`
	RangeLow  string   `json:"range_low"`
	RangeHigh string   `json:"range_high"`
	Flag      string   `json:"flag"`
	Notes     []string `json:"notes"`
}

type result struct {
	ID     string        `json:"id"`
	Date   string        `json:"date"`
	Name   string        `json:"name"`
	Notes  []string      `json:"notes"`
	Values []measurement `json:"values"`
	Detail string        `json:"detail"`
}

type trendPoint struct {
	Date     string `json:"date"`
	ResultID string `json:"result_id"`
	measurement
}

type trend struct {
	Measurements []string     `json:"measurements"`
	Points       []trendPoint `json:"points"`
	Note         string       `json:"note"`
}

type name struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Latest string `json:"latest"`
}

type names struct {
	Tests        []name `json:"tests"`
	Measurements []name `json:"measurements"`
}

func ids(results []resultSummary) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.ID)
	}
	return out
}

func ptr[T any](v T) *T {
	return &v
}
