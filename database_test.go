package main

import (
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCopyData(t *testing.T) {
	open := func(name string) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := migrateSchema(db); err != nil {
			t.Fatal(err)
		}
		return db
	}
	src, dst := open("source.db"), open("destination.db")
	stamp := time.Date(2023, 4, 5, 6, 7, 8, 0, time.UTC)
	event := Event{Model: gorm.Model{ID: 42, CreatedAt: stamp, UpdatedAt: stamp}, Description: "Meeting", Start: stamp, TimeZone: "UTC"}
	if err := src.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	rsvp := Rsvp{Model: gorm.Model{ID: 91, CreatedAt: stamp, UpdatedAt: stamp}, Attendee: "Ada", Guests: 2, EventID: event.ID}
	if err := src.Create(&rsvp).Error; err != nil {
		t.Fatal(err)
	}
	if err := src.Delete(&rsvp).Error; err != nil {
		t.Fatal(err)
	}
	if err := src.Table("rsvps").Create(map[string]interface{}{
		"id": 92, "created_at": stamp, "updated_at": stamp,
		"attendee": "Ben", "guests": 0, "declined": true, "event_id": event.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := copyData(src, dst); err != nil {
		t.Fatal(err)
	}
	var gotEvent Event
	if err := dst.First(&gotEvent, 42).Error; err != nil {
		t.Fatal(err)
	}
	if gotEvent.Description != event.Description || !gotEvent.CreatedAt.Equal(stamp) || gotEvent.TimeZone != "UTC" {
		t.Fatalf("event was not preserved: %+v", gotEvent)
	}
	var gotRsvp Rsvp
	if err := dst.Unscoped().First(&gotRsvp, 91).Error; err != nil {
		t.Fatal(err)
	}
	if gotRsvp.EventID != 42 || gotRsvp.Guests != 2 || !gotRsvp.DeletedAt.Valid {
		t.Fatalf("RSVP was not preserved: %+v", gotRsvp)
	}
	gotRsvp = Rsvp{}
	if err := dst.Unscoped().First(&gotRsvp, 92).Error; err != nil {
		t.Fatal(err)
	}
	if gotRsvp.Guests != 0 || !gotRsvp.Declined {
		t.Fatalf("zero guest RSVP was not preserved: %+v", gotRsvp)
	}
	if err := copyData(src, dst); err == nil {
		t.Fatal("expected a second migration into a nonempty destination to fail")
	}
}

func TestLocalSQLiteDestination(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SQLLITE_FILE", filepath.Join(t.TempDir(), "local.db"))
	db, err := openDestination()
	if err != nil {
		t.Fatal(err)
	}
	if db.Dialector.Name() != "sqlite" {
		t.Fatalf("expected SQLite, got %s", db.Dialector.Name())
	}
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
}
