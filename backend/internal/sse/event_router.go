// Package sse provides Server-Sent Events (SSE) functionality for real-time notifications.
package sse

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
)

// EventRouter routes events from the event bus to user connections.
// It ensures user isolation by only delivering events to connections
// belonging to the event's target user.
type EventRouter struct {
	connManager *InMemoryConnectionManager
	eventBus    *events.InMemoryEventBus
}

// NewEventRouter creates a new EventRouter.
func NewEventRouter(connManager *InMemoryConnectionManager, eventBus *events.InMemoryEventBus) *EventRouter {
	return &EventRouter{
		connManager: connManager,
		eventBus:    eventBus,
	}
}

// RouteNewEmailEvent creates and publishes a new_email event to the alias owner.
// This ensures the event is only sent to connections belonging to the email owner.
func (r *EventRouter) RouteNewEmailEvent(userID string, emailData events.NewEmailEvent) error {
	data, err := json.Marshal(emailData)
	if err != nil {
		return err
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeNewEmail,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now(),
	}

	return r.eventBus.Publish(event)
}

// RouteEmailDeletedEvent creates and publishes an email_deleted event to the owner.
func (r *EventRouter) RouteEmailDeletedEvent(userID string, emailData events.EmailDeletedEvent) error {
	data, err := json.Marshal(emailData)
	if err != nil {
		return err
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeEmailDeleted,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now(),
	}

	return r.eventBus.Publish(event)
}

// RouteAliasCreatedEvent creates and publishes an alias_created event to the owner.
func (r *EventRouter) RouteAliasCreatedEvent(userID string, aliasData events.AliasCreatedEvent) error {
	data, err := json.Marshal(aliasData)
	if err != nil {
		return err
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeAliasCreated,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now(),
	}

	return r.eventBus.Publish(event)
}

// RouteAliasDeletedEvent creates and publishes an alias_deleted event to the owner.
func (r *EventRouter) RouteAliasDeletedEvent(userID string, aliasData events.AliasDeletedEvent) error {
	data, err := json.Marshal(aliasData)
	if err != nil {
		return err
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeAliasDeleted,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now(),
	}

	return r.eventBus.Publish(event)
}

// RouteDomainVerifiedEvent creates and publishes a domain_verified event to the owner.
func (r *EventRouter) RouteDomainVerifiedEvent(userID string, domainData events.DomainVerifiedEvent) error {
	data, err := json.Marshal(domainData)
	if err != nil {
		return err
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeDomainVerified,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now(),
	}

	return r.eventBus.Publish(event)
}

// RouteDomainDeletedEvent creates and publishes a domain_deleted event to the owner.
func (r *EventRouter) RouteDomainDeletedEvent(userID string, domainData events.DomainDeletedEvent) error {
	data, err := json.Marshal(domainData)
	if err != nil {
		return err
	}

	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeDomainDeleted,
		UserID:    userID,
		Data:      data,
		Timestamp: time.Now(),
	}

	return r.eventBus.Publish(event)
}

// BroadcastToUser sends an event directly to all connections for a specific user.
// This bypasses the event bus and sends directly to the connection manager.
// Use this for events that don't need to be stored for replay.
func (r *EventRouter) BroadcastToUser(userID string, event events.Event) error {
	return r.connManager.Broadcast(userID, event)
}

// GetUserConnectionCount returns the number of active connections for a user.
func (r *EventRouter) GetUserConnectionCount(userID string) int {
	return r.connManager.CountConnections(userID)
}

// HasActiveConnections returns true if the user has at least one active connection.
func (r *EventRouter) HasActiveConnections(userID string) bool {
	return r.connManager.CountConnections(userID) > 0
}
