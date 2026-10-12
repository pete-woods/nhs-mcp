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

func ptr(f float64) *float64 {
	return &f
}
