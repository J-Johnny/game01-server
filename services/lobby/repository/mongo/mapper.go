package mongo

import "server/services/lobby/domain"

func playerDocumentFromDomain(player domain.Player) (playerDocument, error) {
	if err := player.Validate(); err != nil {
		return playerDocument{}, err
	}
	return playerDocument{PlayerID: player.ID, AccountID: player.AccountID, Nickname: player.Nickname, Avatar: player.Avatar, Region: player.Region, IsDefault: player.IsDefault, ProfileVersion: player.ProfileVersion, CreatedAt: player.CreatedAt, UpdatedAt: player.UpdatedAt}, nil
}

func playerDomainFromDocument(document playerDocument) domain.Player {
	return domain.Player{ID: document.PlayerID, AccountID: document.AccountID, Nickname: document.Nickname, Avatar: document.Avatar, Region: document.Region, IsDefault: document.IsDefault, ProfileVersion: document.ProfileVersion, CreatedAt: document.CreatedAt, UpdatedAt: document.UpdatedAt}
}

func assetsDocumentFromDomain(assets domain.Assets) assetsDocument {
	return assetsDocument{PlayerID: assets.PlayerID, Currency: cloneCurrency(assets.Currency), AssetVersion: assets.AssetVersion, UpdatedAt: assets.UpdatedAt}
}

func assetsDomainFromDocument(document assetsDocument) domain.Assets {
	return domain.Assets{PlayerID: document.PlayerID, Currency: cloneCurrency(document.Currency), AssetVersion: document.AssetVersion, UpdatedAt: document.UpdatedAt}
}

func ledgerDocumentFromDomain(settlement domain.Settlement) (ledgerDocument, error) {
	if err := settlement.Validate(); err != nil {
		return ledgerDocument{}, err
	}
	return ledgerDocument{SettlementID: settlement.ID, PlayerID: settlement.PlayerID, AssetType: settlement.AssetType, Delta: settlement.Delta, Reason: settlement.Reason, Source: settlement.Source, CreatedAt: settlement.CreatedAt}, nil
}

func settlementDomainFromDocument(document ledgerDocument) domain.Settlement {
	return domain.Settlement{ID: document.SettlementID, PlayerID: document.PlayerID, AssetType: document.AssetType, Delta: document.Delta, Reason: document.Reason, Source: document.Source, CreatedAt: document.CreatedAt}
}

func snapshotDocumentFromDomain(snapshot domain.Snapshot) snapshotDocument {
	return snapshotDocument{PlayerID: snapshot.Player.ID, AccountID: snapshot.Player.AccountID, Nickname: snapshot.Player.Nickname, Region: snapshot.Player.Region, ProfileVersion: snapshot.Player.ProfileVersion, AssetVersion: snapshot.Assets.AssetVersion, Currency: cloneCurrency(snapshot.Assets.Currency), SchemaVersion: snapshot.SchemaVersion, CreatedAt: snapshot.CreatedAt}
}

func snapshotDomainFromDocument(document snapshotDocument) domain.Snapshot {
	return domain.Snapshot{Player: domain.Player{ID: document.PlayerID, AccountID: document.AccountID, Nickname: document.Nickname, Region: document.Region, ProfileVersion: document.ProfileVersion}, Assets: domain.Assets{PlayerID: document.PlayerID, Currency: cloneCurrency(document.Currency), AssetVersion: document.AssetVersion}, SchemaVersion: document.SchemaVersion, CreatedAt: document.CreatedAt}
}

func cloneCurrency(currency map[string]int64) map[string]int64 {
	cloned := make(map[string]int64, len(currency))
	for assetType, balance := range currency {
		cloned[assetType] = balance
	}
	return cloned
}
