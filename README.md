# Wake on LAN Proxy

A small Go service that wakes a sleeping device (via Wake-on-LAN) before forwarding requests to it.

This is useful when you want to access a powerful machine without keeping it running 24/7.
For example, you might have a lightweight Linux home server that wakes your gaming PC on demand so you can use its graphics card - such as running a large language model through the [Ollama API](https://ollama.com/).

## Features
- Reads configuration from `.env`
- Sends WoL magic packets (no external `wakeonlan` binary required)
- Polls the target port until it's reachable, with configurable timeout
- Forwards requests to the target host

## Quickstart
1. Copy `.env.example` to `.env` and edit values.
2. Build & run with Docker Compose:

```bash
docker compose up -d --build
```

## Env vars
See `.env.example`. Required: `DEVICE_MAC`, `DEVICE_IP`, `DEVICE_PORT`.

## Notes
- This proxy waits for the target TCP port to accept connections before forwarding. If your target instance requires extra warmup time after the TCP socket opens, consider increasing `WAKE_TIMEOUT_SEC`.
- The service attempts to send the magic packet both to the device IP and to `255.255.255.255:9`.
