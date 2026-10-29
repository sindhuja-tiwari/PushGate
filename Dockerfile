# One image with every consumer release, the fleet tools and the producer.
# docker compose runs it as the "fleet" service (see docker-compose.yml).

FROM rust:1.82-bookworm AS rust
WORKDIR /src/consumers/rust-proxy
COPY consumers/rust-proxy/ ./
RUN for v in 41 42 43 44; do \
      cargo build --release --features v$v --target-dir target/v$v && \
      mkdir -p /out/bot-proxy && cp target/v$v/release/bot-proxy /out/bot-proxy/v$v; \
    done

FROM golang:1.24-bookworm AS go
WORKDIR /src
COPY go.mod ./
COPY consumers/go-policy/ consumers/go-policy/
COPY fleet/ fleet/
RUN for v in 41 42 43 44; do \
      CGO_ENABLED=0 go build -ldflags "-X main.buildVersion=$v" -o /out/go-policy/v$v ./consumers/go-policy; \
    done && \
    CGO_ENABLED=0 go build -o /out/fleet ./fleet/cmd/fleet && \
    CGO_ENABLED=0 go build -o /out/pgctl ./fleet/cmd/pgctl && \
    CGO_ENABLED=0 go build -o /out/loadgen ./fleet/cmd/loadgen

FROM python:3.12-slim-bookworm
WORKDIR /app
COPY producer/requirements.txt producer/requirements.txt
RUN python -m venv .venv && .venv/bin/pip install --no-cache-dir -r producer/requirements.txt
COPY --from=rust /out/ bin/
COPY --from=go /out/ bin/
COPY deploy/ deploy/
COPY seed/ seed/
COPY producer/ producer/
COPY scripts/ scripts/
ENV PATH=/app/bin:$PATH
CMD ["bin/fleet", "-config", "deploy/fleet.json", "-fresh", "-listen", "0.0.0.0:19000"]
