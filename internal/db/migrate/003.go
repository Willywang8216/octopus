package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 3,
		Up:      lowercaseModelNames,
	})
}

// 003: lowercase model identifiers across channels.model, channels.custom_model,
// group_items.model_name and llm_infos.name. Upstream APIs return mixed case
// (e.g. "GPT-4" vs "gpt-4") which made each sync produce spurious diff churn.
// FetchModels now lowercases at the boundary; this migration aligns the
// historical rows so the next sync produces zero diffs for unchanged catalogues.
func lowercaseModelNames(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	return db.Transaction(func(tx *gorm.DB) error {
		// channels.model and channels.custom_model are free comma-joined strings,
		// safe to lowercase in place. No unique constraints to worry about.
		if err := tx.Exec("UPDATE channels SET model = LOWER(model) WHERE model <> LOWER(model)").Error; err != nil {
			return fmt.Errorf("lowercase channels.model: %w", err)
		}
		if err := tx.Exec("UPDATE channels SET custom_model = LOWER(custom_model) WHERE custom_model <> LOWER(custom_model)").Error; err != nil {
			return fmt.Errorf("lowercase channels.custom_model: %w", err)
		}

		// group_items has a unique index on (group_id, channel_id, model_name).
		// Lowercasing in place can violate that index when two rows differ only
		// by case. Delete the duplicates (keeping the smallest id) first, then
		// lowercase the survivors.
		if err := tx.Exec(`
			DELETE FROM group_items
			WHERE id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (
						PARTITION BY group_id, channel_id, LOWER(model_name)
						ORDER BY id
					) AS rn
					FROM group_items
				) ranked
				WHERE rn > 1
			)
		`).Error; err != nil {
			return fmt.Errorf("dedupe group_items by lowercase model_name: %w", err)
		}
		if err := tx.Exec("UPDATE group_items SET model_name = LOWER(model_name) WHERE model_name <> LOWER(model_name)").Error; err != nil {
			return fmt.Errorf("lowercase group_items.model_name: %w", err)
		}

		// llm_infos.name is the primary key. Merge any case-collision pairs
		// (preferring the row that already has price data) before lowercasing.
		// We do this by deleting duplicates that have no price set, keeping
		// the one with non-zero pricing if any.
		if err := tx.Exec(`
			DELETE FROM llm_infos
			WHERE name IN (
				SELECT name FROM (
					SELECT name, ROW_NUMBER() OVER (
						PARTITION BY LOWER(name)
						ORDER BY
							CASE WHEN input <> 0 OR output <> 0 OR cache_read <> 0 OR cache_write <> 0 THEN 0 ELSE 1 END,
							name
					) AS rn
					FROM llm_infos
				) ranked
				WHERE rn > 1
			)
		`).Error; err != nil {
			return fmt.Errorf("dedupe llm_infos by lowercase name: %w", err)
		}
		if err := tx.Exec("UPDATE llm_infos SET name = LOWER(name) WHERE name <> LOWER(name)").Error; err != nil {
			return fmt.Errorf("lowercase llm_infos.name: %w", err)
		}

		return nil
	})
}
