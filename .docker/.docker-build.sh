#!/usr/bin/env sh
set -e

# Build images using the same tag style as ja-dushu-front:
# registry/name:version.yymmdd and registry/name:version.yymmdd-arm.
make docker-build-amd
make docker-build-arm
make docker-build-public
make docker-build-public-arm

make docker-push-amd
make docker-push-arm
make docker-push-public
make docker-push-public-arm
