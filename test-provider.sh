#!/usr/bin/env bash

set -euo pipefail

# Re-install the provider locally
go install .

# Run tofu init in the test directory
tofu -chdir=examples/provider-install-verification plan
