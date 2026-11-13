ARG GO_VERSION="1.27"


# 1. Build binary
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

ENV GOOS=${TARGETOS}
ENV GOARCH=${TARGETARCH}

WORKDIR /build

RUN apk add git

# Cache dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -ldflags "-s -w" -trimpath


# 2. Final image
FROM alpine

COPY --from=build /build/pnproxy /usr/local/bin/

VOLUME /config
WORKDIR /config

CMD ["pnproxy", "-config", "/config/pnproxy.yaml"]
