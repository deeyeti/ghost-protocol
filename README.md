# Ghost Protocol CLI

> A lightweight local reverse-proxy daemon for LLMs — cuts cloud API costs, enforces privacy, and routes intelligently between local and cloud models.

```
  Your IDE / App
       │  POST localhost:8080/v1/chat/completions
       ▼
  ┌─────────────────────────────────────────┐
  │          Ghost Protocol Proxy           │
  │                                         │
  │  1. Redaction   →  scrub secrets/PII    │
  │  2. Cache       →  hit? return ~0ms     │
  │  3. Classifier  →  trivial or complex?  │
  │  4. Router      →  local Ollama / cloud │
  │  5. Restore     →  un-redact response   │
  └─────────────────────────────────────────┘
         │                     │
   Ollama (local)        OpenAI / Anthropic
```

## Features

| Feature | Description |
|---|---|
| 🔒 **Redaction** | Regex-based scrubbing of API keys, SSNs, emails, IPs before any cloud call |
| ⚡ **Semantic Cache** | Cosine-similarity search over stored embeddings — same/similar prompts returned instantly |
| 🧠 **Smart Routing** | Trivial tasks → local Ollama (free). Complex tasks → cloud (paid). Heuristics + LLM classifier |
| 🔄 **Failover** | Cloud down? Falls back to local. Local down? Falls back to cloud. |
| 📊 **Request Logging** | Every request logged to SQLite with route destination and latency |
| 🚀 **Single Binary** | One `go build` produces a self-contained executable |

## Prerequisites

- **Go ≥ 1.21** — [go.dev/dl](https://go.dev/dl/)
- **Ollama** running locally with (and more):
  ```bash
  ollama pull llama3
  ollama pull nomic-embed-text
  ollama pull qwen2:0.5b
  ```
- An **OpenAI or Anthropic API key**

## Quickstart

```bash
# 1. Clone
git clone https://github.com/deeyeti/ghost-protocol.git
cd ghost-protocol

# 2. Configure
cp config.example.json config.json
# Edit config.json — add your API key and adjust models

# 3. Build
go mod tidy
go build -o ghost-protocol .

# 4. Run
./ghost-protocol

# 5. Point your IDE at localhost:8080
# In Cursor / VS Code Copilot / any OpenAI-compatible client:
#   Base URL → http://localhost:8080
```

## Config Reference

| Key | Description | Default |
|---|---|---|
| `proxy.port` | Listening port | `8080` |
| `cloud.api_key` | Your cloud provider API key | — |
| `cloud.model` | Cloud model to use for complex tasks | `gpt-4o` |
| `local.model` | Local Ollama model for trivial tasks | `llama3` |
| `local.embed_model` | Model used to generate embeddings for the cache | `nomic-embed-text` |
| `local.classifier_model` | Tiny model used to classify prompt complexity | `qwen2:0.5b` |
| `cache.similarity_threshold` | Cosine similarity cutoff for cache hits (0-1) | `0.92` |
| `routing.trivial_max_prompt_len` | Prompts shorter than this are heuristically classified as trivial | `300` |

## Architecture

```
ghost-protocol/
├── main.go               Entry point + CLI flags + graceful shutdown
├── config/config.go      JSON config loader
├── proxy/proxy.go        HTTP server + full request pipeline
├── redaction/redactor.go Regex scrubbing + per-request token sessions
├── cache/cache.go        Cosine similarity search + SQLite backing
├── classifier/           Heuristic + LLM-based complexity scoring
├── router/router.go      Local/cloud dispatch + failover
└── db/db.go              SQLite schema + request logging
```

## Response Headers

Ghost Protocol auto adds diagnostic headers to every response:

| Header | Values |
|---|---|
| `X-Ghost-Protocol-Source` | `cache` · `local` · `cloud` |
| `X-Ghost-Protocol-Complexity` | `trivial` · `complex` |

## License

MIT
