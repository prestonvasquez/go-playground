#!/bin/bash
set -ex

pushd "${DRIVERS_TOOLS}/.evergreen/docker"

MONGODB_VERSION="latest" TOPOLOGY=replica_set ./run-server.sh

popd
