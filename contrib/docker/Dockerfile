FROM docker.io/golang:alpine as builder

COPY . /src
WORKDIR /src

ENV CGO_ENABLED=0

RUN apk add git && ./build && go build -o /src/genkeys cmd/genkeys/main.go

FROM docker.io/alpine

COPY --from=builder /src/mesh /usr/bin/mesh
COPY --from=builder /src/meshctl /usr/bin/meshctl
COPY --from=builder /src/genkeys /usr/bin/genkeys
COPY contrib/docker/entrypoint.sh /usr/bin/entrypoint.sh

# RUN addgroup -g 1000 -S RiV-chain \
#  && adduser -u 1000 -S -g 1000 --home /etc/RiV-chain RiV-chain
#
# USER RiV-chain
# TODO: Make running unprivileged work

VOLUME [ "/etc/RiV-chain" ]

ENTRYPOINT [ "/usr/bin/entrypoint.sh" ]
