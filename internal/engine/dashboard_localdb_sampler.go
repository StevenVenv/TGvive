package engine

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	"my-go-server/internal/engine/localdb"
	"my-go-server/internal/global"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// StartDashboardLocalDBSampler periodically scans task-scoped SQLite files under data/tasks
// and updates dashboard stats (comment queue pending/success/fail).
func StartDashboardLocalDBSampler(ctx context.Context) {
	if ctx == nil {
		return
	}
	go dashboardLocalDBSampler(ctx)
}

type commentQueueTotals struct {
	pending uint64
	success uint64
	fail    uint64
}

func dashboardLocalDBSampler(ctx context.Context) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()

	for {
		totals := sampleCommentQueueTotals()
		global.Stats.SetCommentQueueStats(totals.pending, totals.success, totals.fail)

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func sampleCommentQueueTotals() commentQueueTotals {
	root := localdb.DefaultRootDir
	if _, err := os.Stat(root); err != nil {
		return commentQueueTotals{}
	}

	paths, _ := filepath.Glob(filepath.Join(root, "task_*.sqlite"))
	if len(paths) == 0 {
		return commentQueueTotals{}
	}
	sort.Strings(paths)

	var totals commentQueueTotals

	for _, path := range paths {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil || db == nil {
			continue
		}
		sqlDB, err := db.DB()
		if err != nil || sqlDB == nil {
			continue
		}
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)

		var n int64

		n = 0
		if err := db.Table("comment_queue").Where("status = ?", localdb.CommentStatusPending).Count(&n).Error; err == nil && n > 0 {
			totals.pending += uint64(n)
		}

		n = 0
		if err := db.Table("comment_queue").Where("status = ?", localdb.CommentStatusSuccess).Count(&n).Error; err == nil && n > 0 {
			totals.success += uint64(n)
		}

		n = 0
		if err := db.Table("comment_queue").Where("status = ?", localdb.CommentStatusFailed).Count(&n).Error; err == nil && n > 0 {
			totals.fail += uint64(n)
		}

		_ = sqlDB.Close()
	}

	return totals
}
