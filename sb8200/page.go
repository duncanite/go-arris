package sb8200

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// A table is its rows, a row its cells' text, whitespace collapsed. The
// pages are tables and nothing else: this is all the structure the
// parsers read.
type table [][]string

// tables parses a page and returns every table in document order. The
// pages are not well formed, a header row that forgets its <tr>, a
// </td> closing a <th>; the HTML5 parser repairs them the way a browser
// does, which is the shape the page's own script reads.
func tables(page string) ([]table, error) {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPage, err)
	}

	var found []table

	var walk func(node *html.Node)

	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.DataAtom.String() == "table" {
			found = append(found, rows(node))

			return
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(doc)

	return found, nil
}

// rows collects a table's rows, each as the text of its cells.
func rows(node *html.Node) table {
	var found table

	var walk func(node *html.Node)

	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.DataAtom.String() == "tr" {
				found = append(found, cells(child))

				continue
			}

			walk(child)
		}
	}

	walk(node)

	return found
}

// cells returns the text of a row's td and th elements.
func cells(row *html.Node) []string {
	var found []string

	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}

		if name := child.DataAtom.String(); name == "td" || name == "th" {
			found = append(found, strings.Join(strings.Fields(text(child)), " "))
		}
	}

	return found
}

// text concatenates a node's text, scripts left out.
func text(node *html.Node) string {
	var builder strings.Builder

	var walk func(node *html.Node)

	walk = func(node *html.Node) {
		switch {
		case node.Type == html.TextNode:
			builder.WriteString(node.Data)
		case node.Type == html.ElementNode && node.DataAtom.String() == "script":
			return
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(node)

	return builder.String()
}

// titled finds the table whose first cell starts with title, the pages'
// one label for a table.
func titled(found []table, title string) (table, error) {
	for _, candidate := range found {
		if len(candidate) > 0 && len(candidate[0]) > 0 && strings.HasPrefix(candidate[0][0], title) {
			return candidate, nil
		}
	}

	return nil, fmt.Errorf("%w: no %q table", ErrPage, title)
}

// body returns a table's rows after its header rows, ErrPage when the
// table is shorter than its header.
func body(rows table, header int, what string) (table, error) {
	if len(rows) < header {
		return nil, fmt.Errorf("%w: the %s table has %d rows", ErrPage, what, len(rows))
	}

	return rows[header:], nil
}

// The cell formats: a count, a quantity with its unit, a clock.
func count(cell string) (uint64, error) {
	value, err := strconv.ParseUint(cell, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a count", ErrPage, cell)
	}

	return value, nil
}

func number(cell string) (int, error) {
	value, err := strconv.Atoi(cell)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a number", ErrPage, cell)
	}

	return value, nil
}

// quantity reads "789000000 Hz", "1.9 dBmV", "40.5 dB": the value before
// the unit the page is expected to print there.
func quantity(cell, unit string) (float64, error) {
	raw, ok := strings.CutSuffix(cell, " "+unit)
	if !ok {
		return 0, fmt.Errorf("%w: %q is not in %s", ErrPage, cell, unit)
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a quantity", ErrPage, cell)
	}

	return value, nil
}

func hertz(cell string) (int64, error) {
	value, err := quantity(cell, "Hz")
	if err != nil {
		return 0, err
	}

	return int64(value), nil
}

// clock reads a time the page prints from its own clock, which carries
// no zone: the result is in UTC and means the modem's wall clock.
func clock(layout, cell string) (time.Time, error) {
	value, err := time.Parse(layout, cell)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q is not a time", ErrPage, cell)
	}

	return value, nil
}
