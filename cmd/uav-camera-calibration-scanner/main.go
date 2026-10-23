package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"jingansi/uav-camera-calibration-scanner/internal/app"
	"jingansi/uav-camera-calibration-scanner/internal/config"
	"jingansi/uav-camera-calibration-scanner/internal/result"
)

func main() {
	var configPath string
	var scanDate string
	var dryRun bool
	var mode string
	flag.StringVar(&configPath, "config", "configs/config.yml", "path to JSON/YAML config")
	flag.StringVar(&scanDate, "date", "", "scan date in YYYY-MM-DD; default is previous day in configured timezone")
	flag.BoolVar(&dryRun, "dry-run", false, "force VOD dry-run mode")
	flag.StringVar(&mode, "mode", "once", "run mode: once or schedule")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}
	setupLogger(cfg.Logging)
	if dryRun {
		cfg.VOD.DryRun = true
		cfg.Calibration.Enabled = false
	}

	mysqlSink, err := result.NewMySQLSink(cfg.Result.MySQLDSN, cfg.Result.MySQLTable)
	if err != nil {
		slog.Error("init mysql result sink failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if mysqlSink != nil {
			if err := mysqlSink.Close(); err != nil {
				slog.Warn("close mysql sink failed", "error", err)
			}
		}
	}()
	sinks := []result.Sink{result.NewJSONLSink(cfg.Result.JSONLPath)}
	if mysqlSink != nil {
		sinks = append(sinks, mysqlSink)
	}
	sink := result.NewComposite(sinks...)

	runner, err := app.NewRunner(cfg, sink)
	if err != nil {
		slog.Error("init runner failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "once":
		if err := runOnce(ctx, runner, cfg, scanDate); err != nil {
			slog.Error("run scan failed", "error", err)
			os.Exit(1)
		}
	case "schedule":
		if err := runSchedule(ctx, runner, cfg, scanDate); err != nil {
			slog.Error("run schedule failed", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("unsupported run mode", "mode", mode)
		os.Exit(1)
	}
}

func setupLogger(cfg config.LoggingConfig) {
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(cfg.Level)) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(strings.TrimSpace(cfg.Format), "text") {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))
		return
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
}

func runSchedule(ctx context.Context, runner *app.Runner, cfg config.Config, scanDate string) error {
	loc, err := cfg.Location()
	if err != nil {
		return err
	}
	if cfg.Schedule.RunOnStart {
		if err := runOnce(ctx, runner, cfg, scanDate); err != nil {
			slog.Error("scheduled startup scan failed", "error", err)
		}
	}
	for {
		next, err := nextRunAt(time.Now().In(loc), cfg.Schedule.DailyAt, loc)
		if err != nil {
			return err
		}
		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		slog.Info("next scheduled scan", "runAt", next.Format(time.RFC3339), "wait", wait.String())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			slog.Info("schedule stopped", "reason", ctx.Err())
			return nil
		case <-timer.C:
			if err := runOnce(ctx, runner, cfg, scanDate); err != nil {
				slog.Error("scheduled scan failed", "error", err)
			}
		}
	}
}

func runOnce(ctx context.Context, runner *app.Runner, cfg config.Config, scanDate string) error {
	staleAfter := 24 * time.Hour
	if cfg.Resilience.RunTimeoutMinutes > 0 {
		staleAfter = time.Duration(cfg.Resilience.RunTimeoutMinutes+10) * time.Minute
	}
	release, acquired, err := acquireRunLock(cfg.Resilience.LockFile, staleAfter)
	if err != nil {
		return err
	}
	if !acquired {
		slog.Warn("scan skipped because another run holds the lock", "lockFile", cfg.Resilience.LockFile)
		return nil
	}
	defer release()

	runCtx := ctx
	cancel := func() {}
	if cfg.Resilience.RunTimeoutMinutes > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(cfg.Resilience.RunTimeoutMinutes)*time.Minute)
	}
	defer cancel()

	started := time.Now()
	slog.Info("scan started", "scanDate", scanDate)
	err = runner.RunOnce(runCtx, scanDate)
	if err != nil {
		return err
	}
	slog.Info("scan finished", "duration", time.Since(started).String())
	return nil
}

func acquireRunLock(path string, staleAfter time.Duration) (func(), bool, error) {
	if strings.TrimSpace(path) == "" {
		return func() {}, true, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			if attempt == 0 && staleAfter > 0 {
				info, statErr := os.Stat(path)
				if statErr == nil && time.Since(info.ModTime()) > staleAfter {
					slog.Warn("removing stale scan lock", "lockFile", path, "age", time.Since(info.ModTime()).String())
					_ = os.Remove(path)
					continue
				}
			}
			return func() {}, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		_, _ = fmt.Fprintf(file, "pid=%d\nstarted_at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
		release := func() {
			_ = file.Close()
			_ = os.Remove(path)
		}
		return release, true, nil
	}
	return func() {}, false, nil
}

func nextRunAt(now time.Time, dailyAt string, loc *time.Location) (time.Time, error) {
	clock, err := parseClock(dailyAt, loc)
	if err != nil {
		return time.Time{}, err
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), clock.Second(), 0, loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

func parseClock(value string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{"15:04:05", "15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, loc); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse schedule dailyAt %q", value)
}
