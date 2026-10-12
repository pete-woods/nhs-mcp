package mcp

import (
	"context"
	"slices"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
)

func (a *api) addRecordTools(s *sdk.Server) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_medications",
		Description: `Get the user's medications from their GP record: current repeat prescriptions, acute (one-off) prescriptions, and, with include_discontinued, stopped repeats. Each is the GP's free text: drug, dosage, quantity.`,
		Annotations: &sdk.ToolAnnotations{Title: "Get medications", ReadOnlyHint: true},
	}, a.getMedications)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_allergies",
		Description: `Get the allergies and adverse reactions in the user's GP record.`,
		Annotations: &sdk.ToolAnnotations{Title: "Get allergies", ReadOnlyHint: true},
	}, a.getAllergies)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_problems",
		Description: `Get the coded problems (conditions and diagnoses) in the user's GP record, newest first, with details such as status and significance. active_only keeps those marked active.`,
		Annotations: &sdk.ToolAnnotations{Title: "Get problems", ReadOnlyHint: true},
	}, a.getProblems)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_immunisations",
		Description: `Get the immunisations in the user's GP record, newest first.`,
		Annotations: &sdk.ToolAnnotations{Title: "Get immunisations", ReadOnlyHint: true},
	}, a.getImmunisations)

	sdk.AddTool(s, &sdk.Tool{
		Name:        "get_demographics",
		Description: `Get the patient's details as their GP has them: name, date of birth, sex, address and NHS number. Use it to confirm whose record this is.`,
		Annotations: &sdk.ToolAnnotations{Title: "Get demographics", ReadOnlyHint: true},
	}, a.getDemographics)
}

type medication struct {
	Date  string   `json:"date,omitempty"`
	Lines []string `json:"lines"`
}

type getMedicationsInput struct {
	IncludeDiscontinued bool `json:"include_discontinued,omitempty" jsonschema:"Also return repeat prescriptions that have been stopped."`
}

type getMedicationsOutput struct {
	CurrentRepeat []medication `json:"current_repeat"`
	Acute         []medication `json:"acute"`
	Discontinued  []medication `json:"discontinued,omitempty"`
	Warning       string       `json:"warning,omitempty"`
}

func (a *api) getMedications(ctx context.Context, _ *sdk.CallToolRequest, in getMedicationsInput) (*sdk.CallToolResult, *getMedicationsOutput, error) {
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	m := r.Medications.Data
	out := &getMedicationsOutput{
		CurrentRepeat: toMedications(m.CurrentRepeat),
		Acute:         toMedications(m.Acute),
		Warning:       sectionWarning("medications", r.Medications.SectionStatus),
	}
	if in.IncludeDiscontinued {
		out.Discontinued = toMedications(m.DiscontinuedRepeat)
	}
	return nil, out, nil
}

type allergy struct {
	Date     string `json:"date,omitempty"`
	Name     string `json:"name"`
	Drug     string `json:"drug,omitempty"`
	Reaction string `json:"reaction,omitempty"`
}

type getAllergiesOutput struct {
	Allergies []allergy `json:"allergies"`
	Warning   string    `json:"warning,omitempty"`
}

func (a *api) getAllergies(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, *getAllergiesOutput, error) {
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	out := &getAllergiesOutput{Allergies: []allergy{}, Warning: sectionWarning("allergies", r.Allergies.SectionStatus)}
	for _, al := range r.Allergies.Data {
		out.Allergies = append(out.Allergies, allergy{Date: al.Date.String(), Name: al.Name, Drug: al.Drug, Reaction: al.Reaction})
	}
	return nil, out, nil
}

type getProblemsInput struct {
	ActiveOnly bool `json:"active_only,omitempty" jsonschema:"Only problems marked active."`
}

type problem struct {
	Date    string   `json:"date,omitempty"`
	Name    string   `json:"name"`
	Details []string `json:"details,omitempty" jsonschema:"E.g. \"Status: Active\", \"Significance: Significant\"."`
}

type getProblemsOutput struct {
	Problems []problem `json:"problems"`
	Warning  string    `json:"warning,omitempty"`
}

func (a *api) getProblems(ctx context.Context, _ *sdk.CallToolRequest, in getProblemsInput) (*sdk.CallToolResult, *getProblemsOutput, error) {
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	problems := slices.Clone(r.Problems.Data)
	slices.SortStableFunc(problems, func(a, b nhsapp.Problem) int {
		return b.EffectiveDate.Time().Compare(a.EffectiveDate.Time())
	})
	out := &getProblemsOutput{Problems: []problem{}, Warning: sectionWarning("problems", r.Problems.SectionStatus)}
	for _, p := range problems {
		lines := flatten(p.LineItems)
		if len(lines) == 0 {
			continue
		}
		if in.ActiveOnly && !slices.ContainsFunc(lines, isActive) {
			continue
		}
		out.Problems = append(out.Problems, problem{Date: p.EffectiveDate.String(), Name: lines[0], Details: lines[1:]})
	}
	return nil, out, nil
}

type immunisation struct {
	Date string `json:"date,omitempty"`
	Name string `json:"name"`
}

type getImmunisationsOutput struct {
	Immunisations []immunisation `json:"immunisations"`
	Warning       string         `json:"warning,omitempty"`
}

func (a *api) getImmunisations(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, *getImmunisationsOutput, error) {
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	imms := slices.Clone(r.Immunisations.Data)
	slices.SortStableFunc(imms, func(a, b nhsapp.Immunisation) int {
		return b.EffectiveDate.Time().Compare(a.EffectiveDate.Time())
	})
	out := &getImmunisationsOutput{Immunisations: []immunisation{}, Warning: sectionWarning("immunisations", r.Immunisations.SectionStatus)}
	for _, im := range imms {
		out.Immunisations = append(out.Immunisations, immunisation{Date: im.EffectiveDate.String(), Name: im.Term})
	}
	return nil, out, nil
}

type demographicsOutput struct {
	Name        string `json:"name"`
	DateOfBirth string `json:"date_of_birth"`
	Sex         string `json:"sex,omitempty"`
	Address     string `json:"address,omitempty"`
	NHSNumber   string `json:"nhs_number"`
}

func (a *api) getDemographics(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, *demographicsOutput, error) {
	d, err := a.sessions.Demographics(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}
	dob := nhsapp.Date{Value: d.DateOfBirth + "Z", DatePart: "YearMonthDay"}.String()
	return nil, &demographicsOutput{
		Name:        d.PatientName,
		DateOfBirth: dob,
		Sex:         d.Sex,
		Address:     d.Address,
		NHSNumber:   d.NHSNumber,
	}, nil
}

func toMedications(meds []nhsapp.Medication) []medication {
	out := make([]medication, 0, len(meds))
	for _, m := range meds {
		date := nhsapp.Date{Value: m.Date, DatePart: "YearMonthDay"}.String()
		out = append(out, medication{Date: date, Lines: flatten(m.LineItems)})
	}
	return out
}

// flatten turns nested line items into lines of text.
func flatten(items []nhsapp.LineItem) []string {
	var lines []string
	for _, it := range items {
		if t := strings.TrimSpace(it.Text); t != "" {
			lines = append(lines, t)
		}
		lines = append(lines, flatten(it.LineItems)...)
	}
	return lines
}

func isActive(line string) bool {
	return strings.EqualFold(line, "Status: Active")
}
