FROM golang:1.26.4-bookworm AS build

WORKDIR /src

ARG TARGETOS=linux
ARG TARGETARCH=amd64

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -o /out/ward ./cmd/ward

FROM debian:bookworm-slim

RUN apt-get update \
	&& apt-get install -y --no-install-recommends bash ca-certificates findutils git \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=build /out/ward /usr/local/bin/ward
COPY docker/demo/demo.sh /usr/local/bin/ward-demo

RUN chmod 0755 /usr/local/bin/ward /usr/local/bin/ward-demo

ENV WARD_DEMO_REPO=cli/cli \
	WARD_DEMO_ORG=cli \
	WARD_DEMO_WORKSPACE=/workspace/cli \
	WARD_DEMO_SUBSTRATE=/substrate/cli

ENTRYPOINT ["/usr/local/bin/ward-demo"]
