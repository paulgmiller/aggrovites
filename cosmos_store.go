package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/btnguyen2k/gocosmos"
)

const (
	cosmosEventDoc = "event"
	cosmosRsvpDoc  = "rsvp"
)

var cosmosIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type cosmosStore struct {
	db         *sql.DB
	database   string
	collection string
}

func newCosmosStoreFromEnv() (*cosmosStore, bool, error) {
	endpoint := os.Getenv("COSMOS_ENDPOINT")
	key := os.Getenv("COSMOS_KEY")
	if endpoint == "" && key == "" {
		return nil, false, nil
	}
	if endpoint == "" || key == "" {
		return nil, true, errors.New("COSMOS_ENDPOINT and COSMOS_KEY must both be set")
	}
	database := os.Getenv("COSMOS_DATABASE")
	if database == "" {
		database = "aggrovites"
	}
	collection := os.Getenv("COSMOS_CONTAINER")
	if collection == "" {
		collection = "events"
	}
	store, err := newCosmosStore(endpoint, key, database, collection)
	return store, true, err
}

func newCosmosStore(endpoint, key, database, collection string) (*cosmosStore, error) {
	if !cosmosIdentifierPattern.MatchString(database) {
		return nil, fmt.Errorf("invalid COSMOS_DATABASE %q", database)
	}
	if !cosmosIdentifierPattern.MatchString(collection) {
		return nil, fmt.Errorf("invalid COSMOS_CONTAINER %q", collection)
	}
	dsn := fmt.Sprintf("AccountEndpoint=%s;AccountKey=%s;DefaultDb=%s;AutoId=false", endpoint, key, database)
	db, err := sql.Open("gocosmos", dsn)
	if err != nil {
		return nil, err
	}
	store := &cosmosStore{db: db, database: database, collection: collection}
	if err := store.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *cosmosStore) ensureSchema() error {
	if _, err := s.db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", s.database)); err != nil {
		return err
	}
	_, err := s.db.Exec(fmt.Sprintf("CREATE COLLECTION IF NOT EXISTS %s WITH PK=/public_id", s.collection))
	return err
}

func (s *cosmosStore) Ready() error {
	return s.db.Ping()
}

func (s *cosmosStore) CreateEvent(event *Event) error {
	if event.PublicID == "" {
		publicID, err := newPublicID()
		if err != nil {
			return err
		}
		event.PublicID = publicID
	}
	_, err := s.db.Exec(
		fmt.Sprintf("INSERT INTO %s (id,public_id,doc_type,description,start,time_zone) VALUES (@1,@2,@3,@4,@5,@6) WITH PK=/public_id", s.collection),
		cosmosEventID(event.PublicID),
		event.PublicID,
		cosmosEventDoc,
		event.Description,
		event.Start.Format(time.RFC3339Nano),
		event.TimeZone,
	)
	return err
}

func (s *cosmosStore) GetEvent(id string) (*Event, error) {
	eventRows, err := s.queryMaps(
		fmt.Sprintf("SELECT c.id,c.public_id,c.doc_type,c.description,c.start,c.time_zone FROM %s c WHERE c.public_id=@1 AND c.doc_type=@2", s.collection),
		id,
		cosmosEventDoc,
	)
	if err != nil {
		return nil, err
	}
	if len(eventRows) == 0 {
		return nil, errNotFound
	}
	event, err := eventFromCosmos(eventRows[0])
	if err != nil {
		return nil, err
	}

	rsvpRows, err := s.queryMaps(
		fmt.Sprintf("SELECT c.id,c.public_id,c.doc_type,c.attendee,c.attendee_normalized,c.guests,c.declined FROM %s c WHERE c.public_id=@1 AND c.doc_type=@2", s.collection),
		id,
		cosmosRsvpDoc,
	)
	if err != nil {
		return nil, err
	}
	event.Rsvps = make([]Rsvp, 0, len(rsvpRows))
	for _, row := range rsvpRows {
		rsvp, err := rsvpFromCosmos(row)
		if err != nil {
			return nil, err
		}
		event.Rsvps = append(event.Rsvps, rsvp)
	}
	return event, nil
}

func (s *cosmosStore) CreateRsvp(rsvp *Rsvp) error {
	normalized := normalizeAttendee(rsvp.Attendee)
	_, err := s.db.Exec(
		fmt.Sprintf("INSERT INTO %s (id,public_id,doc_type,attendee,attendee_normalized,guests,declined) VALUES (@1,@2,@3,@4,@5,@6,@7) WITH PK=/public_id", s.collection),
		cosmosRsvpID(rsvp.EventPublicID, normalized),
		rsvp.EventPublicID,
		cosmosRsvpDoc,
		rsvp.Attendee,
		normalized,
		rsvp.Guests,
		rsvp.Declined,
	)
	return err
}

func (s *cosmosStore) queryMaps(query string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	results := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		scanValues := make([]any, len(cols))
		for i := range values {
			scanValues[i] = &values[i]
		}
		if err := rows.Scan(scanValues...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[strings.ToLower(col)] = values[i]
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func eventFromCosmos(row map[string]any) (*Event, error) {
	start, err := time.Parse(time.RFC3339Nano, stringValue(row["start"]))
	if err != nil {
		return nil, err
	}
	return &Event{
		Description: stringValue(row["description"]),
		Start:       start,
		TimeZone:    stringValue(row["time_zone"]),
		PublicID:    stringValue(row["public_id"]),
	}, nil
}

func rsvpFromCosmos(row map[string]any) (Rsvp, error) {
	return Rsvp{
		Attendee:      stringValue(row["attendee"]),
		Guests:        uintValue(row["guests"]),
		Declined:      boolValue(row["declined"]),
		EventPublicID: stringValue(row["public_id"]),
	}, nil
}

func cosmosEventID(publicID string) string {
	return "event:" + publicID
}

func cosmosRsvpID(publicID, normalizedAttendee string) string {
	return "rsvp:" + publicID + ":" + url.PathEscape(normalizedAttendee)
}

func normalizeAttendee(attendee string) string {
	return strings.ToLower(strings.TrimSpace(attendee))
}

func stringValue(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}

func uintValue(v any) uint {
	switch value := v.(type) {
	case int64:
		return uint(value)
	case int:
		return uint(value)
	case float64:
		return uint(value)
	case []byte:
		var parsed uint
		_, _ = fmt.Sscan(string(value), &parsed)
		return parsed
	case string:
		var parsed uint
		_, _ = fmt.Sscan(value, &parsed)
		return parsed
	default:
		return 0
	}
}

func boolValue(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case []byte:
		return strings.EqualFold(string(value), "true")
	case string:
		return strings.EqualFold(value, "true")
	default:
		return false
	}
}
