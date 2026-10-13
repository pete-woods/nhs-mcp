package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pete-woods/nhs-mcp/internal/client/nhsapp"
	"github.com/pete-woods/nhs-mcp/internal/htmltext"
	"github.com/pete-woods/nhs-mcp/internal/session"
)

const (
	defaultConsultationsLimit = 20
	maxConsultationsLimit     = 200
	defaultDocumentsLimit     = 50
	maxDocumentsLimit         = 500
	defaultPages              = 5
	maxPages                  = 20
	// maxImageBytes caps the page images in one get_document result.
	maxImageBytes = 8 << 20
)

func (a *api) addDocumentTools(s *sdk.Server) {
	addTool(s, &sdk.Tool{
		Name: "list_consultations",
		Description: `List the user's consultations with their GP practice, newest first: the date, who they saw, and what was recorded under each heading (History, Examination, Problem, Medication, ...).
Filter by since/until (YYYY-MM-DD) and contains (text anywhere in the consultation, any case). limit defaults to 20, at most 200; truncated says there were more.`,
		Annotations: &sdk.ToolAnnotations{Title: "List consultations", ReadOnlyHint: true},
	}, a.listConsultations)

	addTool(s, &sdk.Tool{
		Name: "list_documents",
		Description: `List the letters, scans and attachments in the user's GP record, newest first: id, date, description, file type and size, and whether it can be opened.
Filter by since/until (YYYY-MM-DD) and contains (part of the description or file name, any case). limit defaults to 50, at most 500. Pass an id to get_document to read one.`,
		Annotations: &sdk.ToolAnnotations{Title: "List documents", ReadOnlyHint: true},
	}, a.listDocuments)

	addTool(s, &sdk.Tool{
		Name: "get_document",
		Description: `Read a document from list_documents by id. Letters come back as a text resource; scans and PDFs come back as page images, which you can read.
max_pages limits the images, default 5, at most 20; pages says how many images the NHS App shows it as, which can differ from the file's own pages, or for a letter, images says how many pictures (like logos) its text has. When a document without text (e.g. a PDF) can be downloaded, its original file is linked as a resource, file_uri, to read only if the text and images aren't enough.`,
		Annotations: &sdk.ToolAnnotations{Title: "Get document", ReadOnlyHint: true},
	}, a.getDocument)

	s.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "document_file",
		Title:       "Document file",
		URITemplate: documentURIPrefix + "{id}" + documentFileSuffix,
		Description: "The original file of a document in the user's GP record (PDF, RTF, TIFF, ...), as get_document links it.",
	}, a.readDocumentFile)
}

type sinceUntil struct {
	Since    string `json:"since,omitempty" jsonschema:"Only on or after this date, YYYY-MM-DD."`
	Until    string `json:"until,omitempty" jsonschema:"Only on or before this date, YYYY-MM-DD."`
	Contains string `json:"contains,omitempty" jsonschema:"Only those containing this text, in any case."`
	Limit    int    `json:"limit,omitempty"`
}

type consultationSection struct {
	Heading string   `json:"heading"`
	Lines   []string `json:"lines" jsonschema:"Each coded entry, followed by its notes after \" - \"."`
}

type consultation struct {
	Date      string                `json:"date"`
	Clinician string                `json:"clinician,omitempty"`
	Location  string                `json:"location,omitempty"`
	Sections  []consultationSection `json:"sections"`
}

type listConsultationsOutput struct {
	Consultations []consultation `json:"consultations"`
	Matched       int            `json:"matched"`
	Truncated     bool           `json:"truncated"`
	Warning       string         `json:"warning,omitempty"`
}

func (a *api) listConsultations(ctx context.Context, _ *sdk.CallToolRequest, in sinceUntil) (*sdk.CallToolResult, *listConsultationsOutput, error) {
	f, err := in.parse()
	if err != nil {
		return nil, nil, err
	}
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}

	consultations := slices.Clone(r.Consultations.Data)
	slices.SortStableFunc(consultations, func(a, b nhsapp.Consultation) int {
		return b.EffectiveDate.Time().Compare(a.EffectiveDate.Time())
	})
	var found []consultation
	for _, c := range consultations {
		out := toConsultation(c)
		if f.match(c.EffectiveDate, consultationText(out)) {
			found = append(found, out)
		}
	}
	shown, truncated := clip(found, in.Limit, defaultConsultationsLimit, maxConsultationsLimit)
	if shown == nil {
		shown = []consultation{}
	}
	return nil, &listConsultationsOutput{
		Consultations: shown,
		Matched:       len(found),
		Truncated:     truncated,
		Warning:       sectionWarning("consultations", r.Consultations.SectionStatus),
	}, nil
}

type document struct {
	ID        string `json:"id"`
	Date      string `json:"date,omitempty"`
	Name      string `json:"name" jsonschema:"What the document is, e.g. \"Letter from specialist\"."`
	FileName  string `json:"file_name,omitempty"`
	Type      string `json:"type" jsonschema:"The file type, e.g. pdf, rtf, tif."`
	Size      int    `json:"size" jsonschema:"In bytes."`
	Available bool   `json:"available" jsonschema:"Whether get_document can open it."`
}

type listDocumentsOutput struct {
	Documents []document `json:"documents"`
	Matched   int        `json:"matched"`
	Truncated bool       `json:"truncated"`
	Warning   string     `json:"warning,omitempty"`
}

func (a *api) listDocuments(ctx context.Context, _ *sdk.CallToolRequest, in sinceUntil) (*sdk.CallToolResult, *listDocumentsOutput, error) {
	f, err := in.parse()
	if err != nil {
		return nil, nil, err
	}
	r, err := a.sessions.Record(ctx)
	if err != nil {
		return nil, nil, userError(err)
	}

	docs := slices.Clone(r.Documents.Data)
	slices.SortStableFunc(docs, func(a, b nhsapp.Document) int {
		return b.EffectiveDate.Time().Compare(a.EffectiveDate.Time())
	})
	var found []document
	for _, d := range docs {
		if f.match(d.EffectiveDate, d.Term+"\n"+d.Name) {
			found = append(found, toDocument(d))
		}
	}
	shown, truncated := clip(found, in.Limit, defaultDocumentsLimit, maxDocumentsLimit)
	if shown == nil {
		shown = []document{}
	}
	return nil, &listDocumentsOutput{
		Documents: shown,
		Matched:   len(found),
		Truncated: truncated,
		Warning:   sectionWarning("documents", r.Documents.SectionStatus),
	}, nil
}

type getDocumentInput struct {
	ID       string `json:"id" jsonschema:"A document id from list_documents."`
	MaxPages int    `json:"max_pages,omitempty" jsonschema:"Maximum page images to return. Defaults to 5, at most 20."`
}

type getDocumentOutput struct {
	document
	Pages           int      `json:"pages" jsonschema:"How many page images the NHS App shows a scan or PDF as. These can differ from the file's own pages: it can split one page into two images."`
	PagesReturned   int      `json:"pages_returned"`
	Images          int      `json:"images,omitempty" jsonschema:"How many pictures, like logos, a letter's text has; they aren't pages."`
	ImagesReturned  int      `json:"images_returned,omitempty"`
	Truncated       bool     `json:"truncated" jsonschema:"Whether some pages or pictures were left out; raise max_pages for more."`
	Unshowable      int      `json:"unshowable_pages,omitempty" jsonschema:"Pages in an image format MCP can't show."`
	UnshowableTypes []string `json:"unshowable_types,omitempty"`
	FileURI         string   `json:"file_uri,omitempty" jsonschema:"The original file, to read as an MCP resource, when it can be downloaded and there is no text."`
}

func (a *api) getDocument(ctx context.Context, _ *sdk.CallToolRequest, in getDocumentInput) (*sdk.CallToolResult, *getDocumentOutput, error) {
	d, content, err := a.sessions.Document(ctx, in.ID)
	if errors.Is(err, session.ErrNoDocument) {
		return nil, nil, fmt.Errorf("no document with id %q: use an id from list_documents", in.ID)
	}
	if err != nil {
		return nil, nil, userError(err)
	}
	if !content.IsViewable && !content.IsDownloadable {
		return nil, nil, fmt.Errorf("the NHS App can't show this %s document", d.Extension)
	}

	out := &getDocumentOutput{document: toDocument(d)}
	// Never nil, so the SDK doesn't add the output as text.
	files := []sdk.Content{}
	text := htmltext.Text(content.Content)
	if text != "" {
		files = append(files, &sdk.EmbeddedResource{Resource: &sdk.ResourceContents{
			URI:      documentURI(d.DocumentIdentifier),
			MIMEType: "text/plain",
			Text:     text,
		}})
	}
	images, skipped := pageImages(content.Content)
	out.Pages = len(images) + len(skipped)
	out.Unshowable = len(skipped)
	out.UnshowableTypes = slices.Compact(slices.Sorted(slices.Values(skipped)))
	limit := in.MaxPages
	if limit <= 0 {
		limit = defaultPages
	}
	limit = min(limit, maxPages)

	budget := maxImageBytes
	for _, img := range images {
		if out.PagesReturned == limit || len(img.Data) > budget {
			out.Truncated = true
			break
		}
		budget -= len(img.Data)
		files = append(files, img)
		out.PagesReturned++
	}
	// A letter's images are pictures in its text, not pages.
	if text != "" {
		out.Images, out.ImagesReturned = out.Pages, out.PagesReturned
		out.Pages, out.PagesReturned = 0, 0
	}
	// A letter's text is already here: the NHS App downloads it as the same
	// text, less its line breaks. The size isn't given, as the NHS App's
	// sizes aren't those of what it downloads.
	if content.IsDownloadable && text == "" {
		out.FileURI = documentURI(d.DocumentIdentifier) + documentFileSuffix
		files = append(files, &sdk.ResourceLink{
			URI:   out.FileURI,
			Name:  d.Name,
			Title: d.Term,
			// Not the listed type: the NHS App can download a "tif" as a GIF.
			Description: "The original file, as the NHS App downloads it.",
		})
	}
	return &sdk.CallToolResult{Content: files}, out, nil
}

func (a *api) readDocumentFile(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	uri := req.Params.URI
	id, ok := documentFileID(uri)
	if !ok {
		return nil, sdk.ResourceNotFoundError(uri)
	}
	_, f, err := a.sessions.DownloadDocument(ctx, id)
	if errors.Is(err, session.ErrNoDocument) {
		return nil, sdk.ResourceNotFoundError(uri)
	}
	if err != nil {
		return nil, userError(err)
	}
	contents := &sdk.ResourceContents{URI: uri, MIMEType: fileType(f)}
	if strings.HasPrefix(contents.MIMEType, "text/") && utf8.Valid(f.Data) {
		contents.Text = string(f.Data)
	} else {
		contents.Blob = f.Data
	}
	return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{contents}}, nil
}

const (
	documentURIPrefix  = "nhs-app://documents/"
	documentFileSuffix = "/file"
)

// documentURI names a document in the resources get_document returns.
func documentURI(id string) string {
	return documentURIPrefix + url.PathEscape(id)
}

// documentFileID is the document id in a document file's URI.
func documentFileID(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, documentURIPrefix)
	if !ok {
		return "", false
	}
	rest, ok = strings.CutSuffix(rest, documentFileSuffix)
	if !ok {
		return "", false
	}
	id, err := url.PathUnescape(rest)
	if err != nil || id == "" {
		return "", false
	}
	return id, true
}

// fileType is a document file's MIME type. The bytes are trusted over the
// NHS App's header when they look like an image or a PDF, as it can be wrong
// (it calls TIFFs it has converted to GIF image/tiff); otherwise the header,
// which knows types that sniffing doesn't, like RTF or Word.
func fileType(f session.File) string {
	sniffed := http.DetectContentType(f.Data)
	if strings.HasPrefix(sniffed, "image/") || sniffed == "application/pdf" || f.ContentType == "" {
		return sniffed
	}
	return f.ContentType
}

// dataImage matches the inline images the NHS App renders scans and PDFs as.
var dataImage = regexp.MustCompile(`(?i)<img[^>]+src\s*=\s*["']data:([^;,"']*)(;base64)?,([^"']+)["']`)

// showable are the image types MCP clients display.
var showable = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// pageImages decodes a document's page images. Those that aren't valid, or
// that MCP clients can't show, are left out; it returns their types.
func pageImages(html string) (images []*sdk.ImageContent, skipped []string) {
	for _, m := range dataImage.FindAllStringSubmatch(html, -1) {
		declared := strings.ToLower(strings.TrimSpace(m[1]))
		if m[2] == "" {
			skipped = append(skipped, declared+" (not base64)")
			continue
		}
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(m[3]), ""))
		if err != nil {
			skipped = append(skipped, declared+" (bad base64)")
			continue
		}
		// Trust the bytes over the declared type, which can be wrong or an
		// alias like image/jpg.
		mime := http.DetectContentType(b)
		if !slices.Contains(showable, mime) {
			skipped = append(skipped, fmt.Sprintf("%s (looks like %s)", declared, mime))
			continue
		}
		images = append(images, &sdk.ImageContent{MIMEType: mime, Data: b})
	}
	return images, skipped
}

func toConsultation(c nhsapp.Consultation) consultation {
	out := consultation{
		Date:      c.EffectiveDate.String(),
		Clinician: c.DoneBy,
		Location:  c.Location,
		Sections:  []consultationSection{},
	}
	if out.Location == "" && c.ConsultantLocation != c.DoneBy {
		out.Location = c.ConsultantLocation
	}
	for _, h := range c.Headers {
		sec := consultationSection{Heading: h.Header, Lines: []string{}}
		sec.Lines = append(sec.Lines, nonEmpty(h.AssociatedTexts)...)
		for _, o := range h.Observations {
			line := strings.Join(slices.Concat([]string{strings.TrimSpace(o.Term)}, nonEmpty(o.AssociatedTexts)), " - ")
			sec.Lines = append(sec.Lines, line)
		}
		out.Sections = append(out.Sections, sec)
	}
	return out
}

func consultationText(c consultation) string {
	parts := []string{c.Clinician, c.Location}
	for _, s := range c.Sections {
		parts = append(parts, s.Heading)
		parts = append(parts, s.Lines...)
	}
	return strings.Join(parts, "\n")
}

func toDocument(d nhsapp.Document) document {
	return document{
		ID:        d.DocumentIdentifier,
		Date:      d.EffectiveDate.String(),
		Name:      d.Term,
		FileName:  d.Name,
		Type:      strings.ToLower(d.Extension),
		Size:      d.Size,
		Available: d.IsAvailable && d.IsValidFile,
	}
}

func nonEmpty(texts []string) []string {
	var out []string
	for _, t := range texts {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

type parsedFilter struct {
	since, until time.Time
	contains     string
}

func (in sinceUntil) parse() (parsedFilter, error) {
	since, err := parseDate("since", in.Since)
	if err != nil {
		return parsedFilter{}, err
	}
	until, err := parseDate("until", in.Until)
	if err != nil {
		return parsedFilter{}, err
	}
	if !until.IsZero() {
		until = until.AddDate(0, 0, 1) // until is inclusive
	}
	return parsedFilter{since: since, until: until, contains: strings.ToLower(in.Contains)}, nil
}

func (f parsedFilter) match(d nhsapp.Date, text string) bool {
	at := d.Time()
	switch {
	case !f.since.IsZero() && at.Before(f.since):
		return false
	case !f.until.IsZero() && !at.Before(f.until):
		return false
	case f.contains != "" && !strings.Contains(strings.ToLower(text), f.contains):
		return false
	}
	return true
}
