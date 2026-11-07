# The API image, also the one Railway builds in Phase 13. It holds the server, the operator CLI, goose and the
# migrations, so one image serves `server`, the migration step and one-off `admin` commands.
# Base images are pinned by tag and multi-arch index digest (T1). No cache mounts: Railway requires its own ids.

FROM docker.io/library/golang:1.27.3-alpine3.24@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build

WORKDIR /src
# The image's Go is the toolchain go.mod pins; local refuses to download another one silently.
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS="-trimpath -buildvcs=false"

# go build fetches only the modules these binaries need and checks each against go.sum. A full
# `go mod download` would also pull sqlc's large dependency tree, which nothing here uses.
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal

# goose comes from the version go.mod pins with the tool directive, built with only the Postgres driver.
RUN go build -ldflags="-s -w" -o /out/server ./cmd/server \
 && go build -ldflags="-s -w" -o /out/admin ./cmd/admin \
 && go build -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck \
 && go build -ldflags="-s -w" \
      -tags "no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb" \
      -o /out/goose github.com/pressly/goose/v3/cmd/goose

# No shell, package manager or libc: static binaries only, run as uid 65532.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

WORKDIR /app
COPY --from=build /out/ /app/
COPY migrations /app/migrations

# goose reads its connection string from GOOSE_DBSTRING, set by whoever runs the migration step.
ENV HTTP_ADDR=:8080 \
    GOOSE_DRIVER=postgres \
    GOOSE_MIGRATION_DIR=/app/migrations

USER nonroot:nonroot
EXPOSE 8080
CMD ["/app/server"]
