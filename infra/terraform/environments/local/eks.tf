resource "aws_eks_cluster" "local" {
  name     = var.cluster_name
  role_arn = aws_iam_role.cluster.arn
  version  = var.kubernetes_version

  vpc_config {
    subnet_ids              = [for subnet in aws_subnet.local : subnet.id]
    endpoint_private_access = false
    endpoint_public_access  = true
  }

  kubernetes_network_config {
    service_ipv4_cidr = "10.100.0.0/16"
  }

  depends_on = [
    aws_iam_role_policy_attachment.cluster,
    aws_route_table_association.local,
  ]

  timeouts {
    create = "10m"
    delete = "5m"
  }
}

# Floci models one shared k3s node; this does not create a second worker.
resource "aws_eks_node_group" "local" {
  cluster_name    = aws_eks_cluster.local.name
  node_group_name = "local"
  node_role_arn   = aws_iam_role.worker.arn
  subnet_ids      = [for subnet in aws_subnet.local : subnet.id]
  instance_types  = ["m5.large"]
  capacity_type   = "ON_DEMAND"

  scaling_config {
    desired_size = 1
    min_size     = 1
    max_size     = 1
  }

  depends_on = [aws_iam_role_policy_attachment.worker]

  timeouts {
    create = "10m"
    delete = "5m"
  }
}
