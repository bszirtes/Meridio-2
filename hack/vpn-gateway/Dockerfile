FROM golang:1.23 AS ctraffic-builder

RUN git clone --depth 1 -b v1.7.0 https://github.com/Nordix/ctraffic.git /tmp/ctraffic && \
    cd /tmp/ctraffic && \
    CGO_ENABLED=0 go build -o ctraffic ./cmd/ctraffic

FROM ubuntu:24.04 AS netperfmeter-builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        software-properties-common \
        gnupg \
    && add-apt-repository -y ppa:dreibh/ppa \
    && apt-get update \
    && apt-get install -y --no-install-recommends netperfmeter-all \
    && rm -rf /var/lib/apt/lists/*

FROM alpine:latest

RUN apk update && apk add iproute2 iputils tcpdump bird ethtool lksctp-tools

RUN mkdir -p /etc/bird/ /run/bird

ARG BIRD_CONFIG_PATH=.

COPY $BIRD_CONFIG_PATH/bird-common.conf /etc/bird/
COPY $BIRD_CONFIG_PATH/bird-gw.conf /etc/bird/
COPY $BIRD_CONFIG_PATH/bird-gw-no-default.conf /etc/bird/
COPY $BIRD_CONFIG_PATH/bird-filler-net.conf /etc/bird/

COPY $BIRD_CONFIG_PATH/init.sh /init.sh
RUN chmod +x /init.sh

COPY --from=ctraffic-builder /tmp/ctraffic/ctraffic /opt/ctraffic
COPY --from=netperfmeter-builder /usr/bin/netperfmeter /opt/netperfmeter

CMD ["/init.sh"]
