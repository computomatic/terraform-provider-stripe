#!/usr/bin/env bash

set -euo pipefail

export GOBIN=$PWD/bin
export PATH=$GOBIN:$PATH
go install github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs

tfplugindocs generate --provider-name stripe
