package localdb

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const DefaultRootDir = "./data/tasks"

type Manager struct {
	root string

	mu    sync.Mutex
	conns map[uint]*conn
}

type conn struct {
	db   *gorm.DB
	sql  *sql.DB
	path string
}

func NewManager(root string) *Manager {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultRootDir
	}
	return &Manager{
		root:  root,
		conns: make(map[uint]*conn),
	}
}

var Default = NewManager(DefaultRootDir)

func (m *Manager) pathForTask(taskID uint) (string, error) {
	if taskID == 0 {
		return "", errors.New("task id is required")
	}
	root := strings.TrimSpace(m.root)
	if root == "" {
		root = DefaultRootDir
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(root, fmt.Sprintf("task_%d.sqlite", taskID)), nil
}

func (m *Manager) Get(taskID uint) *gorm.DB {
	if m == nil || taskID == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.conns[taskID]; c != nil {
		return c.db
	}
	return nil
}

func (m *Manager) Open(taskID uint) (*gorm.DB, string, error) {
	if m == nil {
		return nil, "", errors.New("localdb manager is nil")
	}
	if taskID == 0 {
		return nil, "", errors.New("task id is required")
	}

	m.mu.Lock()
	if c := m.conns[taskID]; c != nil && c.db != nil {
		db := c.db
		path := c.path
		m.mu.Unlock()
		return db, path, nil
	}
	m.mu.Unlock()

	path, err := m.pathForTask(taskID)
	if err != nil {
		return nil, "", err
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return nil, "", err
	}

	// PRAGMA tuning (WAL, lower sync, etc).
	_ = db.Exec("PRAGMA journal_mode=WAL;").Error
	_ = db.Exec("PRAGMA synchronous=NORMAL;").Error
	_ = db.Exec("PRAGMA busy_timeout=5000;").Error
	_ = db.Exec("PRAGMA temp_store=MEMORY;").Error
	_ = db.Exec("PRAGMA foreign_keys=ON;").Error

	if err := ensureSchema(db); err != nil {
		return nil, "", err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	m.mu.Lock()
	m.conns[taskID] = &conn{db: db, sql: sqlDB, path: path}
	m.mu.Unlock()

	return db, path, nil
}

type sqliteColumn struct {
	Name string `gorm:"column:name"`
}

func ensureSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("db is nil")
	}

	// Always migrate mapping table (non-breaking).
	if err := db.AutoMigrate(&LocalMapping{}); err != nil {
		return err
	}
	if err := db.AutoMigrate(&LocalMsgMapping{}); err != nil {
		return err
	}

	// local_comment had a legacy schema; rebuild it once when detected.
	{
		var cols []sqliteColumn
		_ = db.Raw("PRAGMA table_info(local_comment)").Scan(&cols).Error

		hasLegacy := false
		hasSourcePostID := false
		hasCommentMsgID := false
		for _, c := range cols {
			switch strings.ToLower(strings.TrimSpace(c.Name)) {
			case "msg_id", "reply_to_root_id", "status", "attempts", "next_attempt_at":
				hasLegacy = true
			case "source_post_id":
				hasSourcePostID = true
			case "comment_msg_id":
				hasCommentMsgID = true
			}
		}
		// If legacy columns exist, drop and recreate the table to avoid NOT NULL constraint issues.
		// This is task-scoped cache, safe to rebuild.
		if hasLegacy {
			if err := db.Migrator().DropTable("local_comment"); err != nil {
				return err
			}
		} else if len(cols) > 0 && (!hasSourcePostID || !hasCommentMsgID) {
			// Unexpected schema: rebuild to keep the codepath simple and reliable.
			if err := db.Migrator().DropTable("local_comment"); err != nil {
				return err
			}
		}
	}

	return db.AutoMigrate(&LocalComment{})
}

func (m *Manager) Close(taskID uint) error {
	if m == nil || taskID == 0 {
		return nil
	}

	m.mu.Lock()
	c := m.conns[taskID]
	delete(m.conns, taskID)
	m.mu.Unlock()

	if c == nil || c.sql == nil {
		return nil
	}
	return c.sql.Close()
}

func (m *Manager) Destroy(taskID uint) error {
	if m == nil {
		return nil
	}
	if taskID == 0 {
		return errors.New("task id is required")
	}

	path, err := m.pathForTask(taskID)
	if err != nil {
		return err
	}

	_ = m.Close(taskID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
