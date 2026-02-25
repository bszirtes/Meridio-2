ARG BUILD_VERSION=0.0.0-unknown
ARG USER=meridio
ARG UID=10001
ARG HOME=/home/${USER}

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
ARG USER
ARG UID
ARG HOME

RUN apk update && apk add iproute2 tcpdump nftables bird iputils net-tools libcap \
  && setcap 'cap_sys_ptrace,cap_dac_override+ep' /sbin/ss \
  && setcap 'cap_sys_ptrace,cap_dac_override+ep' /bin/netstat \
  && setcap 'cap_net_raw+ep' /bin/ping \
  && setcap 'cap_net_raw+ep' /usr/bin/tcpdump

RUN mkdir -p /run/bird && mkdir -p /etc/bird && chmod ag+rx
RUN addgroup --gid $UID $USER \
  && adduser $USER --home $HOME --uid $UID -G $USER --disabled-password \
  && chown -R :root "${HOME}" && chmod -R g+s=u "${HOME}"
WORKDIR $HOME

COPY --from=builder /workspace/router .

RUN setcap 'cap_net_admin+ep' ./router \
  && setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw+ep' /usr/sbin/bird
USER ${UID}:${UID}

CMD ["./router"]
