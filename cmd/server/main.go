package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"secret-santa/internal/api"
	"secret-santa/internal/config"
	"secret-santa/internal/db"
	"secret-santa/internal/mailer"
	"secret-santa/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "set-password" {
		if err := runSetPassword(os.Args[2:]); err != nil {
			log.Fatalf("set-password error: %v", err)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("database initialization error: %v", err)
	}
	defer sqlDB.Close()

	repo := db.NewRepository(sqlDB)
	mailSvc := mailer.NewMailer(cfg)

	staticFS, err := web.DistFS()
	if err != nil {
		log.Fatalf("embedded static files error: %v", err)
	}

	router := api.NewRouter(cfg, repo, mailSvc, staticFS)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("Secret Santa server listening on :%s (env=%s, base_url=%s)", cfg.Port, cfg.AppEnv, cfg.AppBaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func runSetPassword(args []string) error {
	fs := flag.NewFlagSet("set-password", flag.ContinueOnError)
	envFile := fs.String("env", ".env", "path to .env file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var password string
	if fs.NArg() > 0 {
		password = fs.Arg(0)
	} else {
		fmt.Fprint(os.Stderr, "Enter new Super Admin password (min 12 chars): ")
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("read password from stdin: %w", err)
		}
		password = strings.TrimRight(line, "\r\n")
	}

	if len(password) < 12 {
		return errors.New("password must be at least 12 characters")
	}

	hash, err := config.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := config.SetEnvPasswordHash(*envFile, hash); err != nil {
		return fmt.Errorf("write %s: %w", *envFile, err)
	}
	fmt.Fprintf(os.Stderr, "Updated SUPER_ADMIN_PASSWORD_HASH in %s\n", *envFile)
	return nil
}
