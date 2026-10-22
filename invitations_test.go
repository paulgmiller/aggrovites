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
	return w
}

func inputForm() url.Values {
	return url.Values{
		"Description": {"Dinner at 123 Main St, Seattle, WA. Menu: https://example.com/menu?a=1&b=2"},
		"Start":       {"2026-11-21T18:00"}, "TimeZone": {"America/Los_Angeles"}, "Generate": {"true"},
	}
}

func createEvent(t *testing.T, s *server, handler http.Handler, suffix string, values url.Values) Event {
	t.Helper()
	w := request(t, handler, http.MethodPost, "/event"+suffix, values)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := s.db.First(&event, "id = ?", strings.TrimPrefix(location.Path, "/event/")).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(suffix, "nice") && location.Query().Get("host") != "nice" {
		t.Fatal("Nicevites override lost on redirect")
	}
	return event
}

func TestCreateInvitation(t *testing.T) {
	for _, voice := range []string{"aggro", "nice"} {
		t.Run(voice, func(t *testing.T) {
			ai := &fakeGenerator{}
			s, handler := testApp(t, ai)
			suffix := ""
			if voice == "nice" {
				suffix = "?host=nice"
			}
			event := createEvent(t, s, handler, suffix, inputForm())
			if event.Voice != voice || ai.last.Voice != voice || len(event.Image) == 0 {
				t.Fatalf("wrong saved event: %+v", event)
			}
			if !strings.Contains(event.Markdown, directionsURL(event.Location)) {
				t.Fatal("missing encoded directions")
			}
			for range 2 {
				page := request(t, handler, "GET", "/event/1", nil)
				if page.Code != 200 || strings.Count(page.Body.String(), "<h1>") != 1 || !strings.Contains(page.Body.String(), "<strong>snacks</strong>") || !strings.Contains(page.Body.String(), `/event/1/image`) {
					t.Fatalf("guest page: %s", page.Body.String())
				}
				if strings.Contains(page.Body.String(), "Publish invitation") || strings.Contains(page.Body.String(), "Download invitation") {
					t.Fatal("event should be ready to share without another step")
				}
			}
			if w := request(t, handler, "GET", "/event/1/image", nil); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
				t.Fatal("artwork unavailable")
			}
			if ai.textCalls != 1 || ai.imageCalls != 1 {
				t.Fatal("generation should happen only once during creation")
			}
			if !strings.Contains(event.GoogleCalendarLink(), "location=") || strings.Contains(event.PlainDescription(), "**") {
				t.Fatal("calendar not using plain text/location")
			}
			rsvp := request(t, handler, "POST", "/rsvp?host=nice", url.Values{"Attendee": {"Pat"}, "Guests": {"2"}, "EventID": {"1"}, "Declined": {"false"}})
			if rsvp.Code != 302 || rsvp.Header().Get("Location") != "/event/1?host=nice" {
				t.Fatal("RSVP failed")
			}
		})
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
			if mode == "text failure" {
				response := request(t, handler, "POST", "/event", form)
				if response.Code != 502 || !strings.Contains(response.Body.String(), "Dinner at 123 Main St") || !strings.Contains(response.Body.String(), "2026-11-21T18:00") || !strings.Contains(response.Body.String(), "America/Los_Angeles") || !strings.Contains(response.Body.String(), "Create without AI") {
					t.Fatalf("generation failure lost input or recovery action: %s", response.Body.String())
				}
				var count int64
				s.db.Model(&Event{}).Count(&count)
				if count != 0 || fake.imageCalls != 0 {
					t.Fatal("failed text generation should not create an event or generate artwork")
				}
				form.Set("Generate", "false")
			}
			if mode == "manual" {
				form.Set("Generate", "false")
			}
			event := createEvent(t, s, handler, "", form)
			if event.Description != form.Get("Description") || len(event.Image) != 0 {
				t.Fatal("source lost or unexpected artwork")
			}
			if mode == "image failure" {
				if !strings.Contains(event.Markdown, "# Come to dinner!") {
					t.Fatal("text lost on image failure")
				}
			} else if event.Markdown != event.Description {
				t.Fatal("manual invitation should use original text")
			}
			if mode == "manual" && (fake.textCalls != 0 || fake.imageCalls != 0) {
				t.Fatal("manual creation called AI")
			}
			if image := request(t, handler, "GET", "/event/1/image", nil); image.Code != 404 {
				t.Fatal("event without artwork should have no image")
			}
		})
	}
}

func TestResubmissionCreatesAnotherEvent(t *testing.T) {
	ai := &fakeGenerator{}
	s, handler := testApp(t, ai)
	first := createEvent(t, s, handler, "?host=nice", inputForm())
	form := inputForm()
	form.Set("Description", "Dinner at another venue")
	second := createEvent(t, s, handler, "?host=nice", form)
	if second.ID == first.ID || second.Description != form.Get("Description") || ai.textCalls != 2 {
		t.Fatal("resubmission should create another event")
	}
	var original Event
	if err := s.db.First(&original, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if original.Description != first.Description || original.Markdown != first.Markdown {
		t.Fatal("resubmission changed original event")
	}
}

func TestLegacyEventsAndFormValidation(t *testing.T) {
	s, handler := testApp(t, nil)
	for _, path := range []string{"/", "/?host=nice"} {
		page := request(t, handler, "GET", path, nil)
		if page.Code != 200 || !strings.Contains(page.Body.String(), "Create invitation") || strings.Contains(page.Body.String(), "Create without AI") {
			t.Fatalf("manual creation page: %s", page.Body.String())
		}
		if strings.Contains(path, "nice") && !strings.Contains(page.Body.String(), `/event?host=nice`) {
			t.Fatal("Nicevites override lost on creation form")
		}
	}
	form := inputForm()
	form.Set("Markdown", "injected")
	form.Set("Voice", "nice")
	form.Set("ID", "123")
	event := createEvent(t, s, handler, "", form)
	if event.Markdown != form.Get("Description") || event.Voice != "aggro" || event.ID == 123 {
		t.Fatal("form bound server-controlled fields")
	}
	old := Event{Description: "Old event! Bring snacks.", TimeZone: "America/Los_Angeles"}
	if err := s.db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if w := request(t, handler, "GET", "/event/2", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Old event!") || !strings.Contains(w.Body.String(), "Bring snacks.") {
		t.Fatal("legacy display failed")
	}
	for _, path := range []string{"/event/999/image", "/event/1/invitation.md", "/event/1/image", "/draft/missing", "/draft/missing/image"} {
		if w := request(t, handler, "GET", path, nil); w.Code != 404 {
			t.Fatalf("expected 404 at %s, got %d", path, w.Code)
		}
	}
	if w := request(t, handler, "POST", "/event/preview", form); w.Code != 404 {
		t.Fatal("preview endpoint should be removed")
	}
	for _, invalid := range []string{"", "Invalid/Timezone"} {
		form.Set("TimeZone", invalid)
		w := request(t, handler, "POST", "/event", form)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "Dinner at 123 Main St") {
			t.Fatal("invalid input not retained")
		}
	}
	form.Set("TimeZone", "America/Los_Angeles")
	form.Set("Description", strings.Repeat("x", 12001))
	if w := request(t, handler, "POST", "/event", form); w.Code != 400 {
		t.Fatal("oversized input accepted")
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
