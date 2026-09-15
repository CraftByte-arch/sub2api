# Run tests with the local builder architecture, then cross-compile the static
# binary for the requested target. This avoids relying on Go toolchain emulation
# when producing the hc2 linux/amd64 image from an Apple Silicon workstation.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct

ENV GOPROXY=$GOPROXY

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/account-auto-scheduler ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata && \
    mkdir -p /data && \
    chown 65532:65532 /data

WORKDIR /app
COPY --from=build /out/account-auto-scheduler /app/account-auto-scheduler

USER 65532:65532
EXPOSE 8091
VOLUME ["/data"]

ENTRYPOINT ["/app/account-auto-scheduler"]
