package awsutil

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type TempCreds struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      time.Time
	RoleARN         string
}

// AssumeRoleFromProfile uses the AWS SDK to assume the role specified in the
// given profile and returns temporary credentials. This can be used to set a
// time-limited credential provider for testing against AWS-IAM-enabled MongoDB
// clusters.
func AssumeRoleFromProfile(ctx context.Context, profile string, duration time.Duration) (*TempCreds, error) {
	if profile == "" {
		return nil, fmt.Errorf("profile is required")
	}

	roleARN, err := ResolveRoleARNFromAWSConfig(profile)
	if err != nil {
		return nil, err
	}

	cfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithSharedConfigProfile(profile),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config for profile %q: %w", profile, err)
	}

	stsClient := sts.NewFromConfig(cfg)
	sessionName := "go-driver-aws-auth-test"

	input := &sts.AssumeRoleInput{
		RoleArn:         &roleARN,
		RoleSessionName: &sessionName,
	}
	if duration > 0 {
		secs := int32(duration / time.Second)
		input.DurationSeconds = &secs
	}

	out, err := stsClient.AssumeRole(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("assume role %q using profile %q: %w", roleARN, profile, err)
	}
	if out.Credentials == nil {
		return nil, fmt.Errorf("assume role returned nil credentials")
	}

	return &TempCreds{
		AccessKeyID:     stringValue(out.Credentials.AccessKeyId),
		SecretAccessKey: stringValue(out.Credentials.SecretAccessKey),
		SessionToken:    stringValue(out.Credentials.SessionToken),
		Expiration:      timeValue(out.Credentials.Expiration),
		RoleARN:         roleARN,
	}, nil
}

// ResolveRoleARNFromAWSConfig reads the AWS config file to find the role ARN
// associated with the given profile.
func ResolveRoleARNFromAWSConfig(profile string) (string, error) {
	// Use AWS_CONFIG_FILE if set, otherwise default to ~/.aws/config.
	configPath := os.Getenv("AWS_CONFIG_FILE")
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home dir: %w", err)
		}
		configPath = filepath.Join(home, ".aws", "config")
	}

	f, err := os.Open(configPath)
	if err != nil {
		return "", fmt.Errorf("open aws config %q: %w", configPath, err)
	}
	defer f.Close()

	sectionNames := []string{
		"profile " + profile,
		profile, // tolerate credentials-style section names
	}

	current := ""
	values := map[string]string{}

	// Scan the config file line by line, looking for the section matching the
	// profile and then extracting role_arn or sso_account_id + sso_role_name from
	// that section.
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			continue
		}

		if !matchesAnySection(current, sectionNames) {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		values[k] = v
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan aws config %q: %w", configPath, err)
	}

	if roleARN := values["role_arn"]; roleARN != "" {
		return roleARN, nil
	}

	accountID := values["sso_account_id"]
	roleName := values["sso_role_name"]
	if accountID != "" && roleName != "" {
		return fmt.Sprintf("arn:aws:iam::%s:role/%s", accountID, roleName), nil
	}

	return "", fmt.Errorf(
		"profile %q in %s does not contain role_arn or sso_account_id + sso_role_name",
		profile, configPath,
	)
}

func matchesAnySection(current string, wanted []string) bool {
	for _, w := range wanted {
		if current == w {
			return true
		}
	}
	return false
}

func stringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func timeValue(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

// FetchSecret retrieves a Secrets Manager secret using the given AWS profile.
// Emulates drivers-evergreen-tools/.evergreen/secrets_handling/setup_secrets.py:
// loads the profile (refreshing the SSO session if expired), calls
// secretsmanager:GetSecretValue, and parses the JSON payload. Keys in the
// returned map are upper-cased to match the script's `secrets-export.sh` form.
//
// Stale AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN env vars
// are cleared so the profile isn't shadowed by leftover env state from a
// prior run-aws-test.sh.
func FetchSecret(ctx context.Context, profile, secretID string) (map[string]string, error) {
	if profile == "" {
		return nil, fmt.Errorf("profile is required")
	}
	if secretID == "" {
		return nil, fmt.Errorf("secretID is required")
	}

	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_SESSION_TOKEN")

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithSharedConfigProfile(profile))
	if err != nil {
		return nil, fmt.Errorf("load aws config for profile %q: %w", profile, err)
	}

	if _, retrieveErr := cfg.Credentials.Retrieve(ctx); retrieveErr != nil && isSSOError(retrieveErr) {
		if loginErr := ssoLogin(ctx, profile); loginErr != nil {
			return nil, fmt.Errorf("sso login %q: %w", profile, loginErr)
		}
		cfg, err = awsconfig.LoadDefaultConfig(ctx, awsconfig.WithSharedConfigProfile(profile))
		if err != nil {
			return nil, fmt.Errorf("reload aws config after sso login: %w", err)
		}
	}

	client := secretsmanager.NewFromConfig(cfg)
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	if err != nil {
		return nil, fmt.Errorf("GetSecretValue %q: %w", secretID, err)
	}
	if out.SecretString == nil {
		return nil, fmt.Errorf("secret %q has no string value", secretID)
	}

	var raw map[string]string
	if err := json.Unmarshal([]byte(*out.SecretString), &raw); err != nil {
		return nil, fmt.Errorf("parse secret JSON for %q: %w", secretID, err)
	}

	upper := make(map[string]string, len(raw))
	for k, v := range raw {
		upper[strings.ToUpper(k)] = v
	}
	return upper, nil
}

// StripURICredentials returns mongoURI with any user:password component removed.
// Useful for converting a SCRAM admin URI into a host-only URI for MONGODB-AWS.
func StripURICredentials(mongoURI string) (string, error) {
	u, err := url.Parse(mongoURI)
	if err != nil {
		return "", fmt.Errorf("parse uri: %w", err)
	}
	u.User = nil
	return u.String(), nil
}
