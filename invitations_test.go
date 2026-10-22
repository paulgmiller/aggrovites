package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeGenerator struct {
	textErr, imageErr     error
	textCalls, imageCalls int
	last                  Event
}

func (f *fakeGenerator) Generate(_ context.Context, event Event) (generatedInvitation, error) {
	f.textCalls++
	f.last = event
	return generatedInvitation{
		Markdown: "# Come to dinner!\n\nBring **snacks**. [Menu](https://example.com/menu?a=1&b=2)",
		Location: "123 Main St, Seattle, WA", ImagePrompt: "A playful dinner illustration, no lettering",
	}, f.textErr
}

func (f *fakeGenerator) Image(_ context.Context, _ string) ([]byte, error) {
	f.imageCalls++
	return []byte("test jpeg"), f.imageErr
}

func testApp(t *testing.T, ai invitationGenerator) (*server, http.Handler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "invitations.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	s := &server{db: db, ai: ai}
	return s, newRouter(s)
}

func request(t *testing.T, handler http.Handler, method, path string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code >= 500 {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	return w
}

func inputForm() url.Values {
	return url.Values{
		"Description": {"Dinner at 123 Main St, Seattle, WA. Menu: https://example.com/menu?a=1&b=2"},
		"Start":       {"2026-11-21T18:00"}, "TimeZone": {"America/Los_Angeles"}, "Generate": {"true"},
	}
}

func createDraft(t *testing.T, s *server, handler http.Handler, suffix string, values url.Values) InvitationDraft {
	t.Helper()
	w := request(t, handler, http.MethodPost, "/event/preview"+suffix, values)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	var draft InvitationDraft
	if err := s.db.First(&draft, "token = ?", strings.TrimPrefix(location.Path, "/draft/")).Error; err != nil {
		t.Fatal(err)
	}
	return draft
}

func TestInvitationPreviewAndPublish(t *testing.T) {
	ai := &fakeGenerator{}
	s, handler := testApp(t, ai)
	draft := createDraft(t, s, handler, "?host=nice", inputForm())
	if draft.Voice != "nice" || ai.last.Voice != "nice" || len(draft.Token) != 64 {
		t.Fatalf("wrong draft: %+v", draft)
	}
	if !strings.Contains(draft.Markdown, directionsURL(draft.Location)) {
		t.Fatal("missing encoded directions")
	}
	if ai.textCalls != 1 || ai.imageCalls != 1 {
		t.Fatal("wrong generation counts")
	}
	page := request(t, handler, "GET", "/draft/"+draft.Token+"?host=nice", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "<strong>snacks</strong>") || !strings.Contains(page.Body.String(), "/event?host=nice") {
		t.Fatalf("preview body: %s", page.Body.String())
	}
	if page.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private draft was cacheable")
	}
	if w := request(t, handler, "GET", "/draft/"+draft.Token+"/image", nil); w.Code != 200 {
		t.Fatal("draft artwork unavailable")
	}
	if strings.Contains(page.Body.String(), "<textarea") || strings.Contains(page.Body.String(), "Update preview") || !strings.Contains(page.Body.String(), "back button") {
		t.Fatal("preview should only offer review and publication")
	}
	// Publication uses the stored preview, ignoring client-supplied Markdown.
	form := url.Values{"DraftToken": {draft.Token}, "Markdown": {"# Unreviewed replacement"}, "IncludeImage": {"true"}}
	first := request(t, handler, "POST", "/event?host=nice", form)
	second := request(t, handler, "POST", "/event?host=nice", form)
	if first.Code != 303 || second.Header().Get("Location") != first.Header().Get("Location") {
		t.Fatal("publish was not idempotent")
	}
	var count int64
	s.db.Model(&Event{}).Count(&count)
	if count != 1 {
		t.Fatalf("published %d events", count)
	}
	var event Event
	if err := s.db.First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Markdown != draft.Markdown || event.Voice != "nice" || len(event.Image) == 0 {
		t.Fatalf("wrong saved event: %+v", event)
	}
	// A different hostname does not regenerate or rewrite the reviewed text.
	guest := request(t, handler, "GET", "/event/1", nil)
	if strings.Count(guest.Body.String(), "<h1>") != 1 || !strings.Contains(guest.Body.String(), "<strong>snacks</strong>") || !strings.Contains(guest.Body.String(), `/event/1/image`) {
		t.Fatalf("guest page: %s", guest.Body.String())
	}
	if w := request(t, handler, "GET", "/event/1/image", nil); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal("event artwork unavailable")
	}
	if strings.Contains(guest.Body.String(), "Download invitation") {
		t.Fatal("guest page should not expose a Markdown export")
	}
	if download := request(t, handler, "GET", "/event/1/invitation.md", nil); download.Code != 404 {
		t.Fatal("Markdown download endpoint should be removed")
	}
	if ai.textCalls != 1 || ai.imageCalls != 1 {
		t.Fatal("view or publication called AI")
	}
	if !strings.Contains(event.GoogleCalendarLink(), "location=") || strings.Contains(event.PlainDescription(), "**") {
		t.Fatal("calendar not using plain text/location")
	}
	// Existing RSVP behavior remains usable on the formatted page.
	rsvp := request(t, handler, "POST", "/rsvp?host=nice", url.Values{"Attendee": {"Pat"}, "Guests": {"2"}, "EventID": {"1"}, "Declined": {"false"}})
	if rsvp.Code != 302 || rsvp.Header().Get("Location") != "/event/1?host=nice" {
		t.Fatal("RSVP failed")
	}
}

func TestGenerationFailuresAndManualCreation(t *testing.T) {
	for _, mode := range []string{"text failure", "image failure", "unconfigured", "manual"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeGenerator{}
			var ai invitationGenerator = fake
			switch mode {
			case "text failure":
				fake.textErr = errors.New("provider unavailable")
			case "image failure":
				fake.imageErr = errors.New("provider unavailable")
			case "unconfigured":
				ai = nil
			}
			s, handler := testApp(t, ai)
			form := inputForm()
			if mode == "manual" {
				form.Set("Generate", "false")
			}
			draft := createDraft(t, s, handler, "", form)
			if draft.Description != form.Get("Description") || draft.Markdown == "" || len(draft.Image) != 0 {
				t.Fatal("input not preserved or unexpected artwork")
			}
			if mode != "manual" && draft.Notice == "" {
				t.Fatal("failure not explained")
			}
			if (mode == "text failure" || mode == "unconfigured" || mode == "manual") && draft.Markdown != draft.Description {
				t.Fatal("original text lost")
			}
			if mode == "image failure" && !strings.Contains(draft.Markdown, "# Come to dinner!") {
				t.Fatal("text lost on image failure")
			}
			published := request(t, handler, "POST", "/event", url.Values{"DraftToken": {draft.Token}})
			if published.Code != 303 {
				t.Fatal("could not publish without artwork")
			}
			if mode == "manual" && (fake.textCalls != 0 || fake.imageCalls != 0) {
				t.Fatal("manual creation called AI")
			}
		})
	}
}

func TestNewPreviewAndDraftExpiry(t *testing.T) {
	ai := &fakeGenerator{}
	s, handler := testApp(t, ai)
	draft := createDraft(t, s, handler, "?host=nice", inputForm())
	form := inputForm()
	form.Set("Description", "New description at 123 Main St, Seattle, WA")
	revised := createDraft(t, s, handler, "?host=nice", form)
	if revised.Token == draft.Token || ai.textCalls != 2 || ai.last.Description != form.Get("Description") || revised.Voice != "nice" {
		t.Fatal("resubmitting the creation form should generate a new preview")
	}
	if update := request(t, handler, "POST", "/draft/"+draft.Token, url.Values{"Markdown": {"# Changed"}}); update.Code != 404 {
		t.Fatal("draft update endpoint should be removed")
	}
	var original InvitationDraft
	if err := s.db.First(&original, "token = ?", draft.Token).Error; err != nil {
		t.Fatal(err)
	}
	if original.Description != draft.Description || original.Markdown != draft.Markdown {
		t.Fatal("creating another preview changed the original")
	}
	s.db.Model(&draft).Update("expires_at", time.Now().Add(-time.Minute))
	for _, path := range []string{"/draft/" + draft.Token, "/draft/" + draft.Token + "/image"} {
		if w := request(t, handler, "GET", path, nil); w.Code != 404 {
			t.Fatalf("expired draft accessible at %s", path)
		}
	}
	if w := request(t, handler, "POST", "/event", url.Values{"DraftToken": {draft.Token}}); w.Code != 409 {
		t.Fatal("expired draft published")
	}
	createDraft(t, s, handler, "", inputForm())
	var count int64
	s.db.Model(&InvitationDraft{}).Where("token = ?", draft.Token).Count(&count)
	if count != 0 {
		t.Fatal("expired draft not cleaned up")
	}
}

func TestLegacyEventsAndFormValidation(t *testing.T) {
	s, handler := testApp(t, nil)
	for _, path := range []string{"/", "/?host=nice"} {
		page := request(t, handler, "GET", path, nil)
		if page.Code != 200 || !strings.Contains(page.Body.String(), "Preview invitation") || strings.Contains(page.Body.String(), "Generate invitation &amp; preview") {
			t.Fatalf("manual creation page: %s", page.Body.String())
		}
		if strings.Contains(path, "nice") && !strings.Contains(page.Body.String(), "/event/preview?host=nice") {
			t.Fatal("Nicevites override lost on creation form")
		}
	}
	form := inputForm()
	form.Set("Markdown", "injected")
	form.Set("Voice", "nice")
	form.Set("ID", "123")
	w := request(t, handler, "POST", "/event", form)
	if w.Code != 302 {
		t.Fatal("legacy creation failed")
	}
	var event Event
	s.db.First(&event)
	if event.Markdown != "" || event.Voice != "" || event.ID == 123 {
		t.Fatal("legacy form bound server-controlled fields")
	}
	if w := request(t, handler, "GET", "/event/1", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Dinner at 123 Main St") {
		t.Fatal("legacy display failed")
	}
	for _, path := range []string{"/event/999/image", "/event/999/invitation.md", "/event/1/image", "/draft/missing"} {
		if w := request(t, handler, "GET", path, nil); w.Code != 404 {
			t.Fatalf("expected 404 at %s, got %d", path, w.Code)
		}
	}
	for _, invalid := range []string{"", "Invalid/Timezone"} {
		form.Set("TimeZone", invalid)
		w := request(t, handler, "POST", "/event/preview", form)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "Dinner at 123 Main St") {
			t.Fatal("invalid input not retained")
		}
	}
	form.Set("TimeZone", "America/Los_Angeles")
	form.Set("Description", strings.Repeat("x", 12001))
	if w := request(t, handler, "POST", "/event/preview", form); w.Code != 400 {
		t.Fatal("oversized input accepted")
	}
}

func TestOmitArtwork(t *testing.T) {
	ai := &fakeGenerator{}
	s, handler := testApp(t, ai)
	draft := createDraft(t, s, handler, "", inputForm())
	page := request(t, handler, "GET", "/", nil)
	if !strings.Contains(page.Body.String(), "Generate invitation &amp; preview") {
		t.Fatal("AI creation button missing")
	}
	preview := request(t, handler, "GET", "/draft/"+draft.Token, nil)
	if !strings.Contains(preview.Body.String(), `name="IncludeImage"`) {
		t.Fatal("preview should offer publication without artwork")
	}
	published := request(t, handler, "POST", "/event", url.Values{"DraftToken": {draft.Token}})
	if published.Code != 303 {
		t.Fatal("could not publish without artwork")
	}
	if image := request(t, handler, "GET", "/event/1/image", nil); image.Code != 404 {
		t.Fatal("excluded artwork published")
	}
}

func TestMarkdownSafetyAndDirections(t *testing.T) {
	source := "# **Dinner**\n\nVisit https://example.com/menu?a=1&b=2\n\n- Bring snacks\n\n[bad](javascript:alert(1))\n\n<script>alert('x')</script>\n\n<img src=x onerror=alert(2)>"
	rendered := string(renderMarkdown(source))
	for _, bad := range []string{"<script", "onerror=", "javascript:", "<img src=x"} {
		if strings.Contains(rendered, bad) {
			t.Fatalf("unsafe HTML: %s", rendered)
		}
	}
	for _, good := range []string{"<h1>", "<strong>Dinner</strong>", `<a href="https://example.com/menu?a=1&amp;b=2"`, "<li>Bring snacks</li>"} {
		if !strings.Contains(rendered, good) {
			t.Fatalf("formatting missing %q: %s", good, rendered)
		}
	}
	if markdownTitle(source) != "Dinner" || strings.Contains(plainMarkdown(source), "<") {
		t.Fatal("metadata not plain text")
	}
	location := "Café & Bar, 1 Main St #2, Montréal"
	link, err := url.Parse(directionsURL(location))
	if err != nil || link.Query().Get("destination") != location || link.Query().Get("api") != "1" || link.Query().Has("origin") {
		t.Fatalf("invalid directions URL: %s", link)
	}
	if withDirections("# Dinner", "") != "# Dinner" {
		t.Fatal("invented directions for missing location")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenAIRequestsAndImageResize(t *testing.T) {
	var requests []map[string]any
	var paths []string
	var source bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	img.Set(0, 0, color.White)
	if err := jpeg.Encode(&source, img, nil); err != nil {
		t.Fatal(err)
	}
	ai := &openAI{textModel: "gpt-6-luna", imageModel: "gpt-image-2.5-flare"}
	ai.client = openai.NewClient(option.WithAPIKey("test-key"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer test-key" || req.Method != "POST" {
			t.Fatal("wrong request authentication/method")
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, body)
		paths = append(paths, req.URL.Path)
		var response any
		if strings.HasSuffix(req.URL.Path, "/responses") {
			data, _ := json.Marshal(generatedInvitation{Markdown: "# Dinner\n\n[Menu](https://example.com)", Location: "123 Main St", ImagePrompt: "Dinner illustration"})
			response = map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]string{"type": "output_text", "text": string(data)}}}}}
		} else {
			response = map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(source.Bytes())}}}
		}
		data, _ := json.Marshal(response)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: http.Header{"Content-Type": {"application/json"}}}, nil
	})}))
	for _, voice := range []string{"nice", "aggro"} {
		input := inputForm()
		start, _ := time.Parse("2006-01-02T15:04", input.Get("Start"))
		result, err := ai.Generate(context.Background(), Event{Description: input.Get("Description"), Start: start, TimeZone: input.Get("TimeZone"), Voice: voice})
		if err != nil || result.Location != "123 Main St" {
			t.Fatalf("generate: %+v %v", result, err)
		}
		body := requests[len(requests)-1]
		instructions := body["instructions"].(string)
		want := "comically profane"
		if voice == "nice" {
			want = "extremely polite"
		}
		for _, phrase := range []string{want, "Markdown, not HTML", "Preserve all factual details", "HTTP/HTTPS URL", "Do not invent"} {
			if !strings.Contains(instructions, phrase) {
				t.Fatalf("missing prompt instruction %q", phrase)
			}
		}
		if body["model"] != "gpt-6-luna" || body["store"] != false || !strings.Contains(body["input"].(string), "America/Los_Angeles") {
			t.Fatal("incorrect text request")
		}
		format := body["text"].(map[string]any)["format"].(map[string]any)
		schema := format["schema"].(map[string]any)
		if format["type"] != "json_schema" || format["strict"] != true || schema["additionalProperties"] != false || len(schema["required"].([]any)) != 3 {
			t.Fatal("invitation schema must strictly require all three output fields")
		}
		properties := schema["properties"].(map[string]any)
		for _, field := range []string{"markdown", "location", "image_prompt"} {
			if property, ok := properties[field].(map[string]any); !ok || property["type"] != "string" {
				t.Fatalf("incorrect invitation schema for %s", field)
			}
		}
	}
	imageData, err := ai.Image(context.Background(), "Dinner illustration")
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil || format != "jpeg" || config.Width != 512 || config.Height != 512 {
		t.Fatal("artwork not resized to 512px JPEG")
	}
	body := requests[len(requests)-1]
	if paths[len(paths)-1] != "/v1/images/generations" || body["quality"] != "low" || body["model"] != "gpt-image-2.5-flare" {
		t.Fatal("incorrect image request")
	}
	if _, err := smallJPEG([]byte("not an image")); err == nil {
		t.Fatal("invalid image accepted")
	}
}

func TestOpenAIRejectsFailedOrIncompleteResponses(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		status int
		body   string
	}{
		{"rate limited", 429, `{"error":{"message":"private input"}}`},
		{"incomplete", 200, `{"status":"incomplete","output":[]}`},
		{"refused", 200, `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal"}]}]}`},
		{"empty output", 200, `{"status":"completed","output":[]}`},
		{"malformed", 200, `{`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			a := &openAI{client: openai.NewClient(option.WithAPIKey("test-key"), option.WithMaxRetries(0), option.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: fixture.status, Body: io.NopCloser(strings.NewReader(fixture.body)), Header: http.Header{"Content-Type": {"application/json"}}}, nil
			})}))}
			_, err := a.Generate(context.Background(), Event{})
			if err == nil || strings.Contains(err.Error(), "private input") {
				t.Fatalf("unsafe/missing error: %v", err)
			}
		})
	}
}
