# Go Playground

Personal testing ground for Go development and experimentation.

## Client-Side Encryption (CSE) Testing

CSE tests require the `cse` build tag:

```bash
go test -tags=cse -v ./...
```

### Setup

1. **libmongocrypt** - Install or update via Homebrew:
   https://github.com/mongodb/libmongocrypt?tab=readme-ov-file#installing-libmongocrypt-on-macos

   After updating, modify `~/.zshrc` to reflect the new version path.

2. **mongocryptd** - Download MongoDB Enterprise:
   https://www.mongodb.com/try/download/enterprise

   Extract and move `mongocryptd` to `~/.local/bin/` (or update PATH).
