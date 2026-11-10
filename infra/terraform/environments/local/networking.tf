resource "aws_vpc" "local" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = var.cluster_name }
}

resource "aws_subnet" "local" {
  for_each = { a = 1, b = 2 }

  vpc_id            = aws_vpc.local.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, each.value)
  availability_zone = "${var.region}${each.key}"

  tags = { Name = "${var.cluster_name}-${each.key}" }
}

# Docker provides local egress. This does not model private-subnet egress in AWS.
resource "aws_route_table" "local" {
  vpc_id = aws_vpc.local.id
  tags   = { Name = var.cluster_name }
}

resource "aws_route_table_association" "local" {
  for_each = aws_subnet.local

  subnet_id      = each.value.id
  route_table_id = aws_route_table.local.id
}
