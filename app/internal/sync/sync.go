package sync

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/SHREYANSH1814/stackguard-assignment/app/internal/retry"
)

type Config struct {
	AccountURL            string
	DownloadDir           string
	MaxParallelContainers int
	MaxParallelBlobs      int
}

func RunOnce(ctx context.Context, cfg Config, client *azblob.Client) error {
	var containers []string
	r := retry.Retryer{MaxAttempts: 6}

	err := r.Do(ctx, "list containers", func() error {
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
	}, retry.IsRetriable)
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		log.Println("No containers found (nothing to do).")
		return nil
	}

	log.Printf("Found %d container(s).", len(containers))

	contSem := make(chan struct{}, cfg.MaxParallelContainers)
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex

	for _, container := range containers {
		container := container
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

// helper for safe paths
func safePath(s string) string {
	s = strings.ReplaceAll(s, "..", "__")
	return strings.Trim(s, "/\\")
}

// helper for downloading blobs with retry
func downloadBlobWithRetry(ctx context.Context, containerClient *container.Client, baseDir, container, blobName string) error {
	r := retry.Retryer{MaxAttempts: 6, BaseDelay: 750 * time.Millisecond, MaxDelay: 12 * time.Second}

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
	}, retry.IsRetriable)
}

func syncContainer(ctx context.Context, cfg Config, client *azblob.Client, container string) error {
	log.Printf("Syncing container: %s", container)
	containerClient := client.ServiceClient().NewContainerClient(container)

	type blobInfo struct {
		Name string
		Size int64
	}
	var blobs []blobInfo

	r := retry.Retryer{MaxAttempts: 6}
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
	}, retry.IsRetriable)
	if err != nil {
		return err
	}

	if len(blobs) == 0 {
		log.Printf("Container %s is empty.", container)
		return nil
	}

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
