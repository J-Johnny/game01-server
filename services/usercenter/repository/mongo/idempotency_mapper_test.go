package mongo

import (
	"errors"
	"testing"
	"time"

	"server/services/usercenter/domain"
)

func TestIdempotencyMapperCopiesResponse(t *testing.T) {
	now := time.Now().UTC()
	record := &domain.IdempotencyRecord{Key: "key-1", Operation: "guest_login", RequestHash: "hash", Response: []byte("response"), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	document, err := idempotencyDocumentFromDomain(record)
	if err != nil {
		t.Fatalf("record to document: %v", err)
	}
	document.Response[0] = 'x'
	if record.Response[0] == 'x' {
		t.Fatal("record and document share response storage")
	}
	decoded := idempotencyDomainFromDocument(document)
	decoded.Response[0] = 'y'
	if document.Response[0] == 'y' {
		t.Fatal("decoded record and document share response storage")
	}
}

func TestIdempotencyMapperRejectsExpiredRecord(t *testing.T) {
	if _, err := idempotencyDocumentFromDomain(&domain.IdempotencyRecord{Key: "key-1", Operation: "login", RequestHash: "hash", ExpiresAt: time.Now().UTC().Add(-time.Hour)}); !errors.Is(err, domain.ErrInvalidIdempotency) {
		t.Fatalf("expired record error = %v", err)
	}
}
