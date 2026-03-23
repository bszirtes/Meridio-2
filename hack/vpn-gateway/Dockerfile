FROM golang:1.23 AS builder

RUN git clone --depth 1 https://github.com/Nordix/ctraffic.git /tmp/ctraffic && \
    cd /tmp/ctraffic && \
    CGO_ENABLED=0 go build -o ctraffic ./cmd/ctraffic

FROM alpine:latest

RUN apk update && apk add iproute2 iputils tcpdump bird

RUN mkdir -p /etc/bird/ /run/bird

COPY bird.conf /etc/bird/
COPY init.sh /init.sh
RUN chmod +x /init.sh

COPY --from=builder /tmp/ctraffic/ctraffic /opt/ctraffic

CMD ["/init.sh"]
