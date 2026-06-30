package main

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Event struct {
	ID          string    `json:"id" form:"-"`
	EventID     string    `json:"event_id" form:"-"`
	DocType     string    `json:"doc_type" form:"-"`
	Description string    `json:"description" form:"Description"`
	Start       time.Time `json:"start" form:"Start" time_format:"2006-01-02T15:04"` //no timezone from datetime-local picker.
	TimeZone    string    `json:"time_zone" form:"TimeZone"`
	Rsvps       []Rsvp    `json:"-" form:"-"`
}

type Rsvp struct {
	ID         string `json:"id" form:"-"`
	EventID    string `json:"event_id" form:"EventID"`
	DocType    string `json:"doc_type" form:"-"`
	Attendee   string `json:"attendee" form:"Attendee"`
	AttendeeID string `json:"attendee_id" form:"-"`
	Guests     uint   `json:"guests" form:"Guests"`
	Declined   bool   `json:"declined" form:"Declined"`
}

func newID() (string, error) {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])), nil
}

func eventDocID(eventID string) string {
	return "event:" + eventID
}

func rsvpDocID(eventID, attendee string) string {
	return "rsvp:" + eventID + ":" + url.PathEscape(normalizeAttendee(attendee))
}

func normalizeAttendee(attendee string) string {
	return strings.ToLower(strings.TrimSpace(attendee))
}

// this puts this in  ISO 8601  so javascript can parse it
func (e Event) PrettyStart() string {
	tz, err := time.LoadLocation(e.TimeZone)
	if e.TimeZone == "" || err != nil {
		tz, _ = time.LoadLocation("America/Los_Angeles")
	}

	zoned := time.Date(e.Start.Year(), e.Start.Month(), e.Start.Day(), e.Start.Hour(), e.Start.Minute(), e.Start.Second(), e.Start.Nanosecond(), tz)
	//log.Printf("%s stored with %s: parsed to %s and utc %s", e.Start, e.TimeZone, zoned.Format(time.RFC3339), zoned.UTC().Format(time.RFC3339))
	return zoned.Format(time.RFC3339)
}

// sets location on tz
func (e Event) Validate() error {
	_, err := time.LoadLocation(e.TimeZone)
	return err
}

func (e Event) Total() uint {
	var total uint
	for _, r := range e.Winners() {
		total += r.Guests
	}
	return total
}

func (e Event) Losers() []Rsvp {
	var losers []Rsvp
	for _, r := range e.Rsvps {
		if r.Declined {
			losers = append(losers, r)
		}
	}
	return losers
}

func (e Event) Winners() []Rsvp {
	var winners []Rsvp
	for _, r := range e.Rsvps {
		if !r.Declined {
			winners = append(winners, r)
		}
	}
	return winners
}

// Function to create a Google Calendar link
func (e Event) GoogleCalendarLink() string {
	baseURL := "https://www.google.com/calendar/render?action=TEMPLATE"
	end := e.Start.Add(time.Hour)
	params := url.Values{}
	//params.Add("text", "aggrovite")
	params.Add("dates", fmt.Sprintf("%s/%s", e.Start.Format("20060102T150405Z"), end.Format("20060102T150405Z")))

	params.Add("details", e.Description)
	//params.Add("location", event.Location)

	return baseURL + "&" + params.Encode()
}

// Function to create an Outlook Calendar link
func (e Event) OutlookCalendarLink() string {
	baseURL := "https://outlook.live.com/calendar/0/deeplink/compose"
	end := e.Start.Add(time.Hour)
	params := url.Values{}
	params.Add("path", "/calendar/action/compose")
	params.Add("rru", "addevent")
	params.Add("startdt", e.Start.Format("2006-01-02T15:04:05"))
	params.Add("enddt", end.Format("2006-01-02T15:04:05"))
	//params.Add("subject", "aggrovite")
	params.Add("body", e.Description)
	//params.Add("location", event.Location)

	return baseURL + "?" + params.Encode()
}

// Title returns the first sentence or first 10 words of the description for use as the H1 title
func (e Event) Title() string {
	if e.Description == "" {
		return ""
	}

	// Try to find the first sentence ending with . ! or ?
	for i, char := range e.Description {
		if char == '.' || char == '!' || char == '?' {
			title := strings.TrimSpace(e.Description[:i+1])
			words := strings.Fields(title)
			// If the sentence is reasonable length (1-20 words), use it
			if len(words) >= 1 && len(words) <= 20 {
				return title
			}
		}
	}

	// If no sentence ending found or sentence is too long, take first 10 words
	words := strings.Fields(e.Description)
	if len(words) == 0 {
		return ""
	}

	if len(words) <= 10 {
		return e.Description
	}

	return strings.Join(words[:10], " ")
}

// Body returns the remaining part of the description after the title
func (e Event) Body() string {
	if e.Description == "" {
		return ""
	}

	title := e.Title()
	if title == e.Description {
		return "" // The entire description is the title
	}

	// Find where the title ends in the original description
	titleEnd := strings.Index(e.Description, title)
	if titleEnd == -1 {
		return e.Description // Fallback
	}

	remaining := strings.TrimSpace(e.Description[titleEnd+len(title):])
	return remaining
}
