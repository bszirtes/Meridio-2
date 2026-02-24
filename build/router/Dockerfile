ARG BUILD_VERSION=0.0.0-unknown

FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags "-extldflags -static -X main.Version=${BUILD_VERSION}" -o router ./cmd/router

FROM alpine

RUN apk update && apk add iproute2 tcpdump nftables bird

RUN mkdir -p /run/bird && mkdir -p /etc/bird

COPY --from=builder /workspace/router .

CMD ["./router", "run"]
