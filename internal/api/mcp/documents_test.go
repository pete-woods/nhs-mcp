package mcp_test

import (
	"encoding/json"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/testing/mcptest"
)

func TestListConsultations(t *testing.T) {
	fix := loggedIn(t)

	t.Run("newest first, with each heading's entries", func(t *testing.T) {
		got := mcptest.Call[consultations](t, fix.cs, "list_consultations", nil)
		assert.Check(t, cmp.DeepEqual(got, consultations{
			Consultations: []consultation{
				{
					Date:      "2023-06-05T10:20:00Z",
					Clinician: "EXAMPLE, ALEX (DR)",
					Sections: []consultationSection{
						{Heading: "History", Lines: []string{"Tired for three months", "Tiredness symptom - worse in the afternoons"}},
						{Heading: "Problem", Lines: []string{"Iron deficiency anaemia"}},
					},
				},
				{
					Date:      "2021-02-01",
					Clinician: "NURSE, SAM (NURSE)",
					Location:  "Example Surgery",
					Sections:  []consultationSection{{Heading: "Examination", Lines: []string{"O/E - blood pressure reading - 120/80"}}},
				},
			},
			Matched: 2,
		}))
	})

	t.Run("filters", func(t *testing.T) {
		got := mcptest.Call[consultations](t, fix.cs, "list_consultations", map[string]any{"contains": "BLOOD PRESSURE"})
		assert.Assert(t, cmp.Len(got.Consultations, 1))
		assert.Check(t, cmp.Equal(got.Consultations[0].Date, "2021-02-01"))

		got = mcptest.Call[consultations](t, fix.cs, "list_consultations", map[string]any{"since": "2022-01-01"})
		assert.Check(t, cmp.Len(got.Consultations, 1))

		got = mcptest.Call[consultations](t, fix.cs, "list_consultations", map[string]any{"limit": 1})
		assert.Check(t, cmp.Len(got.Consultations, 1))
		assert.Check(t, got.Truncated)
	})
}

func TestDocuments(t *testing.T) {
	fix := loggedIn(t)

	t.Run("list", func(t *testing.T) {
		got := mcptest.Call[documents](t, fix.cs, "list_documents", nil)
		assert.Check(t, cmp.DeepEqual(got.Documents, []document{
			{ID: "doc-scan", Date: "2024-01-15", Name: "Scanned discharge summary", FileName: "Discharge summary", Type: "pdf", Size: 4096, Available: true},
			{ID: "doc-letter", Date: "2023-07-01", Name: "Letter from haematology", FileName: "Haematology clinic letter", Type: "rtf", Size: 2048, Available: true},
			{ID: "doc-missing", Date: "2019-03-03", Name: "Old referral", FileName: "Referral", Type: "html", Size: 0, Available: false},
		}))

		got = mcptest.Call[documents](t, fix.cs, "list_documents", map[string]any{"contains": "haematology"})
		assert.Check(t, cmp.Len(got.Documents, 1))
	})

	t.Run("a letter is text", func(t *testing.T) {
		res := mcptest.Result(t, fix.cs, "get_document", map[string]any{"id": "doc-letter"})
		assert.Assert(t, !res.IsError)
		got := structured[documentContent](t, res)
		assert.Check(t, cmp.Equal(got.Text, "Dear Dr Example,\nFerritin has recovered on oral iron.\nFerritin | 22 ug/L"))
		assert.Check(t, cmp.Equal(got.Pages, 0))
		assert.Check(t, cmp.Len(res.Content, 1))
	})

	t.Run("a scan is page images", func(t *testing.T) {
		res := mcptest.Result(t, fix.cs, "get_document", map[string]any{"id": "doc-scan"})
		assert.Assert(t, !res.IsError)
		got := structured[documentContent](t, res)
		assert.Check(t, cmp.Equal(got.Pages, 3))
		assert.Check(t, cmp.Equal(got.PagesReturned, 2))
		assert.Check(t, cmp.Equal(got.Unshowable, 1), "TIFF pages can't be shown")
		assert.Check(t, cmp.DeepEqual(got.UnshowableTypes, []string{"image/tiff (looks like application/octet-stream)"}))
		assert.Assert(t, cmp.Len(res.Content, 3))
		img, ok := res.Content[1].(*sdk.ImageContent)
		assert.Assert(t, ok, "%T", res.Content[1])
		assert.Check(t, cmp.Equal(img.MIMEType, "image/png"))
		assert.Check(t, cmp.Contains(string(img.Data), "PNG"))
	})

	t.Run("max_pages", func(t *testing.T) {
		res := mcptest.Result(t, fix.cs, "get_document", map[string]any{"id": "doc-scan", "max_pages": 1})
		got := structured[documentContent](t, res)
		assert.Check(t, cmp.Equal(got.PagesReturned, 1))
		assert.Check(t, got.Truncated)
	})

	t.Run("unavailable and unknown documents", func(t *testing.T) {
		assert.Check(t, cmp.Contains(mcptest.Error(t, fix.cs, "get_document", map[string]any{"id": "doc-missing"}), "can't supply this document"))
		assert.Check(t, cmp.Contains(mcptest.Error(t, fix.cs, "get_document", map[string]any{"id": "nope"}), `no document with id "nope"`))
	})
}

type consultationSection struct {
	Heading string   `json:"heading"`
	Lines   []string `json:"lines"`
}

type consultation struct {
	Date      string                `json:"date"`
	Clinician string                `json:"clinician"`
	Location  string                `json:"location"`
	Sections  []consultationSection `json:"sections"`
}

type consultations struct {
	Consultations []consultation `json:"consultations"`
	Matched       int            `json:"matched"`
	Truncated     bool           `json:"truncated"`
	Warning       string         `json:"warning"`
}

type document struct {
	ID        string `json:"id"`
	Date      string `json:"date"`
	Name      string `json:"name"`
	FileName  string `json:"file_name"`
	Type      string `json:"type"`
	Size      int    `json:"size"`
	Available bool   `json:"available"`
}

type documents struct {
	Documents []document `json:"documents"`
	Matched   int        `json:"matched"`
	Truncated bool       `json:"truncated"`
}

type documentContent struct {
	document
	Text            string   `json:"text"`
	Pages           int      `json:"pages"`
	PagesReturned   int      `json:"pages_returned"`
	Truncated       bool     `json:"truncated"`
	Unshowable      int      `json:"unshowable_pages"`
	UnshowableTypes []string `json:"unshowable_types"`
}

func structured[T any](t *testing.T, res *sdk.CallToolResult) T {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	assert.NilError(t, err)
	var out T
	assert.NilError(t, json.Unmarshal(raw, &out))
	return out
}
