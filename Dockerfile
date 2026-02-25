FROM alpine:latest AS builder

RUN mkdir -p /app/bin
RUN mkdir /src

COPY schemafixer /app/bin/
RUN chmod +x /app/bin/schemafixer

FROM scratch

COPY --from=builder /app/bin /app/bin
COPY --from=builder /src /src

WORKDIR /src

ENTRYPOINT ["/app/bin/schemafixer"]
