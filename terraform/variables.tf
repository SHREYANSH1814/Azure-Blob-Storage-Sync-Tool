variable "location" {
  description = "Azure region for resources"
  type        = string
  default     = "centralus"
}

variable "resource_group_name" {
  description = "Name of the Resource Group"
  type        = string
  default     = "blob-fetcher-rg"
}

variable "storage_account_name" {
  description = "Name of the Azure Storage Account (must be globally unique, lowercase)"
  type        = string
  default     = "blobfetcherstorage123"
}

