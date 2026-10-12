package mcp_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/testing/fakenhs"
	"github.com/pete-woods/nhs-mcp/internal/testing/mcptest"
)

func TestHistoricTestResults(t *testing.T) {
	thisYear := time.Now().Year()
	fix := start(t)
	fix.nhs.SetRecord(recordWithResults(false, resultJSON("current", thisYear, "Serum ferritin", "30")))
	fix.nhs.SetHistoric(thisYear-3, section(resultJSON("three-years-ago", thisYear-3, "Serum ferritin", "9")))
	fix.nhs.SetHistoric(thisYear-12, section(resultJSON("twelve-years-ago", thisYear-12, "Serum ferritin", "40")))
	mcptest.Call[status](t, fix.cs, "login", nil)

	t.Run("goes back ten years by default", func(t *testing.T) {
		got := mcptest.Call[listResults](t, fix.cs, "list_test_results", nil)
		assert.Check(t, cmp.DeepEqual(ids(got.Results), []string{"current", "three-years-ago"}))
		assert.Check(t, cmp.Contains(got.Warning, fmt.Sprintf("Only results since %d were searched", thisYear-10)))
		assert.Check(t, cmp.Equal(historicFetches(fix.nhs), 11))
	})

	t.Run("since goes further, fetching only the new years", func(t *testing.T) {
		got := mcptest.Call[listResults](t, fix.cs, "list_test_results", map[string]any{"since": fmt.Sprintf("%d-01-01", thisYear-15)})
		assert.Check(t, cmp.DeepEqual(ids(got.Results), []string{"current", "three-years-ago", "twelve-years-ago"}))
		assert.Check(t, cmp.Equal(got.Warning, ""))
		assert.Check(t, cmp.Equal(historicFetches(fix.nhs), 16))
	})

	t.Run("trends include them", func(t *testing.T) {
		got := mcptest.Call[trend](t, fix.cs, "get_test_trend", map[string]any{"name": "Serum ferritin"})
		assert.Check(t, cmp.Len(got.Points, 3), "every year fetched so far")
	})

	t.Run("results found earlier can be opened", func(t *testing.T) {
		got := mcptest.Call[result](t, fix.cs, "get_test_result", map[string]any{"id": "twelve-years-ago"})
		assert.Check(t, cmp.Equal(got.Date, fmt.Sprintf("%d-06-01", thisYear-12)))
	})
}

func TestHistoricTestResults_notNeeded(t *testing.T) {
	fix := loggedIn(t)
	mcptest.Call[listResults](t, fix.cs, "list_test_results", nil)
	assert.Check(t, cmp.Equal(historicFetches(fix.nhs), 0))
}

func TestTestResultDetail(t *testing.T) {
	t.Run("GP systems without values", func(t *testing.T) {
		fix := start(t)
		fix.nhs.SetTestResultsVersion("2")
		fix.nhs.SetRecord(recordWithResults(true, `{"id": "legacy", "date": {"value": "2020-01-01T00:00:00+00:00", "datePart": "YearMonthDay"}, "term": "Blood test", "testResultValues": []}`))
		fix.nhs.SetResultDetail("legacy", `<p>Serum sodium</p><table><tr><td>140</td><td>mmol/L</td></tr></table>`)
		mcptest.Call[status](t, fix.cs, "login", nil)

		got := mcptest.Call[result](t, fix.cs, "get_test_result", map[string]any{"id": "legacy"})
		assert.Check(t, cmp.Equal(got.Detail, "Serum sodium\n140 | mmol/L"))
	})

	t.Run("v3 GP systems aren't asked", func(t *testing.T) {
		fix := start(t)
		fix.nhs.SetRecord(recordWithResults(true, `{"id": "empty", "date": {"value": "2020-01-01T00:00:00+00:00", "datePart": "YearMonthDay"}, "term": "Blood test", "testResultValues": []}`))
		mcptest.Call[status](t, fix.cs, "login", nil)

		got := mcptest.Call[result](t, fix.cs, "get_test_result", map[string]any{"id": "empty"})
		assert.Check(t, cmp.Equal(got.Detail, ""))
		for _, r := range fix.nhs.Requests() {
			assert.Check(t, r.Path != "/v1/patient/test-result")
		}
	})
}

func resultJSON(id string, year int, name, value string) string {
	return fmt.Sprintf(`{"id": %q, "date": {"value": "%d-06-01T00:00:00+00:00", "datePart": "YearMonthDay"}, "term": %q,
		"testResultValues": [{"term": %q, "displayValue": "%s ug/L", "numericValue": %s, "units": "ug/L", "range": {"minimum": "5", "maximum": "150"}}]}`,
		id, year, name, name, value, value)
}

func section(results ...string) []byte {
	return []byte(`{"hasAccess": true, "hasErrored": false, "data": [` + strings.Join(results, ",") + `]}`)
}

func recordWithResults(hasAll bool, results ...string) []byte {
	return []byte(fmt.Sprintf(`{"response": {"supplier": "TEST", "testResults": {"hasAccess": true, "hasAllHistoricResults": %v, "data": [%s]}}}`,
		hasAll, strings.Join(results, ",")))
}

func historicFetches(nhs *fakenhs.Server) int {
	n := 0
	for _, r := range nhs.Requests() {
		if strings.HasPrefix(r.Path, "/v1/patient/historic-test-results/") {
			n++
		}
	}
	return n
}
