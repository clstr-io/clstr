FROM python:3.14

RUN apt-get update && \
    apt-get install -y --no-install-recommends iptables iproute2 && \
    rm -rf /var/lib/apt/lists/*

RUN pip install uv

WORKDIR /app

COPY requirements.txt* .
RUN if [ -f requirements.txt ]; then uv pip install --system -r requirements.txt; fi

COPY . .

VOLUME ["/app/data"]

EXPOSE 8080
ENTRYPOINT ["python", "server.py"]
