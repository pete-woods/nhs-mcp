package mcp_test

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/testing/fakenhs"
	"github.com/pete-woods/nhs-mcp/internal/testing/mcptest"
)

func TestRecordTools(t *testing.T) {
	fix := loggedIn(t)

	t.Run("medications", func(t *testing.T) {
		got := mcptest.Call[medications](t, fix.cs, "get_medications", nil)
		assert.Check(t, cmp.DeepEqual(got, medications{
			CurrentRepeat: []medication{{Date: "2023-11-10", Lines: []string{"Ferrous fumarate 210mg tablets", "One Twice A Day"}}},
			Acute:         []medication{{Date: "2024-02-01", Lines: []string{"Amoxicillin 500mg capsules", "One Three Times A Day", "21 capsule"}}},
		}))

		got = mcptest.Call[medications](t, fix.cs, "get_medications", map[string]any{"include_discontinued": true})
		assert.Check(t, cmp.DeepEqual(got.Discontinued, []medication{{Date: "2019-04-02", Lines: []string{"Salbutamol 100micrograms/dose inhaler"}}}))
	})

	t.Run("allergies", func(t *testing.T) {
		got := mcptest.Call[allergies](t, fix.cs, "get_allergies", nil)
		assert.Check(t, cmp.DeepEqual(got, allergies{Allergies: []allergy{{Date: "2001-05-20", Name: "Peanut allergy"}}}))
	})

	t.Run("problems, newest first", func(t *testing.T) {
		got := mcptest.Call[problems](t, fix.cs, "get_problems", nil)
		assert.Check(t, cmp.DeepEqual(got, problems{Problems: []problem{
			{Date: "2023-06-01", Name: "Iron deficiency anaemia", Details: []string{"Significance: Significant", "Status: Active"}},
			{Date: "2015", Name: "Asthma", Details: []string{"Status: Past", "Notes", "Nested as a bare string"}},
		}}))
	})

	t.Run("active problems", func(t *testing.T) {
		got := mcptest.Call[problems](t, fix.cs, "get_problems", map[string]any{"active_only": true})
		assert.Assert(t, cmp.Len(got.Problems, 1))
		assert.Check(t, cmp.Equal(got.Problems[0].Name, "Iron deficiency anaemia"))
	})

	t.Run("immunisations", func(t *testing.T) {
		got := mcptest.Call[immunisations](t, fix.cs, "get_immunisations", nil)
		assert.Check(t, cmp.DeepEqual(got, immunisations{Immunisations: []immunisation{
			{Date: "2023-10-05", Name: "Influenza vaccination"},
			{Date: "1999", Name: "Tetanus booster"},
		}}))
	})

	t.Run("demographics", func(t *testing.T) {
		got := mcptest.Call[map[string]string](t, fix.cs, "get_demographics", nil)
		assert.Check(t, cmp.Equal(got["date_of_birth"], "1980-04-01"))
		assert.Check(t, cmp.Equal(got["nhs_number"], fakenhs.NHSNumber))
	})
}

func TestRecordWarnings(t *testing.T) {
	fix := start(t)
	fix.nhs.SetRecord([]byte(`{"response": {
		"allergies": {"hasAccess": false},
		"testResults": {"hasAccess": true, "hasErrored": true, "data": []}
	}}`))
	mcptest.Call[status](t, fix.cs, "login", nil)

	a := mcptest.Call[allergies](t, fix.cs, "get_allergies", nil)
	assert.Check(t, cmp.Contains(a.Warning, "doesn't share allergies online"))

	r := mcptest.Call[listResults](t, fix.cs, "list_test_results", nil)
	assert.Check(t, cmp.Contains(r.Warning, "returned an error for test results"))
}

type medication struct {
	Date  string   `json:"date"`
	Lines []string `json:"lines"`
}

type medications struct {
	CurrentRepeat []medication `json:"current_repeat"`
	Acute         []medication `json:"acute"`
	Discontinued  []medication `json:"discontinued"`
	Warning       string       `json:"warning"`
}

type allergy struct {
	Date     string `json:"date"`
	Name     string `json:"name"`
	Drug     string `json:"drug"`
	Reaction string `json:"reaction"`
}

type allergies struct {
	Allergies []allergy `json:"allergies"`
	Warning   string    `json:"warning"`
}

type problem struct {
	Date    string   `json:"date"`
	Name    string   `json:"name"`
	Details []string `json:"details"`
}

type problems struct {
	Problems []problem `json:"problems"`
	Warning  string    `json:"warning"`
}

type immunisation struct {
	Date string `json:"date"`
	Name string `json:"name"`
}

type immunisations struct {
	Immunisations []immunisation `json:"immunisations"`
	Warning       string         `json:"warning"`
}
