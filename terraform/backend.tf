terraform {
  backend "azurerm" {
    resource_group_name   = "tfstate-rg"              # jo RG aapne create kiya
    storage_account_name  = "tfstatesg1814"            # apna storage account name (globally unique)
    container_name        = "tfstate"                 # container ka naam
    key                   = "terraform.tfstate"       # file ka naam jo blob me save hoga
  }
}