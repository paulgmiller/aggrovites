package main

import (
	"time"

	"gorm.io/gorm"
)

type Event struct {
	gorm.Model
	Description string
	Start       time.Time `time_format:"2006-01-02T15:04"` //no timezone from datetime-local picker.
	TimeZone    string
	Rsvps       []Rsvp
}

type Rsvp struct {
	gorm.Model
	Attendee string
	Guests   uint `gorm:"default:1"`
	Declined bool
	EventID  uint
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
