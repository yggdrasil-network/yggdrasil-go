FROM golang:stretch
MAINTAINER Christer Waren/CWINFO "christer.waren@cwinfo.org"

RUN apt-get update \
    && apt-get upgrade -y

ADD . /src

WORKDIR /src

RUN adduser --system --home /etc/yggdrasil-network --uid 1000 yggdrasil-network \
    && rm -rf build_* && ./build \
    && cp yggdrasil /usr/bin \
    && cp contrib/docker/entrypoint.sh /

VOLUME [ "/etc/yggdrasil-network" ]

ENTRYPOINT [ "/entrypoint.sh" ]
