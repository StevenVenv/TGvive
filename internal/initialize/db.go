package initialize

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqliteDriver "github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"my-go-server/internal/global"
	"my-go-server/internal/model"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const mysqlErrUnknownDatabase uint16 = 1049

func InitDB() error {
	logMode := logger.Warn
	if global.Config.Server.Mode == "debug" {
		logMode = logger.Info
	}

	driver := strings.ToLower(strings.TrimSpace(global.Config.Database.Driver))
	if driver == "" {
		driver = "mysql"
	}

	switch driver {
	case "sqlite", "sqlite3", "local":
		return initSQLite(logMode)
	case "mysql":
		return initMySQL(logMode)
	default:
		return fmt.Errorf("unsupported database.driver %q (use mysql or sqlite)", global.Config.Database.Driver)
	}
}

func initMySQL(logMode logger.LogLevel) error {
	c := global.Config.MySQL
	if strings.TrimSpace(c.Host) == "" || c.Port <= 0 || strings.TrimSpace(c.User) == "" || strings.TrimSpace(c.DBName) == "" {
		return errors.New("mysql config is incomplete (host/port/user/dbname required)")
	}

	db, err := openMySQL(c, logMode)
	if err != nil && isUnknownDatabaseErr(err) {
		if cErr := createDatabase(c, logMode); cErr != nil {
			return fmt.Errorf("database %q does not exist and auto-create failed: %w", strings.TrimSpace(c.DBName), cErr)
		}
		db, err = openMySQL(c, logMode)
	}
	if err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}

	global.DB = db
	if err := migrateCoreTables(global.DB); err != nil {
		return err
	}

	sqlDB, err := global.DB.DB()
	if err == nil && sqlDB != nil {
		tuneMySQLPool(sqlDB)
	}
	return nil
}

func initSQLite(logMode logger.LogLevel) error {
	path := strings.TrimSpace(global.Config.Database.SQLitePath)
	if path == "" {
		path = "./data/tgvive.sqlite"
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create sqlite directory failed: %w", err)
		}
	}

	db, err := gorm.Open(sqliteDriver.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logMode),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return fmt.Errorf("sqlite open failed: %w", err)
	}

	_ = db.Exec("PRAGMA journal_mode=WAL;").Error
	_ = db.Exec("PRAGMA synchronous=NORMAL;").Error
	_ = db.Exec("PRAGMA busy_timeout=5000;").Error
	_ = db.Exec("PRAGMA foreign_keys=ON;").Error

	global.DB = db
	if err := migrateCoreTables(global.DB); err != nil {
		return err
	}

	if sqlDB, err := global.DB.DB(); err == nil && sqlDB != nil {
		tuneSQLitePool(sqlDB)
	}
	return nil
}

func migrateCoreTables(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.User{}, &model.Task{}, &model.Strategy{}, &model.KeywordProfile{}, &model.MessageMapping{}); err != nil {
		return fmt.Errorf("auto migrate failed: %w", err)
	}
	if err := ensureCoreTables(db); err != nil {
		return err
	}
	return nil
}

func openMySQL(c global.MySQLConfig, logMode logger.LogLevel) (*gorm.DB, error) {
	dsn := makeMySQLDSN(c, strings.TrimSpace(c.DBName))
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logMode),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return db, nil
}

func createDatabase(c global.MySQLConfig, logMode logger.LogLevel) error {
	dbName := strings.TrimSpace(c.DBName)
	if dbName == "" {
		return errors.New("database name is empty")
	}

	dsn := makeMySQLDSN(c, "")
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logMode),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err == nil && sqlDB != nil {
		defer sqlDB.Close()
	}

	stmt := fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		quoteMySQLIdentifier(dbName),
	)
	return db.Exec(stmt).Error
}

func ensureCoreTables(db *gorm.DB) error {
	if db == nil {
		return errors.New("db is nil")
	}

	required := []any{
		&model.User{},
		&model.Task{},
		&model.Strategy{},
		&model.KeywordProfile{},
		&model.MessageMapping{},
	}
	for _, m := range required {
		if !db.Migrator().HasTable(m) {
			return fmt.Errorf("schema initialization failed: table for %T was not created", m)
		}
	}
	return nil
}

func makeMySQLDSN(c global.MySQLConfig, dbName string) string {
	base := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", c.User, c.Password, c.Host, c.Port, strings.TrimSpace(dbName))
	cfg := strings.TrimSpace(c.Config)
	if cfg == "" {
		return base
	}
	return base + "?" + cfg
}

func isUnknownDatabaseErr(err error) bool {
	if err == nil {
		return false
	}
	var me *mysqlDriver.MySQLError
	if errors.As(err, &me) && me != nil && me.Number == mysqlErrUnknownDatabase {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "unknown database")
}

func quoteMySQLIdentifier(name string) string {
	escaped := strings.ReplaceAll(strings.TrimSpace(name), "`", "``")
	return "`" + escaped + "`"
}

func tuneMySQLPool(db *sql.DB) {
	if db == nil {
		return
	}

	// Conservative defaults; can be overridden by upstream if needed.
	db.SetMaxOpenConns(30)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(2 * time.Hour)
	db.SetConnMaxIdleTime(15 * time.Minute)
}

func tuneSQLitePool(db *sql.DB) {
	if db == nil {
		return
	}

	// SQLite is file-backed, so keep writes serialized to avoid lock churn.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
}
