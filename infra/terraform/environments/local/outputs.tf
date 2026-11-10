output "cluster_name" {
  value = aws_eks_cluster.local.name
}

output "cluster_endpoint" {
  value = aws_eks_cluster.local.endpoint
}

output "cluster_certificate_authority_data" {
  value = aws_eks_cluster.local.certificate_authority[0].data
}

output "vpc_id" {
  value = aws_vpc.local.id
}

output "subnet_ids" {
  value = [for subnet in aws_subnet.local : subnet.id]
}

output "region" {
  value = var.region
}

output "operator_credentials" {
  sensitive = true
  value = {
    access_key_id     = aws_iam_access_key.operator.id
    secret_access_key = aws_iam_access_key.operator.secret
  }
}
