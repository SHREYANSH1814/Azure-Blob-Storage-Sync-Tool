package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SHREYANSH1814/stackguard-assignment/app/internal/azure"
	"github.com/SHREYANSH1814/stackguard-assignment/app/internal/config"
	"github.com/SHREYANSH1814/stackguard-assignment/app/internal/sync"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Config load error: %v", err)
	}
	log.Println("Config loaded successfully")

	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		log.Fatalf("creating download dir error: %v", err)
	}
	log.Println("Download dir created successfully")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client, err := azure.NewServiceClient(ctx, cfg.AccountURL)
	if err != nil {
		log.Fatalf("azure client error: %v", err)
	}
	log.Println("Azure client created successfully")

	if err := sync.RunOnce(ctx, sync.Config{
		DownloadDir:           cfg.DownloadDir,
		MaxParallelContainers: cfg.MaxParallelContainers,
		MaxParallelBlobs:      cfg.MaxParallelBlobs,
	}, client); err != nil {
		log.Printf("initial sync error: %v", err)
	}
	log.Println("Initial sync completed successfully")

	log.Println("Shreyansh" + cfg.Interval.String())

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down...")
			return
		case <-ticker.C:
			log.Println("Starting sync...")
			if err := sync.RunOnce(ctx, sync.Config{
				DownloadDir:           cfg.DownloadDir,
				MaxParallelContainers: cfg.MaxParallelContainers,
				MaxParallelBlobs:      cfg.MaxParallelBlobs,
			}, client); err != nil {
				log.Printf("sync error: %v", err)
			}
			log.Println("Sync completed successfully")
		}
	}
}
