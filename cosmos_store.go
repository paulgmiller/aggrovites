package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

const (
	eventDocType = "event"
	rsvpDocType  = "rsvp"
)

var errNotFound = errors.New("not found")

type cosmosStore struct {
	client    *azcosmos.Client
	container *azcosmos.ContainerClient
	database  string
	name      string
}

func newCosmosStoreFromEnv(ctx context.Context) (*cosmosStore, error) {
	endpoint := os.Getenv("COSMOS_ENDPOINT")
	key := os.Getenv("COSMOS_KEY")
	if endpoint == "" || key == "" {
		return nil, errors.New("COSMOS_ENDPOINT and COSMOS_KEY must both be set")
	}
	database := os.Getenv("COSMOS_DATABASE")
	if database == "" {
		database = "aggrovites"
	}
	container := os.Getenv("COSMOS_CONTAINER")
	if container == "" {
		container = "events"
	}

	cred, err := azcosmos.NewKeyCredential(key)
	if err != nil {
		return nil, err
	}
	client, err := azcosmos.NewClientWithKey(endpoint, cred, nil)
	if err != nil {
		return nil, err
	}
	store := &cosmosStore{client: client, database: database, name: container}
	if err := store.ensureSchema(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *cosmosStore) ensureSchema(ctx context.Context) error {
	_, err := s.client.CreateDatabase(ctx, azcosmos.DatabaseProperties{ID: s.database}, nil)
	if err != nil && !isStatus(err, http.StatusConflict) {
		return err
	}

	db, err := s.client.NewDatabase(s.database)
	if err != nil {
		return err
	}
	properties := azcosmos.ContainerProperties{
		ID: s.name,
		PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{
			Paths: []string{"/event_id"},
		},
	}
	_, err = db.CreateContainer(ctx, properties, nil)
	if err != nil && !isStatus(err, http.StatusConflict) {
		return err
	}

	container, err := db.NewContainer(s.name)
	if err != nil {
		return err
	}
	s.container = container
	return nil
}

func (s *cosmosStore) Ready(ctx context.Context) error {
	_, err := s.container.Read(ctx, nil)
	return err
}

func (s *cosmosStore) CreateEvent(ctx context.Context, event *Event) error {
	id, err := newID()
	if err != nil {
		return err
	}
	event.ID = eventDocID(id)
	event.EventID = id
	event.DocType = eventDocType

	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = s.container.CreateItem(ctx, azcosmos.NewPartitionKeyString(event.EventID), body, nil)
	return err
}

func (s *cosmosStore) GetEvent(ctx context.Context, id string) (*Event, error) {
	pk := azcosmos.NewPartitionKeyString(id)
	resp, err := s.container.ReadItem(ctx, pk, eventDocID(id), nil)
	if isStatus(err, http.StatusNotFound) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	var event Event
	if err := json.Unmarshal(resp.Value, &event); err != nil {
		return nil, err
	}

	query := "SELECT * FROM c WHERE c.event_id = @event_id AND c.doc_type = @doc_type"
	pager := s.container.NewQueryItemsPager(query, pk, &azcosmos.QueryOptions{
		QueryParameters: []azcosmos.QueryParameter{
			{Name: "@event_id", Value: id},
			{Name: "@doc_type", Value: rsvpDocType},
		},
	})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			var rsvp Rsvp
			if err := json.Unmarshal(item, &rsvp); err != nil {
				return nil, err
			}
			event.Rsvps = append(event.Rsvps, rsvp)
		}
	}
	return &event, nil
}

func (s *cosmosStore) CreateRsvp(ctx context.Context, rsvp *Rsvp) error {
	rsvp.EventID = stringsTrim(rsvp.EventID)
	rsvp.Attendee = stringsTrim(rsvp.Attendee)
	if rsvp.Guests == 0 && !rsvp.Declined {
		rsvp.Guests = 1
	}
	rsvp.AttendeeID = normalizeAttendee(rsvp.Attendee)
	rsvp.ID = rsvpDocID(rsvp.EventID, rsvp.Attendee)
	rsvp.DocType = rsvpDocType

	body, err := json.Marshal(rsvp)
	if err != nil {
		return err
	}
	_, err = s.container.CreateItem(ctx, azcosmos.NewPartitionKeyString(rsvp.EventID), body, nil)
	if isStatus(err, http.StatusConflict) {
		return fmt.Errorf("already got an RSVP for %s", rsvp.Attendee)
	}
	return err
}

func isStatus(err error, status int) bool {
	var responseErr *azcore.ResponseError
	return errors.As(err, &responseErr) && responseErr.StatusCode == status
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}
