FROM scratch

ENV XDG_RUNTIME_DIR /

COPY yggdrasil /

ENTRYPOINT ["/yggdrasil"]

CMD ["-autoconf"]
