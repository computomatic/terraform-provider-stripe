#!/usr/bin/env bash

set -euo pipefail

# Re-install the provider locally
go install .

# Run tofu plan on each example
TF_LOG=DEBUG tofu -chdir=examples/balance plan
TF_LOG=DEBUG tofu -chdir=examples/product plan

# Run go test
TF_ACC_TERRAFORM_PATH="/usr/local/bin/tofu"
TF_ACC_PROVIDER_NAMESPACE="hashicorp"
TF_ACC_PROVIDER_HOST="registry.opentofu.org"
TF_ACC=1 go test -count=1 -v ./internal/provider
