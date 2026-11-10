# This root is exclusively for Floci. Do not make these endpoints optional.
provider "aws" {
  region     = var.region
  access_key = "test"
  secret_key = "test"
  token      = ""

  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    ec2 = "http://127.0.0.1:4566"
    eks = "http://127.0.0.1:4566"
    iam = "http://127.0.0.1:4566"
    sts = "http://127.0.0.1:4566"
  }
}
