// Command whalegate 是鲸闸网关的服务端入口。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/app"
)

// version 由构建脚本通过 -ldflags 注入。
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath  = flag.String("c", "", "配置文件路径（默认搜索 ./configs/config.yaml）")
		migrateOnly = flag.Bool("migrate", false, "执行数据库迁移后退出")
		migrateDown = flag.Bool("migrate-down", false, "回滚一个数据库迁移版本后退出")
		showVersion = flag.Bool("version", false, "打印版本号后退出")
		migVersion  = flag.Bool("migrate-version", false, "打印当前数据库迁移版本后退出")
		skipDB      = flag.Bool("skip-db", false, "不连接数据库与缓存（仅用于检查配置）")
		autoMigrate = flag.Bool("auto-migrate", true, "启动时自动执行迁移")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return nil
	}

	application, err := app.New(app.Options{
		ConfigPath: *configPath,
		Version:    version,
		SkipDB:     *skipDB,
	})
	if err != nil {
		return err
	}

	if *migVersion {
		v, dirty, err := application.MigrationVersion()
		if err != nil {
			return err
		}
		fmt.Printf("version=%d dirty=%v\n", v, dirty)
		return nil
	}
	if *migrateDown {
		return application.RunMigrations(true)
	}
	if *migrateOnly {
		return application.RunMigrations(false)
	}
	if application.DB == nil {
		return nil
	}
	if *autoMigrate && application.Config.Database.AutoMigrate {
		if err := application.RunMigrations(false); err != nil {
			return err
		}
	}
	if err := application.BootstrapAdmin(); err != nil {
		return err
	}

	stop := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if application.Logger != nil {
			application.Logger.Info("收到退出信号，开始优雅关闭")
		}
		close(stop)
	}()

	if err := application.Run(stop); err != nil {
		if application.Logger != nil {
			application.Logger.Error("服务运行异常", zap.Error(err))
		}
		return err
	}
	return nil
}
