# s3viewer

[![codecov](https://codecov.io/gh/define42/s3viewer/graph/badge.svg?token=69TTE972LZ)](https://codecov.io/gh/define42/s3viewer)

Simple web UI for S3-compatible object storage (AWS S3, MinIO, etc.).

## What it does

- Login with `AccessKey` + `SecretKey` (session cookie via `gorilla/securecookie`)
- List buckets
- Browse objects with pagination (10 items per page, `Prev`/`Next`)
- Show object metadata and tags in bucket view
- View object details (metadata + tags)
- Download objects
- Upload one or multiple files
- Create/delete buckets
- Delete objects
- Generate presigned download URLs (configurable expiry: 1 hour, 24 hours, or 7 days)

## Requirements

- Go `1.25+`
- Docker (for Docker Compose and current test suite)

## Quick start (Docker Compose + MinIO)

```bash
docker compose up -d --build
```

Open:

- App: `http://localhost:8080`
- MinIO Console: `http://localhost:9001`

Login to the app with:

- Access key: `minioadmin`
- Secret key: `minioadmin`

Stop:

```bash
docker compose down
```

## Run locally (without Docker Compose)

```bash
export S3_REGION=us-east-1
export S3_ENDPOINT=http://localhost:9000
export S3_FORCE_PATH_STYLE=true
go run .
```

Then open `http://localhost:8080` and login with your S3 credentials.

## Configuration

### App server

- `LISTEN_ADDR` (default `:8080`)
- `AWS_REGION` or `S3_REGION` (default `eu-west-1`)
- `AWS_ENDPOINT_URL` or `S3_ENDPOINT` (optional, for S3-compatible endpoints like MinIO)
- `S3_FORCE_PATH_STYLE` (`true` for many S3-compatible providers)
- `S3_ENDPOINT_TLSSKIP` (`true` to skip TLS certificate verification; only use with trusted private endpoints)

### Session cookie keys

- `SECURECOOKIE_HASH_KEY` (required length: at least 32 bytes)
- `SECURECOOKIE_BLOCK_KEY` (required length: 16, 24, or 32 bytes)

If cookie keys are not provided, the app generates ephemeral keys at startup (all sessions are invalidated on restart).

## Notes on credentials

- The UI login credentials are used for all S3 operations.
- Environment variables like `S3_ACCESS_KEY`/`S3_SECRET_KEY` are not required for app auth flow.

## Testing

Run all tests:

```bash
go test ./...
```

Coverage:

```bash
go test -coverprofile=cover.out ./...
go tool cover -func=cover.out
```

Current suite includes a MinIO integration test using `testcontainers-go`, so Docker must be available when running `go test ./...`.

## Project layout

- `main.go`: startup and route wiring
- `auth.go`: login/logout, secure cookie session handling
- `aws_config.go`: AWS SDK config/client creation
- `handlers_*.go`: HTTP handlers by domain
- `helpers.go`: shared helper functions
- `templates.go`: HTML templates and render helpers
- `integration_minio_test.go`: end-to-end integration test with MinIO container
