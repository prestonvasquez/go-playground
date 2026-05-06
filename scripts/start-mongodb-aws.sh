#!/bin/bash
set -ex

# Spin up a MongoDB instance with MONGODB-AWS authentication enabled, in
# Docker, in the foreground. Run this in a dedicated terminal; in another
# terminal use scripts/run-aws-test.sh to create AWS-mapped users and
# execute TestMGD_AWS.
#
# Requires: DRIVERS_TOOLS pointing at a clone of mongodb-labs/drivers-evergreen-tools
#
# Optional env:
#   MONGODB_VERSION  default: latest
#   TOPOLOGY         default: server (standalone). Other values: replica_set, sharded_cluster

if [ -z "${DRIVERS_TOOLS:-}" ]; then
    echo "DRIVERS_TOOLS must be set to a drivers-evergreen-tools checkout" 1>&2
    exit 1
fi

pushd "${DRIVERS_TOOLS}/.evergreen/docker"

AUTH=auth \
    ORCHESTRATION_FILE=auth-aws.json \
    TOPOLOGY="${TOPOLOGY:-server}" \
    MONGODB_VERSION="${MONGODB_VERSION:-latest}" \
    ./run-server.sh

popd
