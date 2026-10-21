package main

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
)

const maxMarkdown = 24000

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))
var invitationPolicy = bluemonday.UGCPolicy()

func renderMarkdown(source string) template.HTML {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(source), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(source))
	}
	// This is the only conversion to trusted HTML. Model output and host edits
	// both go through the same Markdown renderer and sanitizer.
	return template.HTML(invitationPolicy.SanitizeBytes(buf.Bytes()))
}

func plainMarkdown(source string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(string(renderMarkdown(source))))
	var out strings.Builder
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return strings.Join(strings.Fields(out.String()), " ")
		case html.TextToken:
			out.Write(tokenizer.Text())
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			switch string(name) {
			case "p", "br", "li", "h1", "h2", "h3", "blockquote":
				out.WriteByte(' ')
			}
		}
	}
}

func markdownTitle(source string) string {
	data := []byte(source)
	doc := markdown.Parser().Parse(text.NewReader(data))
	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		if heading, ok := node.(*ast.Heading); ok {
			return plainMarkdown(string(heading.Text(data)))
		}
	}
	return (Event{Description: plainMarkdown(source)}).Title()
}

func directionsURL(location string) string {
	if strings.TrimSpace(location) == "" {
		return ""
	}
	return "https://www.google.com/maps/dir/?" + url.Values{
		"api": {"1"}, "destination": {location},
	}.Encode()
}

func withDirections(source, location string) string {
	if link := directionsURL(location); link != "" {
		return strings.TrimSpace(source) + "\n\n[Google Maps directions](" + link + ")"
	}
	return source
}

func (e Event) InvitationHTML() template.HTML { return renderMarkdown(e.Markdown) }

func (e Event) PlainDescription() string {
	if e.Markdown != "" {
		return plainMarkdown(e.Markdown)
	}
	return e.Description
}

func (e Event) ExportMarkdown(baseURL string) string {
	source := e.Markdown
	if source == "" {
		// Escape legacy plain text so raw HTML cannot become active on export.
		source = template.HTMLEscapeString(e.Description)
	}
	if len(e.Image) != 0 {
		source = fmt.Sprintf("![Event illustration](%s/event/%d/image)\n\n%s", baseURL, e.ID, source)
	}
	return strings.TrimSpace(source) + fmt.Sprintf("\n\n**When:** %s (%s)\n\n[RSVP](%s/event/%d)\n", e.PrettyStart(), e.TimeZone, baseURL, e.ID)
}
