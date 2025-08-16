# Azure Blob Storage Sync Tool

This repository contains a robust Azure Blob Storage synchronization tool built in Go, along with Terraform configuration for infrastructure provisioning and GitHub Actions for CI/CD automation.

## Key Features

- **Efficient Incremental Sync**: Only downloads new files from Azure during each sync interval, avoiding redundant transfers
- **Automatic Cleanup**: Removes local files that have been deleted from Azure containers
- **Parallel Processing**: Concurrently processes multiple containers and blobs for optimal performance
- **Robust Error Handling**: Implements retry logic with exponential backoff for network resilience

## Table of Contents

- [Prerequisites](#prerequisites)
- [Part 1: Terraform + DevOps](#part-1-terraform--devops)
- [Part 2: Go Azure Blob Fetcher](#part-2-go-azure-blob-fetcher)
- [Edge Case Handling](#edge-case-handling)
- [Environment Variables](#environment-variables)
- [Notes](#notes)

---

## Prerequisites

- **Terraform** (v1.5 or above)
- **Go** (v1.20 or above)
- **Azure CLI** (`az`) logged in
- GitHub account for CI/CD

---

## Part 1: Terraform + DevOps

### Structure

```
terraform/
├── main.tf
├── variables.tf
├── outputs.tf
├── provider.tf
└── terraform.tfvars
.github/
└── workflows/
    └── terraform.yml
```

### Setup

1. **Clone the repository**:
   ```bash
   git clone git@github.com:SHREYANSH1814/Azure-Blob-Storage-Sync-Tool.git
   cd Azure-Blob-Storage-Sync-Tool
   ```

2. **Initialize Terraform**:
   ```bash
   cd terraform
   terraform init
   ```

3. **Set variables in terraform.tfvars**:
   ```
   location              = "eastus"
   resource_group_name   = "blob-fetcher-rg"
   storage_account_name  = "blobfetcherstorage123"
   ```

4. **Apply Terraform configuration**:
   ```bash
   terraform apply
   ```

### CI/CD Pipeline

The GitHub Actions workflow in `.github/workflows/terraform.yml` automates Terraform deployments:

- Triggers on pushes to the `main` branch that modify files in the `terraform/` directory
- Checks for existing Azure resources and imports them if they exist
- Runs `terraform apply` to create or update resources

---

## Part 2: Go Azure Blob Fetcher

### Structure

```
app/
├── cmd/
│   └── azure-blob-fetcher/
│       └── main.go           # Application entry point
├── internal/
│   ├── azure/                # Azure client code
│   ├── config/               # Configuration management
│   ├── retry/                # Retry logic implementation
│   └── sync/                 # Blob synchronization logic
└── Makefile                  # Build and run instructions
```

### Setup

1. **Configure Azure credentials**:
   Edit the `app/Makefile` to set your Azure credentials:
   ```makefile
   export AZURE_CLIENT_ID           = <your-client-id>
   export AZURE_TENANT_ID           = <your-tenant-id>
   export AZURE_CLIENT_SECRET       = <your-client-secret>
   export AZURE_STORAGE_ACCOUNT     = <storage-account-name>
   ```

2. **Build the application**:
   ```bash
   cd app
   make build
   ```

### Running the Application

```bash
cd app
make run
```

The application will:
1. Create the download directory if it doesn't exist
2. Connect to the Azure Blob Storage using environment credentials
3. Sync all containers and their blobs to a local directory
4. Continue syncing periodically (default: every minute)

### Configuration Options

Edit the Makefile to customize behavior:
```makefile
export DOWNLOAD_DIR              = ./downloads   # Local download location
export INTERVAL_MINUTES          = 1             # Sync interval (minutes)
export MAX_PARALLEL_CONTAINERS   = 2             # Concurrency limit for containers
export MAX_PARALLEL_BLOBS        = 4             # Concurrency limit for blobs
```

---

## Edge Case Handling

### Optimized File Synchronization

The application implements intelligent file synchronization to optimize bandwidth and storage:

#### 1. Downloading Only New Files

When syncing, the application:
- Checks if a file already exists locally before downloading
- Skips downloading files that are already present
- Ensures bandwidth efficiency by only transferring new content

Implementation in `sync.go`:
```go
localPath := filepath.Join(localContainerDir, filepath.FromSlash(b.Name))
if _, err := os.Stat(localPath); err == nil {
    continue  // Skip files that already exist
}
```

#### 2. Removing Deleted Files

The application maintains consistency with Azure by:
- Building a map of all files in the local directory
- Comparing against files present in Azure
- Removing local files that no longer exist in Azure

Implementation in `sync.go`:
```go
// Delete local files that were deleted from Azure
localFiles := map[string]struct{}{}
filepath.Walk(localContainerDir, func(path string, info os.FileInfo, err error) error {
    if err != nil || info.IsDir() {
        return nil
    }
    relPath, _ := filepath.Rel(localContainerDir, path)
    localFiles[relPath] = struct{}{}
    return nil
})

azureBlobs := map[string]struct{}{}
for _, b := range blobs {
    azureBlobs[b.Name] = struct{}{}
}

for f := range localFiles {
    if _, ok := azureBlobs[f]; !ok {
        fullPath := filepath.Join(localContainerDir, f)
        _ = os.Remove(fullPath)
    }
}
```

### Robust Error Handling

#### Network Resilience

- Implemented retry logic with exponential backoff
- Automatically retries on transient errors (408, 429, 5xx status codes)
- Configurable max attempts and delay parameters

#### Authentication and Authorization

- Uses Azure SDK's EnvironmentCredential for secure authentication
- Detailed logging for troubleshooting permission issues
- Requires "Storage Blob Data Reader" role for the service principal

---

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `AZURE_CLIENT_ID` | Service Principal client ID | Yes |
| `AZURE_TENANT_ID` | Azure AD tenant ID | Yes |
| `AZURE_CLIENT_SECRET` | Service Principal secret | Yes |
| `AZURE_STORAGE_ACCOUNT` | Storage account name | Yes |

---

## Notes

- This application is designed to run as a continuous sync service or in a scheduled container
- Files are synchronized to a local `./downloads` directory by default
- The sync process maintains a directory structure mirroring the container/blob structure in Azure
- Authentication issues typically require checking the service principal's role assignments
