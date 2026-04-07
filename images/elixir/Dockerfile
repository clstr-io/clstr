FROM elixir:1.19

RUN apt-get update && \
    apt-get install -y --no-install-recommends iptables iproute2 && \
    rm -rf /var/lib/apt/lists/*

RUN mix local.hex --force && mix local.rebar --force

WORKDIR /app

COPY mix.exs mix.lock* .
RUN MIX_ENV=prod mix deps.get --only prod

COPY . .
RUN MIX_ENV=prod mix release

VOLUME ["/app/data"]

EXPOSE 8080
ENTRYPOINT ["./_build/prod/rel/server/bin/server", "start"]
