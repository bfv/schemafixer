FROM alpine:latest AS builder

RUN mkdir -p /app/bin && \ 
    mkdir /app/src && \
    mkdir /app/output 

COPY annotator /app/bin/
RUN chmod +x /app/bin/annotator

WORKDIR /app/src

FROM scratch

COPY --from=builder /app /app

ENTRYPOINT ["/app/bin/annotator", "parse", "/app/src", "--output", "/app/output/annotations.json", "--logtoconsole"]

