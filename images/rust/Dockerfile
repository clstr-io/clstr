FROM rust:1.94

RUN apt-get update && \
    apt-get install -y --no-install-recommends iptables iproute2 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY Cargo.toml Cargo.lock ./
RUN mkdir src && echo 'fn main() {}' > src/main.rs && cargo build --release

COPY src ./src

RUN touch src/main.rs && cargo build --release

VOLUME ["/app/data"]

EXPOSE 8080
ENTRYPOINT ["./target/release/server"]
