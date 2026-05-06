#!/bin/bash
set -eu

VARIANT="${1:-regular}"

if [ -z "${DRIVERS_TOOLS:-}" ]; then
  echo "DRIVERS_TOOLS must be set to a drivers-evergreen-tools checkout" 1>&2
  exit 1
fi

if [ -z "${AWS_PROFILE:-}" ]; then
  if [[ -z "${AWS_ACCESS_KEY_ID:-}" || -z "${AWS_SECRET_ACCESS_KEY:-}" ]]; then
    echo "Set AWS_PROFILE or AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY for setup-secrets.sh" 1>&2
    exit 1
  fi
fi

if [ -n "${AWS_PROFILE:-}" ]; then
  aws sso login --profile "$AWS_PROFILE"
fi

AUTH_AWS_DIR="${DRIVERS_TOOLS}/.evergreen/auth_aws"

# Force a fresh pull from the drivers/auth_aws vault. Without this,
# aws_setup.sh reuses a cached secrets-export.sh and silently authenticates
# with rotated/expired IAM keys.
rm -f "$AUTH_AWS_DIR/secrets-export.sh"

pushd "$AUTH_AWS_DIR" >/dev/null

# aws_setup.sh sources secrets-export.sh internally, calls aws_tester.py to
# create AWS-mapped users on the running mongod, and writes test-env.sh with
# an MONGODB_URI export.
. ./aws_setup.sh "$VARIANT"

popd >/dev/null

# aws_setup.sh sourced test-env.sh, so MONGODB_URI is now in our env.
echo "MONGODB_URI=$MONGODB_URI"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# The awsauth shim signs STS requests with creds from the AWS SDK default chain
# (env vars / profile / IMDS), not with the IAM keys embedded in MONGODB_URI.
# Run `go test` in a subshell with the env scoped to the URI's identity so the
# chain signs as the user the server expects, without disturbing the caller's
# shell. The unsets and exports are local to this subshell only.
(
  unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE
  eval "$(python3 - <<'PY'
import os, shlex
from urllib.parse import urlparse, parse_qs, unquote
u = urlparse(os.environ["MONGODB_URI"])
if u.username:
    print(f"export AWS_ACCESS_KEY_ID={shlex.quote(unquote(u.username))}")
if u.password:
    print(f"export AWS_SECRET_ACCESS_KEY={shlex.quote(unquote(u.password))}")
for kv in parse_qs(u.query).get("authMechanismProperties", [""])[0].split(","):
    if kv.startswith("AWS_SESSION_TOKEN:"):
        print(f"export AWS_SESSION_TOKEN={shlex.quote(kv.split(':', 1)[1])}")
PY
)"
  go test -run '^TestMGD_AWS$' -v -count=1 .
)
