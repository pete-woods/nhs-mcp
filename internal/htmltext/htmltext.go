// Package htmltext turns the HTML the NHS App sends for documents and test
// result details into plain text an agent can read.
package htmltext

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Text extracts the readable text: blocks become lines, a table row's cells
// are separated by " | ", empty cells and rows are dropped, and scripts and
// styles are dropped.
func Text(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return s
	}
	var b strings.Builder
	walk(&b, doc)
	return tidy(b.String())
}

func walk(b *strings.Builder, n *html.Node) {
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
		return
	}
	element := n.Type == html.ElementNode
	if element {
		if skip[n.DataAtom] {
			return
		}
		if n.DataAtom == atom.Br {
			b.WriteString("\n")
			return
		}
		if n.DataAtom == atom.Tr {
			row(b, n)
			return
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(b, c)
	}
	if element && block[n.DataAtom] {
		b.WriteString("\n")
	}
}

// row writes a table row's non-empty cells on one line, or, when a cell
// spans lines (e.g. it holds a table), each cell on its own lines.
func row(b *strings.Builder, tr *html.Node) {
	var cells []string
	oneLine := true
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || !cell[c.DataAtom] {
			continue
		}
		var cb strings.Builder
		walk(&cb, c)
		if text := tidy(cb.String()); text != "" {
			cells = append(cells, text)
			oneLine = oneLine && !strings.Contains(text, "\n")
		}
	}
	sep := " | "
	if !oneLine {
		sep = "\n"
	}
	if len(cells) > 0 {
		b.WriteString(strings.Join(cells, sep) + "\n")
	}
}

var (
	skip  = set(atom.Script, atom.Style, atom.Head)
	cell  = set(atom.Td, atom.Th)
	block = set(atom.P, atom.Div, atom.Tr, atom.Li, atom.Table, atom.H1, atom.H2, atom.H3,
		atom.H4, atom.H5, atom.H6, atom.Section, atom.Article, atom.Header, atom.Footer,
		atom.Ul, atom.Ol, atom.Dl, atom.Dt, atom.Dd, atom.Pre, atom.Blockquote)
)

func set(atoms ...atom.Atom) map[atom.Atom]bool {
	m := make(map[atom.Atom]bool, len(atoms))
	for _, a := range atoms {
		m[a] = true
	}
	return m
}

var (
	spaces     = regexp.MustCompile(`[ \t\x{00a0}]+`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// tidy collapses runs of spaces and blank lines.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaces.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
