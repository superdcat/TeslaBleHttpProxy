FROM --platform=${BUILDPLATFORM} golang:1.25.14 AS builder

# Install git.
# Git is required for fetching the dependencies.
#RUN apk update && apk add --no-cache git tzdata
WORKDIR $GOPATH/src/wimaha/teslaBleHttpProxy/
COPY . .
# Fetch dependencies.
# Using go get.
#RUN go get -d -v

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG GOARM=${TARGETVARIANT#v}
# Version reported by GET /api/proxy/1/version, set by the release workflow from the git tag.
# Empty: the binary keeps the default of config.Version (*undefined*); .git is not in the build context.
ARG VERSION

#WORKDIR /app/
#ADD . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${GOARM} make build-docker VERSION="${VERSION}"
#RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM=${GOARM} go build -ldflags="-w -s" -o /go/bin/teslaBleHttpProxy main.go
RUN mkdir -p /go/bin/key

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/superdcat/TeslaBleHttpProxy" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY LICENSE NOTICE /usr/share/doc/tesla-ble-http-proxy/
# Timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
ENV TZ=Europe/Berlin
#WORKDIR /app/
COPY --from=builder /go/bin/teslaBleHttpProxy /teslaBleHttpProxy
COPY --from=builder /go/bin/key /key
EXPOSE 8080
ENTRYPOINT ["/teslaBleHttpProxy"]
