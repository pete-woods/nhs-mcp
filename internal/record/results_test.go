package record_test

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
	"github.com/pete-woods/nhs-mcp/internal/record"
)

func TestFlagOf(t *testing.T) {
	tests := []struct {
		name     string
		value    *float64
		operator string
		low      string
		high     string
		want     record.Flag
	}{
		{name: "in range", value: ptr(140), low: "133", high: "145", want: record.FlagNone},
		{name: "on the limits", value: ptr(133), low: "133", high: "145", want: record.FlagNone},
		{name: "low", value: ptr(9), low: "15", high: "150", want: record.FlagLow},
		{name: "high", value: ptr(151), low: "15", high: "150", want: record.FlagHigh},
		{name: "only a maximum", value: ptr(12), high: "10", want: record.FlagHigh},
		{name: "only a minimum", value: ptr(1), low: "2", want: record.FlagLow},
		{name: "no range", value: ptr(90), want: record.FlagNone},
		{name: "no number", low: "1", high: "2", want: record.FlagNone},
		{name: "unparseable range", value: ptr(5), low: "n/a", high: "?", want: record.FlagNone},
		{name: "below a value within range", value: ptr(1), operator: "<", high: "10", want: record.FlagNone},
		{name: "below a value at the minimum", value: ptr(2), operator: "<", low: "2", high: "10", want: record.FlagLow},
		{name: "below a value over the maximum can't tell", value: ptr(20), operator: "<", high: "10", want: record.FlagNone},
		{name: "above a value at the maximum", value: ptr(10), operator: ">", high: "10", want: record.FlagHigh},
		{name: "above a value with no range", value: ptr(90), operator: ">", want: record.FlagNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := nhsapp.TestResultValue{
				NumericValue:    tt.value,
				NumericOperator: tt.operator,
				Range:           nhsapp.Range{Minimum: tt.low, Maximum: tt.high},
			}
			assert.Check(t, cmp.Equal(record.FlagOf(v), tt.want))
		})
	}
}

func TestDate_String(t *testing.T) {
	tests := []struct {
		date nhsapp.Date
		want string
	}{
		{date: nhsapp.Date{Value: "1992-01-01T00:00:00+00:00", DatePart: "Year"}, want: "1992"},
		{date: nhsapp.Date{Value: "1992-03-01T00:00:00+00:00", DatePart: "YearMonth"}, want: "1992-03"},
		{date: nhsapp.Date{Value: "1992-03-04T00:00:00+00:00", DatePart: "YearMonthDay"}, want: "1992-03-04"},
		{date: nhsapp.Date{Value: "1992-03-04T10:11:12+00:00", DatePart: "YearMonthDayTime"}, want: "1992-03-04T10:11:12Z"},
		{date: nhsapp.Date{}, want: ""},
	}
	for _, tt := range tests {
		assert.Check(t, cmp.Equal(tt.date.String(), tt.want), "%v", tt.date)
	}
}

func TestFind_Names(t *testing.T) {
	r := recordOf(
		result("lipids", "CVD PP Lipid profile", "Serum cholesterol/HDL ratio", "Se non HDL cholesterol level", "Serum triglycerides"),
		result("psa", "PSA", "Se prostate specific Ag level"),
		result("hba1c", "HbA1c level (DCCT aligned)", "HbA1c levl - IFCC standardised"),
		result("bp", "O/E - blood pressure reading", "Systolic arterial pressure", "Diastolic arterial pressure"),
		result("vitd", "Vitamin D", "Serum 25-OH vitamin D3 level"),
		result("renal", "Renal profile", "eGFRcreat (CKD-EPI)/1.73 m*2", "Serum sodium"),
		result("lft", "Liver function test", "Serum ALT level"),
	)
	tests := []struct {
		name string
		want []string
	}{
		{name: "lipid", want: []string{"lipids"}},
		{name: "lipids", want: []string{"lipids"}},
		{name: "cholesterol hdl ratio", want: []string{"lipids"}},
		{name: "serum non-HDL cholesterol", want: []string{"lipids"}},
		{name: "triglyceride", want: []string{"lipids"}},
		{name: "prostate specific antigen", want: []string{"psa"}},
		{name: "hba1c level", want: []string{"hba1c"}},
		{name: "blood sugar", want: []string{"hba1c"}},
		{name: "blood pressure", want: []string{"bp"}},
		{name: "BP", want: []string{"bp"}},
		{name: "vit d", want: []string{"vitd"}},
		{name: "25-hydroxyvitamin D", want: []string{"vitd"}},
		{name: "kidney function", want: []string{"renal"}},
		{name: "egfr", want: []string{"renal"}},
		{name: "U&E", want: []string{"renal"}},
		{name: "LFTs", want: []string{"lft"}},
		{name: "alanine aminotransferase", want: []string{"lft"}},
		{name: "ess", want: nil},
		{name: "ferritin", want: nil},
		{name: "salt", want: nil},
	}
	for _, tt := range tests {
		var got []string
		for _, tr := range record.Find(r, record.Filter{Name: tt.name}) {
			got = append(got, tr.ID)
		}
		assert.Check(t, cmp.DeepEqual(got, tt.want), "name %q", tt.name)
	}
}

func TestTrend_Names(t *testing.T) {
	r := recordOf(
		result("bp", "O/E - blood pressure reading", "Systolic arterial pressure", "Diastolic arterial pressure"),
		result("vitd", "Vitamin D", "Serum 25-OH vitamin D3 level", "Serum vitamin D2 level"),
		result("psa", "PSA", "Se prostate specific Ag level"),
	)

	t.Run("exact ignores punctuation and abbreviations", func(t *testing.T) {
		_, names := record.Trend(r, "serum prostate specific antigen level")
		assert.Check(t, cmp.DeepEqual(names, []string{"Se prostate specific Ag level"}))
	})

	t.Run("contains wins over common names", func(t *testing.T) {
		_, names := record.Trend(r, "vitamin d")
		assert.Check(t, cmp.DeepEqual(names, []string{"Serum 25-OH vitamin D3 level", "Serum vitamin D2 level"}))
	})

	t.Run("common names include vitamin D2 and D3", func(t *testing.T) {
		_, names := record.Trend(r, "vit d")
		assert.Check(t, cmp.DeepEqual(names, []string{"Serum 25-OH vitamin D3 level", "Serum vitamin D2 level"}))
	})

	t.Run("common names", func(t *testing.T) {
		points, names := record.Trend(r, "blood pressure")
		assert.Check(t, cmp.DeepEqual(names, []string{"Diastolic arterial pressure", "Systolic arterial pressure"}))
		assert.Check(t, cmp.Len(points, 2))
	})
}

func recordOf(results ...nhsapp.TestResult) nhsapp.Record {
	return nhsapp.Record{TestResults: nhsapp.TestResultsSection{Data: results}}
}

func result(id, term string, values ...string) nhsapp.TestResult {
	tr := nhsapp.TestResult{ID: id, Term: term}
	for _, v := range values {
		tr.Values = append(tr.Values, nhsapp.TestResultValue{Term: v})
	}
	return tr
}

func ptr(f float64) *float64 {
	return &f
}
