package main

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestGormStore(t *testing.T) *gormStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Event{}, &Rsvp{}); err != nil {
		t.Fatal(err)
	}
	return newGormStore(db)
}

func TestGormStoreCreateAndGetEvent(t *testing.T) {
	store := newTestGormStore(t)
	event := &Event{
		Description: "Dinner. Bring snacks.",
		Start:       time.Date(2026, 7, 1, 18, 0, 0, 0, time.UTC),
		TimeZone:    "America/Los_Angeles",
	}
	if err := store.CreateEvent(event); err != nil {
		t.Fatal(err)
	}
	if event.PublicID == "" {
		t.Fatal("CreateEvent did not assign PublicID")
	}

	got, err := store.GetEvent(event.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != event.Description {
		t.Fatalf("description = %q, want %q", got.Description, event.Description)
	}

	legacy, err := store.GetEvent(got.RouteID())
	if err != nil {
		t.Fatal(err)
	}
	if legacy.RouteID() != event.PublicID {
		t.Fatalf("route id = %q, want %q", legacy.RouteID(), event.PublicID)
	}
}

func TestGormStoreLegacyNumericLookup(t *testing.T) {
	store := newTestGormStore(t)
	event := &Event{
		Description: "Old link",
		Start:       time.Date(2026, 7, 1, 18, 0, 0, 0, time.UTC),
		TimeZone:    "America/Los_Angeles",
	}
	if err := store.db.Create(event).Error; err != nil {
		t.Fatal(err)
	}

	got, err := store.GetEvent(event.RouteID())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != event.ID {
		t.Fatalf("ID = %d, want %d", got.ID, event.ID)
	}
}

func TestGormStoreCreateRsvp(t *testing.T) {
	store := newTestGormStore(t)
	event := &Event{
		Description: "RSVP test",
		Start:       time.Date(2026, 7, 1, 18, 0, 0, 0, time.UTC),
		TimeZone:    "America/Los_Angeles",
	}
	if err := store.CreateEvent(event); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateRsvp(&Rsvp{EventPublicID: event.PublicID, Attendee: "Ada", Guests: 2}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetEvent(event.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rsvps) != 1 {
		t.Fatalf("len(Rsvps) = %d, want 1", len(got.Rsvps))
	}
	if got.Rsvps[0].EventID != event.ID {
		t.Fatalf("EventID = %d, want %d", got.Rsvps[0].EventID, event.ID)
	}
	if got.Rsvps[0].EventPublicID != event.PublicID {
		t.Fatalf("EventPublicID = %q, want %q", got.Rsvps[0].EventPublicID, event.PublicID)
	}
}

func TestGormStoreCreateRsvpFromLegacyNumericID(t *testing.T) {
	store := newTestGormStore(t)
	event := &Event{
		Description: "Legacy RSVP test",
		Start:       time.Date(2026, 7, 1, 18, 0, 0, 0, time.UTC),
		TimeZone:    "America/Los_Angeles",
	}
	if err := store.CreateEvent(event); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateRsvp(&Rsvp{EventPublicID: strconv.FormatUint(uint64(event.ID), 10), Attendee: "Ada", Guests: 2}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetEvent(event.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rsvps[0].EventPublicID != event.PublicID {
		t.Fatalf("EventPublicID = %q, want %q", got.Rsvps[0].EventPublicID, event.PublicID)
	}
}

func TestGormStoreNotFound(t *testing.T) {
	store := newTestGormStore(t)
	_, err := store.GetEvent("missing")
	if !errors.Is(err, errNotFound) {
		t.Fatalf("err = %v, want errNotFound", err)
	}
}

func TestNormalizeAttendee(t *testing.T) {
	if got := normalizeAttendee("  Ada LOVELACE "); got != "ada lovelace" {
		t.Fatalf("normalizeAttendee = %q", got)
	}
}

func TestNewPublicID(t *testing.T) {
	id, err := newPublicID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 16 {
		t.Fatalf("len(id) = %d, want 16", len(id))
	}
	if strings.ToLower(id) != id {
		t.Fatalf("id = %q, want lowercase", id)
	}
}
