package main

import (
	"time"

	"gorm.io/gorm"
)

type Event struct {
	gorm.Model
	Description string
	Start       time.Time `time_format:"2006-01-02T15:04"` //no timezone ... :(
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
	return e.Start.Format(time.RFC3339)
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
