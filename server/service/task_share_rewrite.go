package service

import (
	"bytes"
	stdhtml "html"
	"net/url"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"
)

func rewriteShareHTML(source []byte, resolve func(string) (string, error)) (string, error) {
	document, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return "", ErrTaskShareUnavailable
	}
	allowed := map[string]bool{}
	for _, tag := range strings.Fields("html head body p div section span article main header footer h1 h2 h3 h4 h5 h6 blockquote pre code ul ol li strong b em i u s del br hr img a table thead tbody tfoot tr th td figure figcaption sup sub") {
		allowed[tag] = true
	}
	var clean func(*html.Node) error
	clean = func(node *html.Node) error {
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == html.CommentNode || (child.Type == html.ElementNode && !allowed[child.Data]) {
				node.RemoveChild(child)
			} else if err := clean(child); err != nil {
				return err
			}
			child = next
		}
		if node.Type != html.ElementNode {
			return nil
		}
		var attrs []html.Attribute
		imageSource := ""
		for _, attr := range node.Attr {
			if attr.Namespace != "" {
				continue
			}
			switch attr.Key {
			case "src", "data-src":
				if node.Data != "img" {
					continue
				}
				mapped, err := resolve(attr.Val)
				if err != nil {
					return err
				}
				if imageSource != "" && imageSource != mapped {
					return ErrTaskShareUnavailable
				}
				imageSource = mapped
			case "style":
				if !hasUnsafeInlineCSS(strings.ToLower(attr.Val)) {
					attrs = append(attrs, attr)
				}
			case "alt", "title", "width", "height", "colspan", "rowspan", "align":
				attrs = append(attrs, attr)
			case "href":
				if node.Data != "a" {
					continue
				}
				u, err := url.Parse(strings.TrimSpace(attr.Val))
				if err == nil && (u.Scheme == "https" || u.Scheme == "http" || (u.Scheme == "" && strings.HasPrefix(attr.Val, "#"))) {
					attrs = append(attrs, attr)
				}
			}
		}
		if node.Data == "img" {
			if imageSource == "" {
				return ErrTaskShareUnavailable
			}
			attrs = append(attrs, html.Attribute{Key: "src", Val: imageSource})
		}
		node.Attr = attrs
		return nil
	}
	if err := clean(document); err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := html.Render(&output, document); err != nil {
		return "", ErrTaskShareUnavailable
	}
	return output.String(), nil
}

type shareSourceEdit struct {
	start, end int
	value      string
}

func rewriteShareMarkdown(source []byte, resolve func(string) (string, error)) (string, error) {
	document := goldmark.New().Parser().Parse(text.NewReader(source))
	edits := map[int]shareSourceEdit{}
	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.Image:
			destination := node.Destination
			mapped, err := resolve(stdhtml.UnescapeString(string(util.UnescapePunctuations(destination))))
			if err != nil {
				return ast.WalkStop, err
			}
			// Goldmark destinations retain their source slices, including references.
			// Match slice identity so identical prose and code samples stay untouched.
			found := false
			for offset := 0; offset < len(source); {
				i := bytes.Index(source[offset:], destination)
				if i < 0 {
					break
				}
				start := offset + i
				if len(destination) > 0 && &source[start] == &destination[0] {
					edits[start] = shareSourceEdit{start: start, end: start + len(destination), value: mapped}
					found = true
					break
				}
				offset = start + 1
			}
			if !found {
				return ast.WalkStop, ErrTaskShareUnavailable
			}
		case *ast.RawHTML:
			for i := 0; i < node.Segments.Len(); i++ {
				segment := node.Segments.At(i)
				edits[segment.Start] = shareSourceEdit{start: segment.Start, end: segment.Stop}
			}
		case *ast.HTMLBlock:
			for i := 0; i < node.Lines().Len(); i++ {
				segment := node.Lines().At(i)
				edits[segment.Start] = shareSourceEdit{start: segment.Start, end: segment.Stop}
			}
			if node.HasClosure() {
				segment := node.ClosureLine
				edits[segment.Start] = shareSourceEdit{start: segment.Start, end: segment.Stop}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", err
	}
	ordered := make([]shareSourceEdit, 0, len(edits))
	for _, edit := range edits {
		ordered = append(ordered, edit)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].start < ordered[j].start })
	var output strings.Builder
	position := 0
	for _, edit := range ordered {
		if edit.start < position {
			return "", ErrTaskShareUnavailable
		}
		output.Write(source[position:edit.start])
		output.WriteString(edit.value)
		position = edit.end
	}
	output.Write(source[position:])
	return output.String(), nil
}
