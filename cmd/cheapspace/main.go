package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cheapspace/internal/config"
	"cheapspace/internal/db"
	"cheapspace/internal/runtime"
	"cheapspace/internal/service"
	"cheapspace/internal/web"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cfg := config.Load()
	if len(args) == 0 {
		return serve(cfg)
	}

	switch args[0] {
	case "serve":
		return serve(cfg)
	case "migrate":
		return migrateCommand(cfg, args[1:], stdout)
	case "workspace", "workspaces":
		return workspaceCommand(cfg, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(cfg config.Config) error {
	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	if cfg.AutoMigrate {
		if err := db.Migrate(sqlDB); err != nil {
			return err
		}
	}

	store := db.NewStore(sqlDB)
	rt, err := runtimeFromConfig(cfg)
	if err != nil {
		return err
	}
	svc := service.New(cfg, store, rt)
	server, err := web.New(cfg, svc)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	svc.Start(ctx)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("cheapspace listening on http://%s (runtime=%s)", cfg.Addr, rt.Kind())
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func migrateCommand(cfg config.Config, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cheapspace migrate [up|status]")
	}

	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	switch args[0] {
	case "up":
		return db.Migrate(sqlDB)
	case "status":
		statuses, err := db.MigrationStatuses(sqlDB)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			applied := "pending"
			if status.Applied {
				applied = "applied"
			}
			when := "-"
			if status.AppliedAt != nil {
				when = status.AppliedAt.Local().Format(time.RFC3339)
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", status.Version, applied, when)
		}
		return nil
	default:
		return fmt.Errorf("unknown migrate subcommand %q", args[0])
	}
}

func runtimeFromConfig(cfg config.Config) (runtime.Runtime, error) {
	switch cfg.Runtime {
	case "", "mock":
		return runtime.NewMock(), nil
	case "docker":
		return runtime.NewDocker(cfg.DockerBinary, cfg.GitBinary, cfg.NixpacksBinary, cfg.WorkspaceNetwork), nil
	default:
		return nil, fmt.Errorf("unsupported runtime %q", cfg.Runtime)
	}
}
