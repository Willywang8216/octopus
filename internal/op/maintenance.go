package op

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func RelayLogPurgeAll(ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Where("1 = 1").Delete(&model.RelayLog{}).Error
}

func RelayLogPurgeBefore(ctx context.Context, cutoff time.Time) error {
	return db.GetDB().WithContext(ctx).Where("time < ?", cutoff.Unix()).Delete(&model.RelayLog{}).Error
}

func Vacuum(ctx context.Context) error {
	if db.GetDB().Dialector == nil || db.GetDB().Dialector.Name() != "sqlite" {
		return nil
	}
	// Ensure deleted rows can be reclaimed and WAL doesn't keep growing.
	if err := db.GetDB().WithContext(ctx).Exec("PRAGMA wal_checkpoint(TRUNCATE);").Error; err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	if err := db.GetDB().WithContext(ctx).Exec("VACUUM;").Error; err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}
