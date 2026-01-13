#!/bin/bash
set -eu

# Setup OIDC secrets and tokens for mongolocal testing.
# Requires: AWS credentials (AWS_PROFILE or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY/AWS_SESSION_TOKEN)
# Output: /tmp/oidc/secrets.json and /tmp/oidc/test_machine (plus other token files)

OIDC_DIR="/tmp/oidc"

if [ -z "${AWS_PROFILE:-}" ]; then
    if [[ -z "${AWS_ACCESS_KEY_ID:-}" || -z "${AWS_SECRET_ACCESS_KEY:-}" ]]; then
        echo "Please set AWS_PROFILE or AWS credentials environment variables" 1>&2
        exit 1
    fi
fi

# Login to AWS SSO if using a profile.
if [ -n "${AWS_PROFILE:-}" ]; then
    echo "Logging in to AWS SSO with profile: $AWS_PROFILE"
    aws sso login --profile "$AWS_PROFILE"
fi

echo "Setting up OIDC in $OIDC_DIR"
mkdir -p "$OIDC_DIR"

# Fetch secrets from AWS Secrets Manager and save to JSON file.
echo "Fetching OIDC secrets from AWS Secrets Manager..."

AWS_ARGS="--secret-id drivers/oidc --region us-east-1 --query SecretString --output text"
if [ -n "${AWS_PROFILE:-}" ]; then
    AWS_ARGS="$AWS_ARGS --profile $AWS_PROFILE"
fi

aws secretsmanager get-secret-value $AWS_ARGS > "$OIDC_DIR/secrets.json"

echo "Secrets saved to $OIDC_DIR/secrets.json"

# Generate tokens using drivers-evergreen-tools.
if [ -z "${DRIVERS_TOOLS:-}" ]; then
    echo "DRIVERS_TOOLS not set, skipping token generation" 1>&2
    echo "Set DRIVERS_TOOLS and re-run to generate tokens" 1>&2
    exit 0
fi

echo "Generating OIDC tokens..."
export OIDC_TOKEN_DIR="$OIDC_DIR"

pushd "$DRIVERS_TOOLS/.evergreen/auth_oidc" > /dev/null

# Activate the Python venv and generate tokens.
. ./activate-authoidcvenv.sh
python ./oidc_get_tokens.py

popd > /dev/null

echo ""
echo "OIDC setup complete!"
echo "  Secrets: $OIDC_DIR/secrets.json"
echo "  Tokens:  $OIDC_DIR/test_machine"
echo ""
