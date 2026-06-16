package migrate

import "gorm.io/gorm"

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 3,
		Up:      noopChannelKeyModelStatus,
	})
}

// 003: claim version 3 for the channel_key_model_status table. AutoMigrate
// creates the table itself; this placeholder reserves the version number so
// future schema tweaks (renames, backfills) can extend it.
func noopChannelKeyModelStatus(_ *gorm.DB) error {
	return nil
}
