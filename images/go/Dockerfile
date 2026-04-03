FROM golang:1.26

RUN apt-get update && \
    apt-get install -y --no-install-recommends iptables iproute2 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# Update '.' if your main package is in a subdirectory, e.g. ./cmd/my-server
RUN go build -o server .

VOLUME ["/app/data"]

EXPOSE 8080
ENTRYPOINT ["./server"]
