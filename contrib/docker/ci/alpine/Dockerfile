FROM alpine

ARG BUILDPLATFORM
ENV TARGETPLATFORM=$TARGETPLATFORM

COPY ${TARGETPLATFORM}/* /usr/bin/

COPY contrib/docker/alpine/entrypoint.sh /usr/bin/entrypoint.sh

ENTRYPOINT [ "/usr/bin/entrypoint.sh" ]
