package localdb

import (
	"errors"

	"gorm.io/gorm"
)

// AutoMigrateTaskDB initializes or upgrades task-scoped SQLite schema.
// This DB is a per-task cache and can be safely rebuilt when incompatible.
func AutoMigrateTaskDB(db *gorm.DB) error {
	if db == nil {
		return errors.New("db is nil")
	}
	return db.AutoMigrate(
		&RootMapping{},
		&CommentQueue{},
		&LocalMsgMapping{},
	)
}
