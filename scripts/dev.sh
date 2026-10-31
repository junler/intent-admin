#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
exec node web/scripts/dev.mjs --api "${1:-configs/config.yaml}"
