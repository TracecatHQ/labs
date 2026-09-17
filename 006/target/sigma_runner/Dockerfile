FROM rust:1.89-slim-bookworm@sha256:d7fc7de78bb8c1469933aeecbf801314d30d7d6e9f0578bba4cfa285bfa37fe6 AS builder

WORKDIR /build
COPY sigma_runner/Cargo.toml sigma_runner/Cargo.lock ./
COPY sigma_runner/src ./src
RUN cargo test --release --locked \
    && cargo build --release --locked

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

ENV CTI_REALM_DATA_DIR=/data

RUN useradd --create-home --uid 10001 runner
COPY --from=builder /build/target/release/sigma-runner /usr/local/bin/sigma-runner

USER runner
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --retries=6 \
  CMD ["/usr/local/bin/sigma-runner", "--healthcheck"]

CMD ["/usr/local/bin/sigma-runner"]
