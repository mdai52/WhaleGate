package database

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // 注册 postgres 驱动
	_ "github.com/golang-migrate/migrate/v4/source/file"       // 注册文件源
	"go.uber.org/zap"
)

// newMigrate 创建迁移器。dir 为包含 *.sql 的目录。
func newMigrate(dsn, dir string) (*migrate.Migrate, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析迁移目录失败: %w", err)
	}
	m, err := migrate.New("file://"+absDir, dsn)
	if err != nil {
		return nil, fmt.Errorf("初始化迁移器失败: %w", err)
	}
	return m, nil
}

// MigrateUp 执行迁移至最新版本。
func MigrateUp(dsn, dir string, lg *zap.Logger) error {
	m, err := newMigrate(dsn, dir)
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			if lg != nil {
				lg.Info("数据库已是最新，无需迁移")
			}
			return nil
		}
		return fmt.Errorf("执行迁移失败: %w", err)
	}
	version, _, _ := m.Version()
	if lg != nil {
		lg.Info("数据库迁移完成", zap.Uint("version", version))
	}
	return nil
}

// MigrateDown 回滚一个版本。
func MigrateDown(dsn, dir string, lg *zap.Logger) error {
	m, err := newMigrate(dsn, dir)
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	if err := m.Steps(-1); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		return fmt.Errorf("回滚迁移失败: %w", err)
	}
	version, dirty, _ := m.Version()
	if lg != nil {
		lg.Info("数据库已回滚", zap.Uint("version", version), zap.Bool("dirty", dirty))
	}
	return nil
}

// MigrateVersion 查询当前迁移版本，dirty 表示处于脏状态。
func MigrateVersion(dsn, dir string) (version uint, dirty bool, err error) {
	m, err := newMigrate(dsn, dir)
	if err != nil {
		return 0, false, err
	}
	defer closeMigrate(m)

	version, dirty, err = m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return version, dirty, nil
}

func closeMigrate(m *migrate.Migrate) {
	srcErr, dbErr := m.Close()
	_ = srcErr
	_ = dbErr
}
