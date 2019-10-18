FROM golang:1.12 as builder

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 ./build

FROM scratch

COPY --from=builder /var/run /var/run
COPY --from=builder /src/yggdrasil /yggdrasil

ENTRYPOINT ["/yggdrasil"]
CMD ["-autoconf"]
