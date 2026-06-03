#!/bin/sh
exec pkg-config --define-variable=prefix=/opt/homebrew/Cellar/libmongocrypt/1.17.0 "$@"
