package mongo

import (
	"testing"
	"time"

	"server/services/usercenter/domain"
)

func TestAccountDocumentUsesArrayForEmptyPlayerLinks(t *testing.T) {
	document, err := accountDocumentFromDomain(&domain.Account{ID: "account-1", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatalf("map account: %v", err)
	}
	if document.PlayerIDs == nil {
		t.Fatal("empty player links must be encoded as an empty BSON array")
	}
}
