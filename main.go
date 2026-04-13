package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tagmanager-microservice/internal/config"
	"tagmanager-microservice/internal/httpapi"
	"tagmanager-microservice/internal/storage"
)

const (
	defaultListenAddr = ":8080"
	defaultConfigPath = "/etc/saymon/saymon-server.conf"
)

func main() {
	cfgPath := os.Getenv("SAYMON_CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = defaultConfigPath
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	listenAddr := os.Getenv("HTTP_ADDR")
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collectionName := os.Getenv("TAGS_COLLECTION")
	if collectionName == "" {
		collectionName = "tags"
	}

	repo, err := storage.NewMongoRepository(ctx, cfg.MongoURL, collectionName)
	if err != nil {
		log.Fatalf("failed to init mongodb repository: %v", err)
	}
	defer func() {
		if cerr := repo.Close(context.Background()); cerr != nil {
			log.Printf("mongodb close error: %v", cerr)
		}
	}()

	router := httpapi.NewRouter(repo)
	server := &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("tagmanager service listens on %s", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server failed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
}
