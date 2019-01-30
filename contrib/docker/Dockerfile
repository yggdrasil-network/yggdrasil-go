FROM docker.io/golang:alpine as builder

WORKDIR /src

RUN apk add git

RUN git clone https://github.com/yggdrasil-network/yggdrasil-go.git .

ENV CGO_ENABLED=0

RUN ./build

FROM docker.io/alpine
LABEL maintainer="Christer Waren/CWINFO <christer.waren@cwinfo.org>"

COPY --from=builder /src/yggdrasil /usr/bin/yggdrasil
COPY --from=builder /src/yggdrasilctl /usr/bin/yggdrasilctl
COPY --from=builder /src/contrib/docker/entrypoint.sh /usr/bin/entrypoint.sh

# RUN addgroup -g 1000 -S yggdrasil-network \
#  && adduser -u 1000 -S -g 1000 --home /etc/yggdrasil-network yggdrasil-network
#
# USER yggdrasil-network
# TODO: Make running unprivileged work

VOLUME [ "/etc/yggdrasil-network" ]

ENTRYPOINT [ "/usr/bin/entrypoint.sh" ]
