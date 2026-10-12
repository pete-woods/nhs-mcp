package nhsapp

import (
	"encoding/json"
	"time"
)

// Record is the GP medical record from GET /v1/patient/my-record. It holds
// only the sections the MCP uses; the API returns more.
type Record struct {
	HasSummaryRecordAccess  bool                  `json:"hasSummaryRecordAccess"`
	HasDetailedRecordAccess bool                  `json:"hasDetailedRecordAccess"`
	Supplier                string                `json:"supplier"`
	Allergies               Section[Allergy]      `json:"allergies"`
	Medications             MedicationsSection    `json:"medications"`
	Immunisations           Section[Immunisation] `json:"immunisations"`
	TestResults             TestResultsSection    `json:"testResults"`
	HistoricTestResults     TestResultsSection    `json:"historicTestResults"`
	Problems                Section[Problem]      `json:"problems"`
	Consultations           Section[Consultation] `json:"consultations"`
	Documents               Section[Document]     `json:"documents"`
}

// SectionStatus says whether a section of the record could be read.
type SectionStatus struct {
	HasAccess             bool `json:"hasAccess"`
	HasErrored            bool `json:"hasErrored"`
	HasUndeterminedAccess bool `json:"hasUndeterminedAccess"`
	RecordCount           int  `json:"recordCount"`
}

type Section[T any] struct {
	SectionStatus
	Data []T `json:"data"`
}

type TestResultsSection struct {
	Section[TestResult]
	HasAllHistoricResults bool `json:"hasAllHistoricResults"`
}

type MedicationsSection struct {
	SectionStatus
	Data Medications `json:"data"`
}

// Date is a date recorded only as precisely as the GP system knows it.
type Date struct {
	Value string `json:"value"`
	// DatePart is the precision: Year, YearMonth, YearMonthDay or
	// YearMonthDayTime.
	DatePart string `json:"datePart"`
}

// Time parses the date, or returns the zero time when there isn't one.
func (d Date) Time() time.Time {
	t, err := time.Parse(time.RFC3339, d.Value)
	if err != nil {
		return time.Time{}
	}
	return t
}

// String formats the date to its known precision, e.g. "2019" or "2019-03-14".
func (d Date) String() string {
	t := d.Time()
	if t.IsZero() {
		return ""
	}
	switch d.DatePart {
	case "Year":
		return t.Format("2006")
	case "YearMonth":
		return t.Format("2006-01")
	case "YearMonthDayTime":
		return t.Format(time.RFC3339)
	default:
		return t.Format(time.DateOnly)
	}
}

type TestResult struct {
	ID              string            `json:"id"`
	Date            Date              `json:"date"`
	Term            string            `json:"term"`
	Description     string            `json:"description"`
	AssociatedTexts []AssociatedText  `json:"associatedTexts"`
	Values          []TestResultValue `json:"testResultValues"`
}

// TestResultValue is one measurement in a result, e.g. serum sodium in a
// U&E panel.
type TestResultValue struct {
	Term             string           `json:"term"`
	Description      string           `json:"description"`
	DisplayValue     string           `json:"displayValue"`
	NumericValue     *float64         `json:"numericValue"`
	NumericOperator  string           `json:"numericOperator"`
	Units            string           `json:"units"`
	Range            Range            `json:"range"`
	IsHeadlineResult bool             `json:"isHeadlineResult"`
	AssociatedTexts  []AssociatedText `json:"associatedTexts"`
}

// Range is a reference range. Either end may be missing.
type Range struct {
	Minimum string `json:"minimum"`
	Maximum string `json:"maximum"`
	Units   string `json:"units"`
}

// AssociatedText is a note, e.g. a clinician's comment or a lab method.
type AssociatedText struct {
	Text     string `json:"text"`
	TextType string `json:"textType"`
}

type Allergy struct {
	Name     string `json:"name"`
	Date     Date   `json:"date"`
	Drug     string `json:"drug"`
	Reaction string `json:"reaction"`
}

type Medications struct {
	Acute              []Medication `json:"acuteMedications"`
	CurrentRepeat      []Medication `json:"currentRepeatMedications"`
	DiscontinuedRepeat []Medication `json:"discontinuedRepeatMedications"`
}

// Medication is free text: the drug, dosage and quantity, one per line item.
type Medication struct {
	Date      string     `json:"date"`
	LineItems []LineItem `json:"lineItems"`
}

type LineItem struct {
	Text      string     `json:"text"`
	LineItems []LineItem `json:"lineItems"`
}

// UnmarshalJSON accepts a bare string too, which nested line items can be.
func (li *LineItem) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		*li = LineItem{}
		return json.Unmarshal(b, &li.Text)
	}
	type plain LineItem
	return json.Unmarshal(b, (*plain)(li))
}

type Immunisation struct {
	Term          string `json:"term"`
	EffectiveDate Date   `json:"effectiveDate"`
}

// Problem is a coded problem, with lines like "Status: Active".
type Problem struct {
	EffectiveDate Date       `json:"effectiveDate"`
	LineItems     []LineItem `json:"lineItems"`
}

// Consultation is an encounter with the practice: who saw the patient, and
// what they recorded under each heading (History, Examination, ...).
type Consultation struct {
	EffectiveDate      Date                 `json:"effectiveDate"`
	ConsultantLocation string               `json:"consultantLocation"`
	Location           string               `json:"location"`
	DoneBy             string               `json:"doneBy"`
	Headers            []ConsultationHeader `json:"consultationHeaders"`
}

type ConsultationHeader struct {
	Header          string        `json:"header"`
	AssociatedTexts []string      `json:"associatedTexts"`
	Observations    []Observation `json:"observationsWithTerm"`
}

type Observation struct {
	Term            string   `json:"term"`
	AssociatedTexts []string `json:"associatedTexts"`
	ObservationType string   `json:"observationType"`
	EventGUID       string   `json:"eventGuid"`
}

// Document is a letter, scan or attachment in the record. Fetch it with
// Client.Document or Client.DownloadDocument.
type Document struct {
	DocumentIdentifier  string `json:"documentIdentifier"`
	EffectiveDate       Date   `json:"effectiveDate"`
	Term                string `json:"term"`
	Name                string `json:"name"`
	Extension           string `json:"extension"`
	Size                int    `json:"size"`
	IsAvailable         bool   `json:"isAvailable"`
	IsValidFile         bool   `json:"isValidFile"`
	NeedMoreInformation bool   `json:"needMoreInformation"`
}
