package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AccountURL            string
	DownloadDir           string
	Interval              time.Duration
	MaxParallelContainers int
	MaxParallelBlobs      int
}

func Load() (Config, error) {
	cfg := Config{
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
		accURL = "https://" + accName + ".blob.core.windows.net"
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
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Minute
		}
	}
	return time.Duration(def) * time.Minute
}

func intEnv(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
