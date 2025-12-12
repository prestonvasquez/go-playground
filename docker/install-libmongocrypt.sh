#!/usr/bin/env bash
# install libmongocrypt
# This script installs libmongocrypt into an "install" directory.
set -eux

LIBMONGOCRYPT_TAG="1.12.0"

rm -rf libmongocrypt
git clone https://github.com/mongodb/libmongocrypt --depth=1 --branch $LIBMONGOCRYPT_TAG 2> /dev/null
if ! ( ./libmongocrypt/.evergreen/compile.sh >| output.txt 2>&1 ); then
    cat output.txt 1>&2
    exit 1
fi
mv output.txt install
rm -rf libmongocrypt
