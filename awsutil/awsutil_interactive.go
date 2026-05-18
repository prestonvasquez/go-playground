package awsutil

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AssumeRoleFromProfileInteractive is like AssumeRoleFromProfile but handles
// two interactive auth cases:
//
//   - MFA profile: prompts for the one-time token on stderr/stdin.
//   - SSO profile: starts the OIDC device-auth flow, opens the verification
//     URL in the default browser, and blocks until the user approves.
func AssumeRoleFromProfileInteractive(ctx context.Context, profile string, duration time.Duration) (*TempCreds, error) {
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
		// MFA: when the profile has mfa_serial, the SDK calls o.TokenProvider
		// on the AssumeRole call. stscreds.StdinTokenProvider prompts on stderr
		// and reads the one-time code from stdin.
		awsconfig.WithAssumeRoleCredentialOptions(func(o *stscreds.AssumeRoleOptions) {
			o.TokenProvider = stscreds.StdinTokenProvider
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config for profile %q: %w", profile, err)
	}

	// If the profile is SSO-based the token may be expired. Attempt credential
	// retrieval now; if it fails with an SSO error, run the device-auth flow
	// and retry once.
	_, credErr := cfg.Credentials.Retrieve(ctx)
	if credErr != nil && isSSOError(credErr) {
		fmt.Fprintf(os.Stderr, "SSO token for profile %q is expired — starting login flow…\n", profile)
		if err := ssoLogin(ctx, profile); err != nil {
			return nil, fmt.Errorf("sso login: %w", err)
		}
		// Reload config so the fresh SSO token is picked up.
		cfg, err = awsconfig.LoadDefaultConfig(
			ctx,
			awsconfig.WithSharedConfigProfile(profile),
			awsconfig.WithAssumeRoleCredentialOptions(func(o *stscreds.AssumeRoleOptions) {
				o.TokenProvider = stscreds.StdinTokenProvider
			}),
		)
		if err != nil {
			return nil, fmt.Errorf("reload aws config after sso login: %w", err)
		}
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

// ssoLogin runs `aws sso login --profile <profile>`, which handles the full
// OIDC device-auth flow: it prints a URL + code, opens the browser, and blocks
// until the user approves.
func ssoLogin(ctx context.Context, profile string) error {
	cmd := exec.CommandContext(ctx, "aws", "sso", "login", "--profile", profile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr // show the URL/code to the user
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// aws CLI not available — fall back to printing the URL manually.
		fmt.Fprintf(os.Stderr, "`aws sso login` failed (%v). Open your SSO start URL manually, then press Enter to continue.\n", err)
		fmt.Fprint(os.Stderr, "Press Enter when authenticated: ")
		bufio.NewReader(os.Stdin).ReadString('\n')
	}
	return nil
}

// openBrowser opens url in the system default browser.
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}

// isSSOError reports whether err looks like an expired/missing SSO token.
func isSSOError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sso") ||
		strings.Contains(msg, "token has expired") ||
		strings.Contains(msg, "refresh token") ||
		strings.Contains(msg, "login")
}
