# Ollama Waker

A small Go service that wakes a sleeping gaming PC (via Wake-on-LAN) before forwarding requests to a self-hosted Ollama instance. Useful when you want an always-on proxy that ensures your remote LLM host is awake before Open WebUI or other clients send requests.

## Features
- Reads configuration from `.env`
- Sends WoL magic packets (no external `wakeonlan` binary required)
- Polls the Ollama port until it's reachable, with configurable timeout
- Forwards requests (including streaming) to the Ollama host

## Quickstart
1. Copy `.env.example` to `.env` and edit values.
2. Build & run with Docker Compose:

```bash
docker compose up -d --build
```

3. Point your Open WebUI Ollama provider to the host running this service (e.g., `http://home-server:11434`).

## Env vars
See `.env.example`. Required: `DEVICE_MAC`, `DEVICE_IP`, `DEVICE_PORT`.

## Notes
- This proxy waits for the Ollama TCP port to accept connections before forwarding. If your Ollama instance requires extra warmup time after the TCP socket opens, consider increasing `WAKE_TIMEOUT_SEC`.
- The service attempts to send the magic packet both to the device IP and to `255.255.255.255:9`.
