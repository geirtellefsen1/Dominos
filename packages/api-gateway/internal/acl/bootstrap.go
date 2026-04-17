package acl

import (
	"context"
	"fmt"
	"log/slog"
)

// Bootstrap finds or creates the "dominion" store and writes the current
// model. The (store_id, model_id) pair is cached on the client.
//
// Safe to call on every boot: when FGA's datastore is persistent the
// store is found, and writing the same model again simply creates a new
// authorization_model_id which becomes the current one.
func Bootstrap(ctx context.Context, c *Client, storeName string) error {
	if storeName == "" {
		storeName = "dominion"
	}
	storeID, err := c.FindStoreByName(ctx, storeName)
	if err != nil {
		return fmt.Errorf("list fga stores: %w", err)
	}
	if storeID == "" {
		slog.Info("creating fga store", "name", storeName)
		storeID, err = c.CreateStore(ctx, storeName)
		if err != nil {
			return fmt.Errorf("create fga store: %w", err)
		}
	}
	modelID, err := c.WriteModel(ctx, storeID, DominionModelJSON)
	if err != nil {
		return fmt.Errorf("write fga model: %w", err)
	}
	c.setIDs(storeID, modelID)
	slog.Info("fga bootstrapped", "store_id", storeID, "model_id", modelID)
	return nil
}
