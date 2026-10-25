package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"secret-santa/internal/api"
	"secret-santa/internal/config"
	"secret-santa/internal/db"
	"secret-santa/internal/mailer"
	"secret-santa/web"
)

func main() {
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
