FROM alpine:latest AS builder

RUN mkdir -p /app/bin && \ 
    mkdir /app/src && \
    mkdir /app/output 

COPY schemafixer /app/bin/
RUN chmod +x /app/bin/schemafixer

WORKDIR /app/src

FROM scratch

COPY --from=builder /app /app
WORKDIR /app/src

ENTRYPOINT ["/app/bin/schemafixer"]
