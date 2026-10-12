package record

import (
	"strings"
	"unicode"
)

// nameQuery matches test and measurement names against what the user asked
// for, ignoring case and punctuation, and knowing some common names for
// tests that GP systems record differently (e.g. "vitamin d" for
// "Serum 25-OH vitamin D3 level").
type nameQuery struct {
	text    string   // normalised, without padding
	aliases []string // normalised and padded, so they match whole words
}

func newNameQuery(name string) nameQuery {
	text := strings.TrimSpace(normalise(name))
	return nameQuery{text: text, aliases: aliases[text]}
}

// exact says whether name is what was asked for.
func (q nameQuery) exact(name string) bool {
	return strings.TrimSpace(normalise(name)) == q.text
}

// contains says whether name contains what was asked for, starting at a word,
// so that "ess" doesn't match "pressure" but "triglyceride" matches
// "triglycerides".
func (q nameQuery) contains(name string) bool {
	return strings.Contains(normalise(name), " "+q.text)
}

// alias says whether name contains, as whole words, a common name for what was
// asked for.
func (q nameQuery) alias(name string) bool {
	n := normalise(name)
	for _, a := range q.aliases {
		if strings.Contains(n, a) {
			return true
		}
	}
	return false
}

func (q nameQuery) matches(name string) bool {
	return q.contains(name) || q.alias(name)
}

// aliases maps a normalised query to the normalised names it also matches.
var aliases = buildAliases([]aliasGroup{
	{
		queries: []string{"vitamin d", "vit d", "25 oh d", "25 oh vitamin d", "25 hydroxyvitamin d"},
		names:   []string{"vitamin d", "vitamin d2", "vitamin d3", "vit d", "hydroxyvitamin d", "hydroxy vitamin d", "25 oh", "cholecalciferol", "ergocalciferol", "calciferol", "calcidiol"},
	},
	{
		queries: []string{"b12", "vitamin b12", "vit b12", "cobalamin"},
		names:   []string{"b12", "cobalamin", "cyanocobalamin"},
	},
	{queries: []string{"folate", "folic acid"}, names: []string{"folate", "folic"}},
	{queries: []string{"iron", "iron studies"}, names: []string{"iron", "ferritin", "transferrin"}},
	{
		queries: []string{"thyroid", "thyroid function", "tft", "tfts"},
		names:   []string{"thyroid", "tsh", "thyroxine", "free t4", "free t3", "triiodothyronine"},
	},
	{queries: []string{"tsh", "thyroid stimulating hormone"}, names: []string{"tsh", "thyroid stimulating hormone"}},
	{queries: []string{"t4", "free t4", "ft4", "thyroxine"}, names: []string{"t4", "thyroxine"}},
	{queries: []string{"t3", "free t3", "ft3", "triiodothyronine"}, names: []string{"t3", "triiodothyronine"}},
	{
		queries: []string{"cholesterol", "lipid", "lipids", "lipid profile", "blood fats"},
		names:   []string{"cholesterol", "lipid", "hdl", "ldl", "triglyceride", "triglycerides"},
	},
	{queries: []string{"ldl", "bad cholesterol"}, names: []string{"ldl", "low density lipoprotein"}},
	{queries: []string{"hdl", "good cholesterol"}, names: []string{"hdl", "high density lipoprotein"}},
	{queries: []string{"triglyceride", "triglycerides"}, names: []string{"triglyceride", "triglycerides"}},
	{
		queries: []string{"hba1c", "a1c", "glycated haemoglobin", "glycated hemoglobin", "diabetes", "blood sugar", "sugar", "glucose"},
		names:   []string{"hba1c", "haemoglobin a1c", "hemoglobin a1c", "glycated", "glycosylated", "glucose"},
	},
	{
		queries: []string{"kidney", "kidney function", "renal", "renal function", "u e", "u es", "ue", "ues"},
		names:   []string{"renal", "kidney", "egfr", "egfrcreat", "glomerular filtration", "creatinine", "urea", "electrolytes"},
	},
	{queries: []string{"egfr", "gfr"}, names: []string{"egfr", "egfrcreat", "glomerular filtration"}},
	{
		queries: []string{"liver", "liver function", "lft", "lfts"},
		names:   []string{"liver", "alt", "alanine", "ast", "aspartate", "alkaline phosphatase", "alp", "bilirubin", "albumin", "ggt", "gamma gt", "gamma glutamyl"},
	},
	{queries: []string{"alt", "alanine aminotransferase", "alanine transaminase"}, names: []string{"alt", "alanine"}},
	{queries: []string{"ast", "aspartate aminotransferase", "aspartate transaminase"}, names: []string{"ast", "aspartate"}},
	{queries: []string{"alp", "alk phos"}, names: []string{"alp", "alkaline phosphatase"}},
	{queries: []string{"ggt", "gamma gt"}, names: []string{"ggt", "gamma gt", "gamma glutamyl"}},
	{queries: []string{"fbc", "full blood count", "blood count", "cbc"}, names: []string{"fbc", "full blood count", "blood count"}},
	{queries: []string{"haemoglobin", "hemoglobin", "hb"}, names: []string{"haemoglobin", "hemoglobin", "hb"}},
	{queries: []string{"haematocrit", "hematocrit"}, names: []string{"haematocrit", "hematocrit"}},
	{queries: []string{"anaemia", "anemia"}, names: []string{"anaemia", "anemia"}},
	{
		queries: []string{"wbc", "white cells", "white cell count", "white blood cells", "white blood cell count"},
		names:   []string{"wbc", "white cell", "white blood cell", "leucocyte", "leukocyte"},
	},
	{queries: []string{"platelets", "platelet count", "plt"}, names: []string{"platelet", "platelets"}},
	{queries: []string{"crp", "c reactive protein"}, names: []string{"crp", "c reactive protein"}},
	{queries: []string{"esr", "erythrocyte sedimentation rate"}, names: []string{"esr", "erythrocyte sedimentation rate"}},
	{queries: []string{"psa", "prostate specific antigen", "prostate"}, names: []string{"psa", "prostate specific"}},
	{
		queries: []string{"inr", "clotting", "warfarin"},
		names:   []string{"inr", "international normalised ratio", "international normalized ratio", "prothrombin"},
	},
	{
		queries: []string{"bp", "blood pressure"},
		names:   []string{"bp", "blood pressure", "systolic", "diastolic"},
	},
	{queries: []string{"bmi", "body mass index"}, names: []string{"bmi", "body mass index"}},
	{queries: []string{"heart rate", "pulse"}, names: []string{"heart rate", "pulse"}},
	{queries: []string{"urate", "uric acid", "gout"}, names: []string{"urate", "uric acid"}},
	{queries: []string{"ck", "cpk", "creatine kinase"}, names: []string{"ck", "cpk", "creatine kinase"}},
	{
		queries: []string{"acr", "albumin creatinine ratio", "urine albumin", "microalbumin"},
		names:   []string{"acr", "albumin creatinine", "microalbumin"},
	},
	{queries: []string{"bone", "bone profile"}, names: []string{"bone profile", "calcium", "phosphate", "alkaline phosphatase"}},
	{queries: []string{"fsh", "follicle stimulating hormone"}, names: []string{"fsh", "follicle stimulating"}},
	{queries: []string{"lh", "luteinising hormone", "luteinizing hormone"}, names: []string{"lh", "luteinising", "luteinizing"}},
	{queries: []string{"oestradiol", "estradiol"}, names: []string{"oestradiol", "estradiol"}},
	{
		queries: []string{"hcg", "pregnancy"},
		names:   []string{"hcg", "chorionic gonadotrophin", "chorionic gonadotropin", "pregnancy"},
	},
	{queries: []string{"heart risk", "cardiovascular risk", "cvd risk", "qrisk"}, names: []string{"qrisk", "qrisk2", "qrisk3", "cardiovascular"}},
	{queries: []string{"sleepiness", "epworth", "ess"}, names: []string{"epworth", "ess"}},
	{queries: []string{"sleep apnoea", "sleep apnea", "stop bang"}, names: []string{"stopbang", "stop bang", "epworth"}},
	{queries: []string{"ipss", "prostate symptoms", "urinary symptoms"}, names: []string{"ipss", "prostate symptom"}},
	{queries: []string{"alcohol", "drinking"}, names: []string{"alcohol", "alcoholic", "audit"}},
	{queries: []string{"height"}, names: []string{"height"}},
	{queries: []string{"weight"}, names: []string{"weight"}},
})

// abbreviations are words GP systems shorten or misspell in names, with
// what they stand for.
var abbreviations = map[string]string{
	"se":   "serum",
	"ag":   "antigen",
	"levl": "level",
	"lvl":  "level",
	"conc": "concentration",
}

type aliasGroup struct {
	queries, names []string
}

func buildAliases(groups []aliasGroup) map[string][]string {
	m := map[string][]string{}
	for _, g := range groups {
		names := make([]string, len(g.names))
		for i, n := range g.names {
			names[i] = normalise(n)
		}
		for _, q := range g.queries {
			q = strings.TrimSpace(normalise(q))
			m[q] = append(m[q], names...)
		}
	}
	return m
}

// normalise lowercases s, turns punctuation into spaces, expands
// abbreviations, and pads it with a space at each end, so that
// "Se non-HDL/cholesterol" becomes " serum non hdl cholesterol ".
func normalise(s string) string {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, w := range words {
		if full, ok := abbreviations[w]; ok {
			words[i] = full
		}
	}
	return " " + strings.Join(words, " ") + " "
}
