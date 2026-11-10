variable "cluster_name" {
  description = "Name of the isolated local Floci cluster."
  type        = string
  default     = "polyglot-ticketing-local"
}

variable "region" {
  description = "Emulated AWS region."
  type        = string
  default     = "us-east-1"
}

variable "kubernetes_version" {
  description = "Kubernetes version mapped to a pinned k3s image by Floci 2.4.0."
  type        = string
  default     = "1.34"
}

variable "vpc_cidr" {
  description = "Local emulated VPC range; choose a range without host/Docker/VPN overlap."
  type        = string
  default     = "10.60.0.0/16"
}
