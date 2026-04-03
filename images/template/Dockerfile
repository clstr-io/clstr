# Replace with the base image for your language/runtime
# e.g. FROM golang:1.26, FROM python:3.14
FROM your-language:latest

RUN apt-get update && \
    apt-get install -y --no-install-recommends iptables iproute2 && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy dependency manifests first so Docker can cache the install layer.
# Replace with the right files for your language, e.g.:
#   COPY go.mod go.sum .     (Go)
#   COPY requirements.txt .  (Python)

# Install dependencies. Examples:
#   RUN go mod download
#   RUN pip install -r requirements.txt

COPY . .

# Build your server (if your language requires a compile step). Examples:
#   RUN go build -o server .
#   RUN cargo build --release
# Interpreted languages (Python, Node, Ruby, etc.) can skip this step.

VOLUME ["/app/data"]

EXPOSE 8080
# Update ENTRYPOINT to start your server. Examples:
#   ENTRYPOINT ["./server"]            (compiled binary)
#   ENTRYPOINT ["python", "server.py"] (Python)
ENTRYPOINT ["./server"]
