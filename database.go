package main

import (
	"fmt"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
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
