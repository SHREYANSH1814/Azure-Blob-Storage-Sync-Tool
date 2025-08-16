export DOWNLOAD_DIR              = ./downloads
export INTERVAL_MINUTES          = 1
export MAX_PARALLEL_CONTAINERS   = 2
export MAX_PARALLEL_BLOBS        = 4
export AZURE_CLIENT_ID           = <>
export AZURE_TENANT_ID           = <>
export AZURE_CLIENT_SECRET       = <>
export AZURE_STORAGE_ACCOUNT     = blobfetcherstorage123



build:
	@echo "Building Go program..."
	go build -o blob-fetcher cmd/azure-blob-fetcher/main.go

run: build
	@echo "Running blob fetcher..."
	./blob-fetcher


# Clean
clean:
	rm -rf cmd/azure-blob-fetcher/$(DOWNLOAD_DIR)