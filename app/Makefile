export DOWNLOAD_DIR              = ./downloads
export INTERVAL_MINUTES          = 1
export MAX_PARALLEL_CONTAINERS   = 2
export MAX_PARALLEL_BLOBS        = 4
export AZURE_CLIENT_ID           = a6808a8f-e41b-4071-a7d4-e162c48af064
export AZURE_TENANT_ID           = 841e991f-1730-433f-82ba-ea7895004ffa
export AZURE_CLIENT_SECRET       = .Kh8Q~FjJyvB-nIeapwu2WtuON2dltM95Q29_cPM
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