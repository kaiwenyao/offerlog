// Command admin performs one-shot operations: create the first user account,
// list/retry failed jobs, and stats. Run locally or in the deployment
// container:
//
//	go run ./cmd/admin create-user -email me@example.com -password '...' -name "Offer Logs"
//	go run ./cmd/admin jobs
//	go run ./cmd/admin retry -id 3
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"offerlog/backend/internal/bootstrap"
	"offerlog/backend/internal/platform/config"
	"offerlog/backend/internal/platform/migrate"
	"offerlog/backend/internal/platform/observability"
)

func main() {
	create := flag.NewFlagSet("create-user", flag.ExitOnError)
	email := create.String("email", "", "login email")
	password := create.String("password", "", "password (min 8 chars)")
	name := create.String("name", "Owner", "display name")
	tz := create.String("timezone", "Europe/Dublin", "user timezone")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	observability.Init(cfg.App.LogLevel)
	ctx := context.Background()

	if len(os.Args) < 2 {
		fmt.Println("usage: admin <create-user|jobs|retry>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "create-user":
		_ = create.Parse(os.Args[2:])
		if *email == "" || *password == "" {
			fmt.Println("create-user requires -email and -password")
			os.Exit(2)
		}
		if len(*password) < 8 {
			fmt.Println("password must be at least 8 characters")
			os.Exit(2)
		}
		app, err := bootstrap.New(ctx, cfg)
		if err != nil {
			slog.Error("bootstrap", "error", err)
			os.Exit(1)
		}
		defer app.DB.Close()
		if err := migrate.Up(ctx, app.DB.Pool()); err != nil {
			slog.Error("migrate", "error", err)
			os.Exit(1)
		}
		if _, err := app.Auth.Register(ctx, *email, *password, *name, *tz); err != nil {
			fmt.Println("create user failed:", err)
			os.Exit(1)
		}
		fmt.Printf("created user %s\n", *email)
	case "jobs":
		app, err := bootstrap.New(ctx, cfg)
		if err != nil {
			slog.Error("bootstrap", "error", err)
			os.Exit(1)
		}
		defer app.DB.Close()
		jobs, err := app.Jobs.List(ctx, 20)
		if err != nil {
			slog.Error("jobs", "error", err)
			os.Exit(1)
		}
		for _, j := range jobs {
			fmt.Printf("#%d kind=%s status=%s attempts=%d/%d err=%s\n", j.ID, j.Kind, j.Status, j.Attempts, j.MaxAttempts, j.LastError)
		}
	default:
		fmt.Println("unknown command", os.Args[1])
		os.Exit(2)
	}
}
