#!/usr/bin/env bash

set -euo pipefail

# Re-install the provider locally
go install .

# Run tofu init in the test directory
TF_LOG=DEBUG tofu -chdir=examples/data-sources plan
