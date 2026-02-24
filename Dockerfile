FROM alpine:latest AS builder

RUN mkdir -p /app/bin && \ 
    mkdir /app/src && \
    mkdir /app/output 

COPY schemafixer /app/bin/
RUN chmod +x /app/bin/schemafixer

WORKDIR /app/src

FROM scratch

COPY --from=builder /app /app

ENTRYPOINT ["/app/bin/schemafixer", "parse", "/app/src", "--output", "/app/output/schemafixer.json", "--logtoconsole"]

