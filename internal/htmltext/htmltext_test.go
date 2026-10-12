package htmltext_test

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/pete-woods/nhs-mcp/internal/htmltext"
)

func TestText(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "blocks become lines",
			html: `<html><head><title>x</title><style>p{}</style></head><body><h1>Letter</h1><p>Dear  patient,</p><p>Line one<br>line two</p></body></html>`,
			want: "Letter\nDear patient,\nLine one\nline two",
		},
		{
			name: "tables",
			html: `<table><tr><th>Test</th><th>Value</th></tr><tr><td>Sodium</td><td>140 mmol/L</td></tr></table>`,
			want: "Test | Value\nSodium | 140 mmol/L",
		},
		{
			name: "scripts dropped, entities decoded",
			html: `<div>A &amp; B<script>alert(1)</script></div>`,
			want: "A & B",
		},
		{
			name: "plain text",
			html: "just text",
			want: "just text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Check(t, cmp.Equal(htmltext.Text(tt.html), tt.want))
		})
	}
}
