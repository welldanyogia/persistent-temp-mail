package sse

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/events"
	"pgregory.net/rapid"
)

// Feature: realtime-notifications, Property 5: New Email Event Routing
// *For any* new email received, the SSE_Server SHALL send new_email event only to
// connections belonging to the alias owner. The event SHALL include all required fields:
// id, alias_id, alias_email, from_address, from_name, subject, preview_text, received_at,
// has_attachments, size_bytes.
// **Validates: Requirements 3.1, 3.2, 3.3**
func TestProperty5_NewEmailEventRouting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user IDs
		ownerUserID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "ownerUserID")
		otherUserID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "otherUserID")

		// Ensure different users
		for ownerUserID == otherUserID {
			otherUserID = rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "otherUserID2")
		}

		// Generate random email event data
		emailID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "emailID")
		aliasID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "aliasID")
		aliasEmail := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "aliasEmail")
		fromAddress := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "fromAddress")
		fromName := rapid.StringMatching(`[A-Z][a-z]{3,10} [A-Z][a-z]{3,10}`).Draw(t, "fromName")
		subject := rapid.StringMatching(`[A-Za-z0-9 ]{10,50}`).Draw(t, "subject")
		previewText := rapid.StringMatching(`[A-Za-z0-9 ]{20,100}`).Draw(t, "previewText")
		hasAttachments := rapid.Bool().Draw(t, "hasAttachments")
		sizeBytes := rapid.Int64Range(100, 10000000).Draw(t, "sizeBytes")

		// Setup
		config := DefaultConfig()
		eventStore := events.NewEventStore(100)
		eventBus := events.NewEventBus(eventStore)
		connManager := NewConnectionManager(config)
		router := NewEventRouter(connManager, eventBus)

		// Create connections for both users
		ownerConn, ownerWriter := createTestConnection(ownerUserID)
		otherConn, otherWriter := createTestConnection(otherUserID)
		connManager.AddConnection(ownerUserID, ownerConn)
		connManager.AddConnection(otherUserID, otherConn)

		// Subscribe both users to event bus
		var ownerReceived []events.Event
		var otherReceived []events.Event
		var mu sync.Mutex

		eventBus.Subscribe(ownerUserID, func(event events.Event) {
			mu.Lock()
			ownerReceived = append(ownerReceived, event)
			mu.Unlock()
			connManager.Broadcast(ownerUserID, event)
		})

		eventBus.Subscribe(otherUserID, func(event events.Event) {
			mu.Lock()
			otherReceived = append(otherReceived, event)
			mu.Unlock()
			connManager.Broadcast(otherUserID, event)
		})

		// Create and route new email event
		emailEvent := events.NewEmailEvent{
			ID:             emailID,
			AliasID:        aliasID,
			AliasEmail:     aliasEmail,
			FromAddress:    fromAddress,
			FromName:       &fromName,
			Subject:        &subject,
			PreviewText:    previewText,
			ReceivedAt:     time.Now(),
			HasAttachments: hasAttachments,
			SizeBytes:      sizeBytes,
		}

		err := router.RouteNewEmailEvent(ownerUserID, emailEvent)
		if err != nil {
			t.Fatalf("failed to route new email event: %v", err)
		}

		// Wait for event delivery
		time.Sleep(50 * time.Millisecond)

		// Property 1: Owner should receive the event
		mu.Lock()
		ownerReceivedCount := len(ownerReceived)
		otherReceivedCount := len(otherReceived)
		mu.Unlock()

		if ownerReceivedCount != 1 {
			t.Errorf("owner should receive exactly 1 event, got %d", ownerReceivedCount)
		}

		// Property 2: Other user should NOT receive the event (user isolation)
		if otherReceivedCount != 0 {
			t.Errorf("other user should not receive event, got %d", otherReceivedCount)
		}

		// Property 3: Owner's connection should have the event written
		ownerBody := ownerWriter.Body.String()
		if !strings.Contains(ownerBody, "event: new_email") {
			t.Error("owner connection should receive new_email event")
		}

		// Property 4: Other user's connection should NOT have the event
		otherBody := otherWriter.Body.String()
		if strings.Contains(otherBody, "event: new_email") {
			t.Error("other user connection should NOT receive new_email event")
		}

		// Property 5: Event should contain all required fields
		if ownerReceivedCount > 0 {
			mu.Lock()
			receivedEvent := ownerReceived[0]
			mu.Unlock()

			var receivedEmailEvent events.NewEmailEvent
			if err := json.Unmarshal(receivedEvent.Data, &receivedEmailEvent); err != nil {
				t.Fatalf("failed to unmarshal event data: %v", err)
			}

			// Verify all required fields
			if receivedEmailEvent.ID != emailID {
				t.Errorf("expected ID %s, got %s", emailID, receivedEmailEvent.ID)
			}
			if receivedEmailEvent.AliasID != aliasID {
				t.Errorf("expected AliasID %s, got %s", aliasID, receivedEmailEvent.AliasID)
			}
			if receivedEmailEvent.AliasEmail != aliasEmail {
				t.Errorf("expected AliasEmail %s, got %s", aliasEmail, receivedEmailEvent.AliasEmail)
			}
			if receivedEmailEvent.FromAddress != fromAddress {
				t.Errorf("expected FromAddress %s, got %s", fromAddress, receivedEmailEvent.FromAddress)
			}
			if receivedEmailEvent.FromName == nil || *receivedEmailEvent.FromName != fromName {
				t.Error("FromName mismatch")
			}
			if receivedEmailEvent.Subject == nil || *receivedEmailEvent.Subject != subject {
				t.Error("Subject mismatch")
			}
			if receivedEmailEvent.PreviewText != previewText {
				t.Errorf("expected PreviewText %s, got %s", previewText, receivedEmailEvent.PreviewText)
			}
			if receivedEmailEvent.HasAttachments != hasAttachments {
				t.Errorf("expected HasAttachments %v, got %v", hasAttachments, receivedEmailEvent.HasAttachments)
			}
			if receivedEmailEvent.SizeBytes != sizeBytes {
				t.Errorf("expected SizeBytes %d, got %d", sizeBytes, receivedEmailEvent.SizeBytes)
			}
			if receivedEmailEvent.ReceivedAt.IsZero() {
				t.Error("ReceivedAt should not be zero")
			}
		}

		// Cleanup
		ownerConn.Close()
		otherConn.Close()
	})
}


// Feature: realtime-notifications, Property 6: Email Deleted Event
// *For any* email deletion, the SSE_Server SHALL send email_deleted event to the owner
// with id, alias_id, and deleted_at fields.
// **Validates: Requirements 4.1, 4.2**
func TestProperty6_EmailDeletedEvent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user IDs
		ownerUserID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "ownerUserID")
		otherUserID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "otherUserID")

		// Ensure different users
		for ownerUserID == otherUserID {
			otherUserID = rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "otherUserID2")
		}

		// Generate random email deleted event data
		emailID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "emailID")
		aliasID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "aliasID")

		// Setup
		config := DefaultConfig()
		eventStore := events.NewEventStore(100)
		eventBus := events.NewEventBus(eventStore)
		connManager := NewConnectionManager(config)
		router := NewEventRouter(connManager, eventBus)

		// Create connections for both users
		ownerConn, ownerWriter := createTestConnection(ownerUserID)
		otherConn, otherWriter := createTestConnection(otherUserID)
		connManager.AddConnection(ownerUserID, ownerConn)
		connManager.AddConnection(otherUserID, otherConn)

		// Subscribe both users to event bus
		var ownerReceived []events.Event
		var otherReceived []events.Event
		var mu sync.Mutex

		eventBus.Subscribe(ownerUserID, func(event events.Event) {
			mu.Lock()
			ownerReceived = append(ownerReceived, event)
			mu.Unlock()
			connManager.Broadcast(ownerUserID, event)
		})

		eventBus.Subscribe(otherUserID, func(event events.Event) {
			mu.Lock()
			otherReceived = append(otherReceived, event)
			mu.Unlock()
			connManager.Broadcast(otherUserID, event)
		})

		// Create and route email deleted event
		deletedAt := time.Now()
		emailDeletedEvent := events.EmailDeletedEvent{
			ID:        emailID,
			AliasID:   aliasID,
			DeletedAt: deletedAt,
		}

		err := router.RouteEmailDeletedEvent(ownerUserID, emailDeletedEvent)
		if err != nil {
			t.Fatalf("failed to route email deleted event: %v", err)
		}

		// Wait for event delivery
		time.Sleep(50 * time.Millisecond)

		// Property 1: Owner should receive the event
		mu.Lock()
		ownerReceivedCount := len(ownerReceived)
		otherReceivedCount := len(otherReceived)
		mu.Unlock()

		if ownerReceivedCount != 1 {
			t.Errorf("owner should receive exactly 1 event, got %d", ownerReceivedCount)
		}

		// Property 2: Other user should NOT receive the event
		if otherReceivedCount != 0 {
			t.Errorf("other user should not receive event, got %d", otherReceivedCount)
		}

		// Property 3: Owner's connection should have the event written
		ownerBody := ownerWriter.Body.String()
		if !strings.Contains(ownerBody, "event: email_deleted") {
			t.Error("owner connection should receive email_deleted event")
		}

		// Property 4: Other user's connection should NOT have the event
		otherBody := otherWriter.Body.String()
		if strings.Contains(otherBody, "event: email_deleted") {
			t.Error("other user connection should NOT receive email_deleted event")
		}

		// Property 5: Event should contain all required fields (id, alias_id, deleted_at)
		if ownerReceivedCount > 0 {
			mu.Lock()
			receivedEvent := ownerReceived[0]
			mu.Unlock()

			var receivedDeletedEvent events.EmailDeletedEvent
			if err := json.Unmarshal(receivedEvent.Data, &receivedDeletedEvent); err != nil {
				t.Fatalf("failed to unmarshal event data: %v", err)
			}

			if receivedDeletedEvent.ID != emailID {
				t.Errorf("expected ID %s, got %s", emailID, receivedDeletedEvent.ID)
			}
			if receivedDeletedEvent.AliasID != aliasID {
				t.Errorf("expected AliasID %s, got %s", aliasID, receivedDeletedEvent.AliasID)
			}
			if receivedDeletedEvent.DeletedAt.IsZero() {
				t.Error("DeletedAt should not be zero")
			}
		}

		// Cleanup
		ownerConn.Close()
		otherConn.Close()
	})
}

// Feature: realtime-notifications, Property 7: Alias Events
// *For any* alias creation, the SSE_Server SHALL send alias_created event with id,
// email_address, domain_id, created_at. *For any* alias deletion, the SSE_Server SHALL
// send alias_deleted event with id, email_address, deleted_at, emails_deleted.
// **Validates: Requirements 5.1, 5.2, 5.3, 5.4**
func TestProperty7_AliasEvents(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user IDs
		ownerUserID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "ownerUserID")
		otherUserID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "otherUserID")

		// Ensure different users
		for ownerUserID == otherUserID {
			otherUserID = rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "otherUserID2")
		}

		// Generate random alias event data
		aliasID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "aliasID")
		emailAddress := rapid.StringMatching(`[a-z]{5,10}@[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "emailAddress")
		domainID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "domainID")
		emailsDeleted := rapid.IntRange(0, 100).Draw(t, "emailsDeleted")
		testCreated := rapid.Bool().Draw(t, "testCreated")

		// Setup
		config := DefaultConfig()
		eventStore := events.NewEventStore(100)
		eventBus := events.NewEventBus(eventStore)
		connManager := NewConnectionManager(config)
		router := NewEventRouter(connManager, eventBus)

		// Create connections for both users
		ownerConn, ownerWriter := createTestConnection(ownerUserID)
		otherConn, otherWriter := createTestConnection(otherUserID)
		connManager.AddConnection(ownerUserID, ownerConn)
		connManager.AddConnection(otherUserID, otherConn)

		// Subscribe both users to event bus
		var ownerReceived []events.Event
		var otherReceived []events.Event
		var mu sync.Mutex

		eventBus.Subscribe(ownerUserID, func(event events.Event) {
			mu.Lock()
			ownerReceived = append(ownerReceived, event)
			mu.Unlock()
			connManager.Broadcast(ownerUserID, event)
		})

		eventBus.Subscribe(otherUserID, func(event events.Event) {
			mu.Lock()
			otherReceived = append(otherReceived, event)
			mu.Unlock()
			connManager.Broadcast(otherUserID, event)
		})

		if testCreated {
			// Test alias_created event
			aliasCreatedEvent := events.AliasCreatedEvent{
				ID:           aliasID,
				EmailAddress: emailAddress,
				DomainID:     domainID,
				CreatedAt:    time.Now(),
			}

			err := router.RouteAliasCreatedEvent(ownerUserID, aliasCreatedEvent)
			if err != nil {
				t.Fatalf("failed to route alias created event: %v", err)
			}

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			ownerReceivedCount := len(ownerReceived)
			otherReceivedCount := len(otherReceived)
			mu.Unlock()

			// Property 1: Owner should receive alias_created event
			if ownerReceivedCount != 1 {
				t.Errorf("owner should receive exactly 1 event, got %d", ownerReceivedCount)
			}

			// Property 2: Other user should NOT receive the event
			if otherReceivedCount != 0 {
				t.Errorf("other user should not receive event, got %d", otherReceivedCount)
			}

			// Property 3: Event should be alias_created type
			ownerBody := ownerWriter.Body.String()
			if !strings.Contains(ownerBody, "event: alias_created") {
				t.Error("owner connection should receive alias_created event")
			}

			// Property 4: Other user should NOT receive
			otherBody := otherWriter.Body.String()
			if strings.Contains(otherBody, "event: alias_created") {
				t.Error("other user connection should NOT receive alias_created event")
			}

			// Property 5: Event should contain all required fields
			if ownerReceivedCount > 0 {
				mu.Lock()
				receivedEvent := ownerReceived[0]
				mu.Unlock()

				var receivedAliasEvent events.AliasCreatedEvent
				if err := json.Unmarshal(receivedEvent.Data, &receivedAliasEvent); err != nil {
					t.Fatalf("failed to unmarshal event data: %v", err)
				}

				if receivedAliasEvent.ID != aliasID {
					t.Errorf("expected ID %s, got %s", aliasID, receivedAliasEvent.ID)
				}
				if receivedAliasEvent.EmailAddress != emailAddress {
					t.Errorf("expected EmailAddress %s, got %s", emailAddress, receivedAliasEvent.EmailAddress)
				}
				if receivedAliasEvent.DomainID != domainID {
					t.Errorf("expected DomainID %s, got %s", domainID, receivedAliasEvent.DomainID)
				}
				if receivedAliasEvent.CreatedAt.IsZero() {
					t.Error("CreatedAt should not be zero")
				}
			}
		} else {
			// Test alias_deleted event
			aliasDeletedEvent := events.AliasDeletedEvent{
				ID:            aliasID,
				EmailAddress:  emailAddress,
				DeletedAt:     time.Now(),
				EmailsDeleted: emailsDeleted,
			}

			err := router.RouteAliasDeletedEvent(ownerUserID, aliasDeletedEvent)
			if err != nil {
				t.Fatalf("failed to route alias deleted event: %v", err)
			}

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			ownerReceivedCount := len(ownerReceived)
			otherReceivedCount := len(otherReceived)
			mu.Unlock()

			// Property 1: Owner should receive alias_deleted event
			if ownerReceivedCount != 1 {
				t.Errorf("owner should receive exactly 1 event, got %d", ownerReceivedCount)
			}

			// Property 2: Other user should NOT receive the event
			if otherReceivedCount != 0 {
				t.Errorf("other user should not receive event, got %d", otherReceivedCount)
			}

			// Property 3: Event should be alias_deleted type
			ownerBody := ownerWriter.Body.String()
			if !strings.Contains(ownerBody, "event: alias_deleted") {
				t.Error("owner connection should receive alias_deleted event")
			}

			// Property 4: Other user should NOT receive
			otherBody := otherWriter.Body.String()
			if strings.Contains(otherBody, "event: alias_deleted") {
				t.Error("other user connection should NOT receive alias_deleted event")
			}

			// Property 5: Event should contain all required fields
			if ownerReceivedCount > 0 {
				mu.Lock()
				receivedEvent := ownerReceived[0]
				mu.Unlock()

				var receivedAliasEvent events.AliasDeletedEvent
				if err := json.Unmarshal(receivedEvent.Data, &receivedAliasEvent); err != nil {
					t.Fatalf("failed to unmarshal event data: %v", err)
				}

				if receivedAliasEvent.ID != aliasID {
					t.Errorf("expected ID %s, got %s", aliasID, receivedAliasEvent.ID)
				}
				if receivedAliasEvent.EmailAddress != emailAddress {
					t.Errorf("expected EmailAddress %s, got %s", emailAddress, receivedAliasEvent.EmailAddress)
				}
				if receivedAliasEvent.DeletedAt.IsZero() {
					t.Error("DeletedAt should not be zero")
				}
				if receivedAliasEvent.EmailsDeleted != emailsDeleted {
					t.Errorf("expected EmailsDeleted %d, got %d", emailsDeleted, receivedAliasEvent.EmailsDeleted)
				}
			}
		}

		// Cleanup
		ownerConn.Close()
		otherConn.Close()
	})
}


// Feature: realtime-notifications, Property 8: Domain Events
// *For any* domain verification, the SSE_Server SHALL send domain_verified event with id,
// domain_name, verified_at, ssl_status. *For any* domain deletion, the SSE_Server SHALL
// send domain_deleted event with id, domain_name, deleted_at, aliases_deleted, emails_deleted.
// **Validates: Requirements 6.1, 6.2, 6.3, 6.4**
func TestProperty8_DomainEvents(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random user IDs
		ownerUserID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "ownerUserID")
		otherUserID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "otherUserID")

		// Ensure different users
		for ownerUserID == otherUserID {
			otherUserID = rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "otherUserID2")
		}

		// Generate random domain event data
		domainID := rapid.StringMatching(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`).Draw(t, "domainID")
		domainName := rapid.StringMatching(`[a-z]{5,10}\.[a-z]{2,3}`).Draw(t, "domainName")
		sslStatus := rapid.SampledFrom([]string{"pending", "active", "expired", "none"}).Draw(t, "sslStatus")
		aliasesDeleted := rapid.IntRange(0, 50).Draw(t, "aliasesDeleted")
		emailsDeleted := rapid.IntRange(0, 500).Draw(t, "emailsDeleted")
		testVerified := rapid.Bool().Draw(t, "testVerified")

		// Setup
		config := DefaultConfig()
		eventStore := events.NewEventStore(100)
		eventBus := events.NewEventBus(eventStore)
		connManager := NewConnectionManager(config)
		router := NewEventRouter(connManager, eventBus)

		// Create connections for both users
		ownerConn, ownerWriter := createTestConnection(ownerUserID)
		otherConn, otherWriter := createTestConnection(otherUserID)
		connManager.AddConnection(ownerUserID, ownerConn)
		connManager.AddConnection(otherUserID, otherConn)

		// Subscribe both users to event bus
		var ownerReceived []events.Event
		var otherReceived []events.Event
		var mu sync.Mutex

		eventBus.Subscribe(ownerUserID, func(event events.Event) {
			mu.Lock()
			ownerReceived = append(ownerReceived, event)
			mu.Unlock()
			connManager.Broadcast(ownerUserID, event)
		})

		eventBus.Subscribe(otherUserID, func(event events.Event) {
			mu.Lock()
			otherReceived = append(otherReceived, event)
			mu.Unlock()
			connManager.Broadcast(otherUserID, event)
		})

		if testVerified {
			// Test domain_verified event
			domainVerifiedEvent := events.DomainVerifiedEvent{
				ID:         domainID,
				DomainName: domainName,
				VerifiedAt: time.Now(),
				SSLStatus:  sslStatus,
			}

			err := router.RouteDomainVerifiedEvent(ownerUserID, domainVerifiedEvent)
			if err != nil {
				t.Fatalf("failed to route domain verified event: %v", err)
			}

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			ownerReceivedCount := len(ownerReceived)
			otherReceivedCount := len(otherReceived)
			mu.Unlock()

			// Property 1: Owner should receive domain_verified event
			if ownerReceivedCount != 1 {
				t.Errorf("owner should receive exactly 1 event, got %d", ownerReceivedCount)
			}

			// Property 2: Other user should NOT receive the event
			if otherReceivedCount != 0 {
				t.Errorf("other user should not receive event, got %d", otherReceivedCount)
			}

			// Property 3: Event should be domain_verified type
			ownerBody := ownerWriter.Body.String()
			if !strings.Contains(ownerBody, "event: domain_verified") {
				t.Error("owner connection should receive domain_verified event")
			}

			// Property 4: Other user should NOT receive
			otherBody := otherWriter.Body.String()
			if strings.Contains(otherBody, "event: domain_verified") {
				t.Error("other user connection should NOT receive domain_verified event")
			}

			// Property 5: Event should contain all required fields
			if ownerReceivedCount > 0 {
				mu.Lock()
				receivedEvent := ownerReceived[0]
				mu.Unlock()

				var receivedDomainEvent events.DomainVerifiedEvent
				if err := json.Unmarshal(receivedEvent.Data, &receivedDomainEvent); err != nil {
					t.Fatalf("failed to unmarshal event data: %v", err)
				}

				if receivedDomainEvent.ID != domainID {
					t.Errorf("expected ID %s, got %s", domainID, receivedDomainEvent.ID)
				}
				if receivedDomainEvent.DomainName != domainName {
					t.Errorf("expected DomainName %s, got %s", domainName, receivedDomainEvent.DomainName)
				}
				if receivedDomainEvent.VerifiedAt.IsZero() {
					t.Error("VerifiedAt should not be zero")
				}
				if receivedDomainEvent.SSLStatus != sslStatus {
					t.Errorf("expected SSLStatus %s, got %s", sslStatus, receivedDomainEvent.SSLStatus)
				}
			}
		} else {
			// Test domain_deleted event
			domainDeletedEvent := events.DomainDeletedEvent{
				ID:             domainID,
				DomainName:     domainName,
				DeletedAt:      time.Now(),
				AliasesDeleted: aliasesDeleted,
				EmailsDeleted:  emailsDeleted,
			}

			err := router.RouteDomainDeletedEvent(ownerUserID, domainDeletedEvent)
			if err != nil {
				t.Fatalf("failed to route domain deleted event: %v", err)
			}

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			ownerReceivedCount := len(ownerReceived)
			otherReceivedCount := len(otherReceived)
			mu.Unlock()

			// Property 1: Owner should receive domain_deleted event
			if ownerReceivedCount != 1 {
				t.Errorf("owner should receive exactly 1 event, got %d", ownerReceivedCount)
			}

			// Property 2: Other user should NOT receive the event
			if otherReceivedCount != 0 {
				t.Errorf("other user should not receive event, got %d", otherReceivedCount)
			}

			// Property 3: Event should be domain_deleted type
			ownerBody := ownerWriter.Body.String()
			if !strings.Contains(ownerBody, "event: domain_deleted") {
				t.Error("owner connection should receive domain_deleted event")
			}

			// Property 4: Other user should NOT receive
			otherBody := otherWriter.Body.String()
			if strings.Contains(otherBody, "event: domain_deleted") {
				t.Error("other user connection should NOT receive domain_deleted event")
			}

			// Property 5: Event should contain all required fields
			if ownerReceivedCount > 0 {
				mu.Lock()
				receivedEvent := ownerReceived[0]
				mu.Unlock()

				var receivedDomainEvent events.DomainDeletedEvent
				if err := json.Unmarshal(receivedEvent.Data, &receivedDomainEvent); err != nil {
					t.Fatalf("failed to unmarshal event data: %v", err)
				}

				if receivedDomainEvent.ID != domainID {
					t.Errorf("expected ID %s, got %s", domainID, receivedDomainEvent.ID)
				}
				if receivedDomainEvent.DomainName != domainName {
					t.Errorf("expected DomainName %s, got %s", domainName, receivedDomainEvent.DomainName)
				}
				if receivedDomainEvent.DeletedAt.IsZero() {
					t.Error("DeletedAt should not be zero")
				}
				if receivedDomainEvent.AliasesDeleted != aliasesDeleted {
					t.Errorf("expected AliasesDeleted %d, got %d", aliasesDeleted, receivedDomainEvent.AliasesDeleted)
				}
				if receivedDomainEvent.EmailsDeleted != emailsDeleted {
					t.Errorf("expected EmailsDeleted %d, got %d", emailsDeleted, receivedDomainEvent.EmailsDeleted)
				}
			}
		}

		// Cleanup
		ownerConn.Close()
		otherConn.Close()
	})
}

// Unit test for EventRouter basic functionality
func TestEventRouter_BasicFunctionality(t *testing.T) {
	config := DefaultConfig()
	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	router := NewEventRouter(connManager, eventBus)

	userID := uuid.New().String()

	// Test HasActiveConnections with no connections
	if router.HasActiveConnections(userID) {
		t.Error("should have no active connections initially")
	}

	// Test GetUserConnectionCount with no connections
	if router.GetUserConnectionCount(userID) != 0 {
		t.Error("should have 0 connections initially")
	}

	// Add a connection
	conn, _ := createTestConnection(userID)
	connManager.AddConnection(userID, conn)

	// Test HasActiveConnections with connection
	if !router.HasActiveConnections(userID) {
		t.Error("should have active connections after adding one")
	}

	// Test GetUserConnectionCount with connection
	if router.GetUserConnectionCount(userID) != 1 {
		t.Error("should have 1 connection after adding one")
	}

	// Cleanup
	conn.Close()
}

// Test multiple users receiving events concurrently
func TestEventRouter_ConcurrentMultipleUsers(t *testing.T) {
	config := DefaultConfig()
	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	router := NewEventRouter(connManager, eventBus)

	numUsers := 5
	users := make([]string, numUsers)
	connections := make([]*Connection, numUsers)
	writers := make([]*mockResponseWriter, numUsers)
	receivedEvents := make([][]events.Event, numUsers)
	var mu sync.Mutex

	// Setup users and connections
	for i := 0; i < numUsers; i++ {
		users[i] = uuid.New().String()
		connections[i], writers[i] = createTestConnection(users[i])
		connManager.AddConnection(users[i], connections[i])
		receivedEvents[i] = make([]events.Event, 0)

		idx := i
		eventBus.Subscribe(users[i], func(event events.Event) {
			mu.Lock()
			receivedEvents[idx] = append(receivedEvents[idx], event)
			mu.Unlock()
			connManager.Broadcast(users[idx], event)
		})
	}

	// Send events to each user concurrently
	var wg sync.WaitGroup
	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			emailEvent := events.NewEmailEvent{
				ID:          uuid.New().String(),
				AliasID:     uuid.New().String(),
				AliasEmail:  "test@example.com",
				FromAddress: "sender@example.com",
				PreviewText: "Test email",
				ReceivedAt:  time.Now(),
			}
			router.RouteNewEmailEvent(users[idx], emailEvent)
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	// Verify each user received exactly one event
	mu.Lock()
	for i := 0; i < numUsers; i++ {
		if len(receivedEvents[i]) != 1 {
			t.Errorf("user %d should receive exactly 1 event, got %d", i, len(receivedEvents[i]))
		}
	}
	mu.Unlock()

	// Cleanup
	for i := 0; i < numUsers; i++ {
		connections[i].Close()
	}
}

// Test event routing with no subscribers
func TestEventRouter_NoSubscribers(t *testing.T) {
	config := DefaultConfig()
	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	router := NewEventRouter(connManager, eventBus)

	userID := uuid.New().String()

	// Route event with no subscribers - should not error
	emailEvent := events.NewEmailEvent{
		ID:          uuid.New().String(),
		AliasID:     uuid.New().String(),
		AliasEmail:  "test@example.com",
		FromAddress: "sender@example.com",
		PreviewText: "Test email",
		ReceivedAt:  time.Now(),
	}

	err := router.RouteNewEmailEvent(userID, emailEvent)
	if err != nil {
		t.Errorf("routing event with no subscribers should not error: %v", err)
	}
}

// Test BroadcastToUser
func TestEventRouter_BroadcastToUser(t *testing.T) {
	config := DefaultConfig()
	eventStore := events.NewEventStore(100)
	eventBus := events.NewEventBus(eventStore)
	connManager := NewConnectionManager(config)
	router := NewEventRouter(connManager, eventBus)

	userID := uuid.New().String()
	conn, writer := createTestConnection(userID)
	connManager.AddConnection(userID, conn)

	// Create event
	event := events.Event{
		ID:        uuid.New().String(),
		Type:      events.EventTypeHeartbeat,
		UserID:    userID,
		Data:      []byte(`{"timestamp":"2024-01-01T00:00:00Z"}`),
		Timestamp: time.Now(),
	}

	// Broadcast directly
	err := router.BroadcastToUser(userID, event)
	if err != nil {
		t.Errorf("broadcast should not error: %v", err)
	}

	// Verify event was written
	body := writer.Body.String()
	if !strings.Contains(body, "event: heartbeat") {
		t.Error("event should be written to connection")
	}

	// Cleanup
	conn.Close()
}
