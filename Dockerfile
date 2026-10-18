# syntax=docker/dockerfile:1
# debroxy container image (spec §11, C12-04, C13-06): the browser client
# built on Node, embedded into a static binary on Alpine, run as a non-root
# user; all state lives in the /data volume.
#
# Multi-arch without emulation: every stage that runs commands runs on the
# build platform; Go cross-compiles for the target, and the final stage only
# copies files.

FROM --platform=$BUILDPLATFORM node:24.21.0-alpine AS web
WORKDIR /src/web
ENV COREPACK_HOME=/tmp/corepack
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack pnpm install --frozen-lockfile
COPY web/ ./
RUN corepack pnpm run build

FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/debroxy ./cmd/debroxy

# The debroxy user and /data, made on the build platform; the same Alpine
# release, so its account files fit the target image.
FROM --platform=$BUILDPLATFORM alpine:3.24.2 AS accounts
RUN addgroup -S -g 65532 debroxy \
	&& adduser -S -D -H -u 65532 -G debroxy -h /data debroxy \
	&& install -d -o debroxy -g debroxy -m 0700 /data

FROM alpine:3.24.2
# The base image already carries the CA bundle (ca-certificates-bundle).
# A new named volume takes /data's owner: the non-root debroxy user.
COPY --from=accounts /etc/passwd /etc/group /etc/shadow /etc/
COPY --from=accounts --chown=65532:65532 --chmod=0700 /data /data
COPY --from=build /out/debroxy /usr/local/bin/debroxy
USER debroxy:debroxy
ENV DEBROXY_DATA_DIR=/data \
	DEBROXY_SERVER_LISTEN=:8080
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 CMD ["debroxy", "healthcheck"]
ENTRYPOINT ["debroxy"]
CMD ["serve"]
