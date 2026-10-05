package main

import (
	"fmt"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

func openDestination() (*gorm.DB, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		filename := os.Getenv("SQLLITE_FILE")
		if filename == "" {
			filename = "test.db"
		}
		db, err := gorm.Open(sqlite.Open(filename), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("open SQLite database: %w", err)
		}
		return db, nil
	}
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open CockroachDB: %w", err)
	}
	return db, nil
}

func migrateSchema(db *gorm.DB) error {
	return db.AutoMigrate(&Event{}, &Rsvp{})
}

func runDataMigration(dst *gorm.DB) error {
	var source gorm.Dialector
	if dsn := os.Getenv("MSSQL_DSN"); dsn != "" {
		source = sqlserver.Open(dsn)
	} else if filename := os.Getenv("SQLLITE_FILE"); filename != "" {
		source = sqlite.Open(filename)
	} else {
		return fmt.Errorf("set MSSQL_DSN or SQLLITE_FILE for the source database")
	}
	src, err := gorm.Open(source, &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open source database: %w", err)
	}
	if err := migrateSchema(dst); err != nil {
		return fmt.Errorf("migrate destination schema: %w", err)
	}
	if err := copyData(src, dst); err != nil {
		return err
	}
	fmt.Println("Data migration complete")
	return nil
}

// copyData preserves primary keys, timestamps, and soft-deleted rows. Run it
// after stopping writes to the source, and only against an empty destination.
func copyData(src, dst *gorm.DB) error {
	var events []Event
	if err := src.Unscoped().Order("id").Find(&events).Error; err != nil {
		return fmt.Errorf("read source events: %w", err)
	}
	var rsvps []Rsvp
	if err := src.Unscoped().Order("id").Find(&rsvps).Error; err != nil {
		return fmt.Errorf("read source RSVPs: %w", err)
	}

	if err := dst.Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{"events", "rsvps"} {
			var count int64
			if err := tx.Unscoped().Table(table).Count(&count).Error; err != nil {
				return fmt.Errorf("check destination %s: %w", table, err)
			}
			if count != 0 {
				return fmt.Errorf("destination %s is not empty; data migration requires an empty destination", table)
			}
		}
		for _, event := range events {
			row := map[string]interface{}{
				"id": event.ID, "created_at": event.CreatedAt, "updated_at": event.UpdatedAt,
				"deleted_at": deletedAt(event.DeletedAt), "description": event.Description,
				"start": event.Start, "time_zone": event.TimeZone,
			}
			if err := tx.Table("events").Create(row).Error; err != nil {
				return fmt.Errorf("copy event %d: %w", event.ID, err)
			}
		}
		for _, rsvp := range rsvps {
			row := map[string]interface{}{
				"id": rsvp.ID, "created_at": rsvp.CreatedAt, "updated_at": rsvp.UpdatedAt,
				"deleted_at": deletedAt(rsvp.DeletedAt), "attendee": rsvp.Attendee,
				"guests": rsvp.Guests, "declined": rsvp.Declined, "event_id": rsvp.EventID,
			}
			if err := tx.Table("rsvps").Create(row).Error; err != nil {
				return fmt.Errorf("copy RSVP %d: %w", rsvp.ID, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("Copied %d events and %d RSVPs\n", len(events), len(rsvps))
	return nil
}

func deletedAt(value gorm.DeletedAt) *time.Time {
	if value.Valid {
		return &value.Time
	}
	return nil
}
