package core

import (
	"context"
	"errors"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var ErrSettingNotFound = errors.New("setting not found")

// UpdateSetting upserts a setting and returns it before and after the write,
// both read in the transaction that writes. before is nil for a new key.
func UpdateSetting(ctx context.Context, db store.Store, s *models.SystemSetting) (before, after *models.SystemSetting, err error) {
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		var err error
		before, err = tx.GetSettingForUpdate(ctx, s.Key)
		if errors.Is(err, models.ErrNotFound) {
			before, err = nil, nil
		}
		if err != nil {
			return err
		}
		if err := tx.UpsertSetting(ctx, s.Key, s.Value, s.ValueType, s.Category, s.Description, s.IsSensitive); err != nil {
			return err
		}
		after, err = tx.GetSettingForUpdate(ctx, s.Key)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// DeleteSetting deletes a setting and returns it as read in the transaction
// that deletes it.
func DeleteSetting(ctx context.Context, db store.Store, key string) (*models.SystemSetting, error) {
	var deleted *models.SystemSetting
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		var err error
		if deleted, err = tx.GetSettingForUpdate(ctx, key); err != nil {
			if errors.Is(err, models.ErrNotFound) {
				return ErrSettingNotFound
			}
			return err
		}
		return tx.DeleteSetting(ctx, key)
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}
