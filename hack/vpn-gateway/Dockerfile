FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        lksctp-tools \
        iproute2 \
        tcpdump \
        iputils-ping \
        bird2 \
        ethtool \
        xz-utils \
        curl \
        software-properties-common \
    && add-apt-repository -y ppa:dreibh/ppa \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        netperfmeter-all \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*

WORKDIR /root/

ADD https://github.com/Nordix/ctraffic/releases/download/v1.7.0/ctraffic.gz ctraffic.gz
RUN gunzip ctraffic.gz \
  && chmod u+x ctraffic \
  && mv ctraffic /usr/bin/

ARG BIRD_CONFIG_PATH=.

COPY $BIRD_CONFIG_PATH/bird-common.conf /etc/bird/
COPY $BIRD_CONFIG_PATH/bird-gw.conf /etc/bird/
COPY $BIRD_CONFIG_PATH/bird-gw-no-default.conf /etc/bird/
COPY $BIRD_CONFIG_PATH/bird-filler-net.conf /etc/bird/

COPY $BIRD_CONFIG_PATH/init.sh /init.sh
RUN chmod +x /init.sh

CMD ["/init.sh"]
