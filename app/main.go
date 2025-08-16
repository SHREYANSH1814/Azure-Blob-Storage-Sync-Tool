package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

// --------- Configuration ---------

// Configuration via env vars:
//
//   AZURE_STORAGE_ACCOUNT_URL = https://<account>.blob.core.windows.net
//   (or) AZURE_STORAGE_ACCOUNT = <account>  (we'll derive the URL)
//   DOWNLOAD_DIR = ./downloads (default)
//   INTERVAL_MINUTES = 10 (default)
//   MAX_PARALLEL_CONTAINERS = 4 (default)
//   MAX_PARALLEL_BLOBS = 8 (default per container)
//
// Authentication (DefaultAzureCredential):
//   - az login                      (developer machine)
//   - Managed Identity / Workload Identity in Azure
//   - Service Principal via AZURE_CLIENT_ID / TENANT_ID / CLIENT_SECRET, etc.
//
// SDK handles token refresh automatically.

type config struct {
	AccountURL            string
	DownloadDir           string
	Interval              time.Duration
	MaxParallelContainers int
	MaxParallelBlobs      int
}

func loadConfig() (config, error) {
	cfg := config{
		DownloadDir:           getenv("DOWNLOAD_DIR", "downloads"),
		Interval:              minutesEnv("INTERVAL_MINUTES", 10),
		MaxParallelContainers: intEnv("MAX_PARALLEL_CONTAINERS", 4),
		MaxParallelBlobs:      intEnv("MAX_PARALLEL_BLOBS", 8),
	}

	accURL := os.Getenv("AZURE_STORAGE_ACCOUNT_URL")
	if accURL == "" {
		accName := os.Getenv("AZURE_STORAGE_ACCOUNT")
		if accName == "" {
			return cfg, errors.New("set either AZURE_STORAGE_ACCOUNT_URL or AZURE_STORAGE_ACCOUNT")
		}
		accURL = fmt.Sprintf("https://%s.blob.core.windows.net", accName)
	}
	cfg.AccountURL = accURL
	return cfg, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func minutesEnv(k string, def int) time.Duration {
	if v := os.Getenv(k); v != "" {
		if n, err := time.ParseDuration(v + "m"); err == nil {
			return n
		}
	}
	return time.Duration(def) * time.Minute
}

func intEnv(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		var n int
		_, err := fmt.Sscanf(v, "%d", &n)
		if err == nil && n > 0 {
			return n
		}
	}
	return def
}

// --------- Backoff / Retry ---------

type retryer struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (r retryer) Do(ctx context.Context, opName string, fn func() error, retriable func(error) bool) error {
	if r.MaxAttempts <= 0 {
		r.MaxAttempts = 5
	}
	if r.BaseDelay <= 0 {
		r.BaseDelay = 500 * time.Millisecond
	}
	if r.MaxDelay <= 0 {
		r.MaxDelay = 10 * time.Second
	}

	var attempt int
	for {
		attempt++
		err := fn()
		if err == nil {
			return nil
		}
		if !retriable(err) || attempt >= r.MaxAttempts {
			return fmt.Errorf("%s failed after %d attempt(s): %w", opName, attempt, err)
		}
		// Exponential backoff with jitter
		backoff := time.Duration(float64(r.BaseDelay) * math.Pow(2, float64(attempt-1)))
		if backoff > r.MaxDelay {
			backoff = r.MaxDelay
		}
		jitter := time.Duration(float64(backoff) * (0.2 * (0.5 - randFloat())))
		sleep := backoff + jitter

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

func randFloat() float64 {
	// simple LCG-based pseudo-rand without bringing extra deps; good enough for jitter
	// NOT cryptographically secure
	now := time.Now().UnixNano()
	seed := uint64(now*1103515245 + 12345)
	return float64(seed%1000) / 1000.0
}

func isRetriable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	// The Azure SDK returns rich errors; as a generic rule treat temporary network or server errors as retriable.
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		// 5xx & some 408/429 are usually worth retrying
		if respErr.StatusCode == 408 || respErr.StatusCode == 429 || (respErr.StatusCode >= 500 && respErr.StatusCode <= 599) {
			return true
		}
		return false
	}
	// Fallback: likely network error
	return true
}

// --------- Azure Clients ---------

func makeServiceClient(ctx context.Context, accountURL string) (*azblob.Client, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("auth (DefaultAzureCredential) init: %w", err)
	}
	return azblob.NewClient(accountURL, cred, nil)
}

// --------- Work ---------

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Ensure download dir exists
	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		log.Fatalf("create download dir: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("Starting Azure Blob Fetcher")
	log.Printf("Account URL: %s", cfg.AccountURL)
	log.Printf("Download dir: %s", cfg.DownloadDir)
	log.Printf("Interval: %s", cfg.Interval)

	// First run immediately, then every interval
	if err := runOnce(ctx, cfg); err != nil {
		log.Printf("Initial sync error: %v", err)
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down...")
			return
		case <-ticker.C:
			if err := runOnce(ctx, cfg); err != nil {
				log.Printf("Sync error: %v", err)
			}
		}
	}
}

func runOnce(ctx context.Context, cfg config) error {
	client, err := makeServiceClient(ctx, cfg.AccountURL)
	if err != nil {
		return err
	}

	// list all containers with pagination (and retry wrapper)
	var containers []string
	r := retryer{MaxAttempts: 6}

	err = r.Do(ctx, "list containers", func() error {
		containers = containers[:0]
		pager := client.NewListContainersPager(&azblob.ListContainersOptions{})
		for pager.More() {
			pageCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			page, err := pager.NextPage(pageCtx)
			if err != nil {
				return err
			}
			for _, c := range page.ContainerItems {
				containers = append(containers, *c.Name)
			}
		}
		return nil
	}, isRetriable)
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		log.Println("No containers found (nothing to do).")
		return nil
	}

	log.Printf("Found %d container(s).", len(containers))

	// container parallelism
	contSem := make(chan struct{}, cfg.MaxParallelContainers)
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex

	for _, container := range containers {
		container := container // Create a copy for the goroutine
		wg.Add(1)
		contSem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-contSem }()

			if err := syncContainer(ctx, cfg, client, container); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				log.Printf("Container %q sync error: %v", container, err)
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func syncContainer(ctx context.Context, cfg config, client *azblob.Client, container string) error {
	log.Printf("Syncing container: %s", container)
	containerClient := client.ServiceClient().NewContainerClient(container)

	// pager for blobs
	type blobInfo struct {
		Name string
		Size int64
	}
	var blobs []blobInfo

	r := retryer{MaxAttempts: 6}
	err := r.Do(ctx, "list blobs "+container, func() error {
		blobs = blobs[:0]
		pager := containerClient.NewListBlobsFlatPager(&azblob.ListBlobsFlatOptions{})
		for pager.More() {
			pageCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()

			page, err := pager.NextPage(pageCtx)
			if err != nil {
				return err
			}
			for _, b := range page.Segment.BlobItems {
				size := int64(0)
				if b.Properties.ContentLength != nil {
					size = *b.Properties.ContentLength
				}
				blobs = append(blobs, blobInfo{Name: *b.Name, Size: size})
			}
		}
		return nil
	}, isRetriable)
	if err != nil {
		return err
	}

	if len(blobs) == 0 {
		log.Printf("Container %s is empty.", container)
		return nil
	}

	// blob parallelism within container
	blobSem := make(chan struct{}, cfg.MaxParallelBlobs)
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex

	for _, b := range blobs {
		b := b
		wg.Add(1)
		blobSem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-blobSem }()

			if err := downloadBlobWithRetry(ctx, containerClient, cfg.DownloadDir, container, b.Name); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				log.Printf("Blob %s/%s download error: %v", container, b.Name, err)
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func downloadBlobWithRetry(ctx context.Context, containerClient *container.Client, baseDir, container, blobName string) error {
	r := retryer{MaxAttempts: 6, BaseDelay: 750 * time.Millisecond, MaxDelay: 12 * time.Second}

	return r.Do(ctx, "download "+container+"/"+blobName, func() error {
		// Per-op timeout (streaming; keep reasonably large)
		opCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		blobClient := containerClient.NewBlobClient(blobName)

		// Start streaming download (no full buffering)
		resp, err := blobClient.DownloadStream(opCtx, nil)
		if err != nil {
			return err
		}
		body := resp.Body
		defer body.Close()

		localPath := filepath.Join(baseDir, safePath(container), filepath.FromSlash(blobName))
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(localPath), err)
		}

		tmpPath := localPath + ".part"
		out, err := os.Create(tmpPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", tmpPath, err)
		}
		defer func() {
			out.Close()
			// best effort cleanup if failed
			if err != nil {
				_ = os.Remove(tmpPath)
			}
		}()

		// Stream copy
		if _, err = io.Copy(out, body); err != nil {
			return fmt.Errorf("stream copy %s: %w", blobName, err)
		}
		if err := out.Sync(); err != nil {
			return fmt.Errorf("fsync %s: %w", blobName, err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("close %s: %w", blobName, err)
		}

		// Atomic-ish move
		if err := os.Rename(tmpPath, localPath); err != nil {
			return fmt.Errorf("rename to final %s: %w", localPath, err)
		}

		log.Printf("Saved: %s", localPath)
		return nil
	}, isRetriable)
}

func safePath(s string) string {
	// minimal sanitation for folder names
	s = strings.ReplaceAll(s, "..", "__")
	s = strings.Trim(s, "/\\")
	return s
}
