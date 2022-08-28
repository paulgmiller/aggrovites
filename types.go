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
	Total       uint `gorm:"-"`
}

type Rsvp struct {
	gorm.Model
	Attendee string
	Guests   uint `gorm:"default:1"`
	Declined bool
	EventID  uint
}
