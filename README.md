<div align="center">

<img src="web/public/logo.svg" alt="Octopus Logo" width="120" height="120">

### Octopus

**A Simple, Beautiful, and Elegant LLM API Aggregation & Load Balancing Service for Individuals**

 English | [简体中文](README_zh.md)

</div>


## ✨ Features

- 🔀 **Multi-Channel Aggregation** - Connect multiple LLM provider channels with unified management
- 🔑 **Multi-Key Support** - Support multiple API keys for a single channel
- ⚡ **Smart Selection** - Multiple endpoints per channel, smart selection of the endpoint with the shortest delay
- ⚖️ **Load Balancing** - Automatic request distribution for stable and efficient service
- 🔄 **Protocol Conversion** - Seamless conversion between OpenAI Chat / OpenAI Responses / Anthropic API formats
- 💰 **Price Sync** - Automatic model pricing updates
- 🔃 **Model Sync** - Automatic synchronization of available model lists with channels
- 📊 **Analytics** - Comprehensive request statistics, token consumption, and cost tracking
- 🎨 **Elegant UI** - Clean and beautiful web management panel
- 🗄️ **Multi-Database Support** - Support for SQLite, MySQL, PostgreSQL


## 🚀 Quick Start

### 🐳 Docker

Run directly:

```bash
docker run -d --name octopus -v /path/to/data:/app/data -p 8080:8080 bestrui/octopus
```

Or use docker compose:

```bash
wget https://raw.githubusercontent.com/bestruirui/octopus/refs/heads/dev/docker-compose.yml
docker compose up -d
```


### 📦 Download from Release

Download the binary for your platform from [Releases](https://github.com/bestruirui/octopus/releases), then run:

```bash
./octopus start
```

### 🛠️ Build from Source

**Requirements:**
- Go 1.24.4
- Node.js 18+
- pnpm

```bash
# Clone the repository
git clone https://github.com/bestruirui/octopus.git
cd octopus
# Build frontend
cd web && pnpm install && pnpm run build && cd ..
# Move frontend assets to static directory
mv web/out static/
# Start the backend service
go run main.go start 
```

> 💡 **Tip**: The frontend build artifacts are embedded into the Go binary, so you must build the frontend before starting the backend.

**Development Mode**

```bash
cd web && pnpm install && NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:8080" pnpm run dev
## Open a new terminal, start the backend service
go run main.go start
## Access the frontend at
http://localhost:3000
```

### 🔐 Default Credentials

After first launch, visit http://localhost:8080 and log in to the management panel with:

- **Username**: `admin`
- **Password**: `admin`

> ⚠️ **Security Notice**: Please change the default password immediately after first login.

### 📝 Configuration File

The configuration file is located at `data/config.json` by default and is automatically generated on first startup.

**Complete Configuration Example:**

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080
  },
  "database": {
    "type": "sqlite",
    "path": "data/data.db"
  },
  "log": {
    "level": "info"
  }
}
```

**Configuration Options:**

| Option | Description | Default |
|--------|-------------|---------|
| `server.host` | Listen address | `0.0.0.0` |
| `server.port` | Server port | `8080` |
| `database.type` | Database type | `sqlite` |
| `database.path` | Database connection string | `data/data.db` |
| `log.level` | Log level | `info` |

**Database Configuration:**

Three database types are supported:

| Type | `database.type` | `database.path` Format |
|------|-----------------|-----------------------|
| SQLite | `sqlite` | `data/data.db` |
| MySQL | `mysql` | `user:password@tcp(host:port)/dbname` |
| PostgreSQL | `postgres` | `postgresql://user:password@host:port/dbname?sslmode=disable` |

**MySQL Configuration Example:**

```json
{
  "database": {
    "type": "mysql",
    "path": "root:password@tcp(127.0.0.1:3306)/octopus"
  }
}
```

**PostgreSQL Configuration Example:**

```json
{
  "database": {
    "type": "postgres",
    "path": "postgresql://user:password@localhost:5432/octopus?sslmode=disable"
  }
}
```

> 💡 **Tip**: MySQL and PostgreSQL require manual database creation. The application will automatically create the table structure.

### 🌐 Environment Variables

All configuration options can be overridden via environment variables using the format `OCTOPUS_` + configuration path (joined with `_`):

| Environment Variable | Configuration Option |
|---------------------|---------------------|
| `OCTOPUS_SERVER_PORT` | `server.port` |
| `OCTOPUS_SERVER_HOST` | `server.host` |
| `OCTOPUS_DATABASE_TYPE` | `database.type` |
| `OCTOPUS_DATABASE_PATH` | `database.path` |
| `OCTOPUS_LOG_LEVEL` | `log.level` |
| `OCTOPUS_GITHUB_PAT` | For rate limiting when getting the latest version (optional) |
| `OCTOPUS_RELAY_MAX_SSE_EVENT_SIZE` | Maximum SSE event size (optional) |

## 📸 Screenshots

### 🖥️ Desktop

<div align="center">
<table>
<tr>
<td align="center"><b>Dashboard</b></td>
<td align="center"><b>Channel Management</b></td>
<td align="center"><b>Group Management</b></td>
</tr>
<tr>
<td><img src="web/public/screenshot/desktop-home.png" alt="Dashboard" width="400"></td>
<td><img src="web/public/screenshot/desktop-channel.png" alt="Channel" width="400"></td>
<td><img src="web/public/screenshot/desktop-group.png" alt="Group" width="400"></td>
</tr>
<tr>
<td align="center"><b>Price Management</b></td>
<td align="center"><b>Logs</b></td>
<td align="center"><b>Settings</b></td>
</tr>
<tr>
<td><img src="web/public/screenshot/desktop-price.png" alt="Price Management" width="400"></td>
<td><img src="web/public/screenshot/desktop-log.png" alt="Logs" width="400"></td>
<td><img src="web/public/screenshot/desktop-setting.png" alt="Settings" width="400"></td>
</tr>
</table>
</div>

### 📱 Mobile

<div align="center">
<table>
<tr>
<td align="center"><b>Home</b></td>
<td align="center"><b>Channel</b></td>
<td align="center"><b>Group</b></td>
<td align="center"><b>Price</b></td>
<td align="center"><b>Logs</b></td>
<td align="center"><b>Settings</b></td>
</tr>
<tr>
<td><img src="web/public/screenshot/mobile-home.png" alt="Mobile Home" width="140"></td>
<td><img src="web/public/screenshot/mobile-channel.png" alt="Mobile Channel" width="140"></td>
<td><img src="web/public/screenshot/mobile-group.png" alt="Mobile Group" width="140"></td>
<td><img src="web/public/screenshot/mobile-price.png" alt="Mobile Price" width="140"></td>
<td><img src="web/public/screenshot/mobile-log.png" alt="Mobile Logs" width="140"></td>
<td><img src="web/public/screenshot/mobile-setting.png" alt="Mobile Settings" width="140"></td>
</tr>
</table>
</div>


## 📖 Documentation

### 📡 Channel Management

Channels are the basic configuration units for connecting to LLM providers.

**Base URL Guide:**

The program automatically appends API paths based on channel type. You only need to provide the base URL:

| Channel Type | Auto-appended Path | Base URL | Full Request URL Example |
|--------------|-------------------|----------|--------------------------|
| OpenAI Chat | `/chat/completions` | `https://api.openai.com/v1` | `https://api.openai.com/v1/chat/completions` |
| OpenAI Responses | `/responses` | `https://api.openai.com/v1` | `https://api.openai.com/v1/responses` |
| Anthropic | `/messages` | `https://api.anthropic.com/v1` | `https://api.anthropic.com/v1/messages` |
| Gemini | `/models/:model:generateContent` | `https://generativelanguage.googleapis.com/v1beta` | `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent` |

> 💡 **Tip**: No need to include specific API endpoint paths in the Base URL - the program handles this automatically.

---

### 📁 Group Management

Groups aggregate multiple channels into a unified external model name.

**Core Concepts:**

- **Group name** is the model name exposed by the program
- When calling the API, set the `model` parameter to the group name

**Load Balancing Modes:**

| Mode | Description |
|------|-------------|
| 🔄 **Round Robin** | Cycles through channels sequentially for each request |
| 🎲 **Random** | Randomly selects an available channel for each request |
| 🛡️ **Failover** | Prioritizes high-priority channels, switches to lower priority only on failure |
| ⚖️ **Weighted** | Distributes requests based on configured channel weights |

> 💡 **Example**: Create a group named `gpt-4o`, add multiple providers' GPT-4o channels to it, then access all channels via a unified `model: gpt-4o`.

---

### 💰 Price Management

Manage model pricing information in the system.

**Data Sources:**

- The system periodically syncs model pricing data from [models.dev](https://github.com/sst/models.dev)
- When creating a channel, if the channel contains models not in models.dev, the system automatically creates pricing information for those models on this page, so this page displays models that haven't had their prices fetched from upstream, allowing users to set prices manually
- Manual creation of models that exist in models.dev is also supported for custom pricing

**Price Priority:**

| Priority | Source | Description |
|:--------:|--------|-------------|
| 🥇 High | This Page | Prices set by user in price management page |
| 🥈 Low | models.dev | Auto-synced default prices |

> 💡 **Tip**: To override a model's default price, simply set a custom price for it in the price management page.

---

### ⚙️ Settings

Global system configuration.

**Statistics Save Interval (minutes):**

Since the program handles numerous statistics, writing to the database on every request would impact read/write performance. The program uses this strategy:

- Statistics are first stored in **memory**
- Periodically **batch-written** to the database at the configured interval

> ⚠️ **Important**: When exiting the program, use proper shutdown methods (like `Ctrl+C` or sending `SIGTERM` signal) to ensure in-memory statistics are correctly written to the database. **Do NOT use `kill -9` or other forced termination methods**, as this may result in statistics data loss.

---

## 🔌 Client Integration

Octopus exposes your **groups as model IDs**.  
Any group you create (for example:

- `Omni-Intelligence`
- `Flash-Efficiency`
- `Deep Reasoning`
- `Agentic Coder`
- `Multimodal Generation Groups`
- `Audio/Speech Group`
- `The MoE Safety Net`
- `Embeddings-Universal` (recommended for embeddings, see below)

can be used directly as the `model` when calling the API.

### OpenAI SDK (Python)

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8080/v1",
    api_key="sk-octopus-xxxxxxxx",  # Your Octopus API key
)

# High‑quality general assistant
omni = client.chat.completions.create(
    model="Omni-Intelligence",
    messages=[
        {"role": "user", "content": "Explain what Octopus does in one paragraph."},
    ],
)
print(omni.choices[0].message.content)

# Fast and cheap assistant
flash = client.chat.completions.create(
    model="Flash-Efficiency",
    messages=[
        {"role": "user", "content": "Summarize this text quickly."},
    ],
)

# Deep reasoning assistant
reasoning = client.chat.completions.create(
    model="Deep Reasoning",
    messages=[
        {"role": "user", "content": "Solve a tricky reasoning puzzle step by step."},
    ],
)

# Coding assistant
coder = client.chat.completions.create(
    model="Agentic Coder",
    messages=[
        {"role": "user", "content": "Write a Python function to compute Fibonacci numbers."},
    ],
)
```

#### Embeddings with a dedicated group

Create an embedding‑only group (for example `Embeddings-Universal`) and use it with the OpenAI embeddings API:

```bash
curl -sS "http://127.0.0.1:8080/v1/embeddings" \
  -H "Authorization: Bearer sk-octopus-xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Embeddings-Universal",
    "input": "test embedding ping"
  }'
```

### Claude Code / Claude Desktop

Claude tools expect an Anthropic‑style API.  
Point them to Octopus and use group names as models.

Edit `~/.claude/settings.json`:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8080",
    "ANTHROPIC_AUTH_TOKEN": "sk-octopus-xxxxxxxx",
    "API_TIMEOUT_MS": "3000000",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",

    // Map Claude roles onto your Octopus groups
    "ANTHROPIC_MODEL": "Omni-Intelligence",
    "ANTHROPIC_SMALL_FAST_MODEL": "Flash-Efficiency",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "Omni-Intelligence",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "Omni-Intelligence",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "Flash-Efficiency"
  }
}
```

Now Claude Code will send requests to Octopus, which will route them through your groups.

### Codex

Edit `~/.codex/config.toml`:

```toml
# Use your coding group as the model
model = "Agentic Coder"

model_provider = "octopus"

[model_providers.octopus]
name = "octopus"
base_url = "http://127.0.0.1:8080/v1"
```

Edit `~/.codex/auth.json`:

```json
{
  "OPENAI_API_KEY": "sk-octopus-xxxxxxxx"
}
```

### Other OpenAI‑compatible clients (Cursor, VS Code, JetBrains, etc.)

For any client that speaks the OpenAI API (Cursor, many VS Code / JetBrains extensions, etc.):

- Set **Base URL** to `http://127.0.0.1:8080/v1` (or your Octopus URL).
- Set **API Key** to your Octopus API key (`sk-octopus-...`).
- Set **Model** to one of your groups depending on the use‑case:
  - `Omni-Intelligence` – high‑quality general assistant
  - `Flash-Efficiency` – fast & cheap assistant
  - `Deep Reasoning` – long, chain‑of‑thought style tasks
  - `Agentic Coder` – coding work
  - `Multimodal Generation Groups` – image / video capable models
  - `Audio/Speech Group` – TTS / ASR / speech models
  - `Embeddings-Universal` – embeddings (only for `/v1/embeddings`)

---

## 🧩 Recommended Group Presets & Regex

Octopus groups are defined by a **group name** and an optional **regex** used by `ChannelAutoGroup` to attach channel models automatically.

Below are recommended presets based on the models included in `internal/price/presets.go` (sourced from `models.dev`).  
The regexes assume your upstream model IDs follow the same naming (optionally prefixed with a provider like `openai/`, `anthropic/`, `google/`, etc.).

> These are starting points. You can adjust them in the Web UI under **Group Management**.

### 1. Omni‑Intelligence (frontier generalists)

**Use‑case:** highest‑quality general assistant across providers.

**Group name:** `Omni-Intelligence`

**Recommended regex:**

```text
(?i)^((openai\/)?(gpt-5(\.1)?|gpt-4o(-2024-[0-9\-]+)?|gpt-4\.1|gpt-4-turbo|o1(-pro)?|o3(-pro)?)
|(anthropic\/)?(claude-4\.5-(opus|sonnet)|claude-4\.0-opus)
|(google\/)?(gemini-2\.5-pro(-preview.*)?|gemini-3-pro-preview)
|(xai\/)?grok-4
|(alibaba\/)?qwen3-max
|(zhipuai\/)?glm-4\.6)$
```

### 2. Flash‑Efficiency (fast & cheap)

**Use‑case:** low‑latency, low‑cost responses.

**Group name:** `Flash-Efficiency`

**Recommended regex:**

```text
(?i)^(
  (openai\/)?(gpt-5-(nano|mini)|gpt-4\.1-nano|gpt-4o-mini|gpt-3\.5-turbo)
  |(google\/)?gemini-(1\.5-flash(-8b)?|2\.0-flash(-lite)?|2\.5-flash(-lite)?(-preview.*)?|flash-latest|flash-lite-latest)
  |(xai\/)?(grok-3-mini(-fast)?|grok-3-fast(-latest)?|grok-4-fast(-non-reasoning)?)
  |(alibaba\/)?(qwen-flash|qwen-omni-turbo(-realtime)?|qwen3-omni-flash)
)$
```

### 3. Deep Reasoning

**Use‑case:** tasks where you want explicit, heavier reasoning.

**Group name:** `Deep Reasoning`

**Recommended regex:**

```text
(?i)^(
  (openai\/)?(o1(-mini|-preview|-pro)?|o3(-mini|-pro)?|o3-deep-research|o4-mini-deep-research)
  |(deepseek\/)?deepseek-reasoner
  |(moonshotai\/)?kimi-k2-thinking(-turbo)?
  |(alibaba\/)?qwen3-next-80b-a3b-thinking
)$
```

### 4. Agentic Coder

**Use‑case:** coding, refactors, code review, tool‑heavy agents.

**Group name:** `Agentic Coder`

**Recommended regex:**

```text
(?i)^(
  (openai\/)?(gpt-5(\.1)?-codex(-mini)?|codex-mini-latest)
  |(xai\/)?grok-code-fast-1
  |(alibaba\/)?qwen3-coder.*
)$
```

### 5. Multimodal Generation Groups (vision / image / video)

**Use‑case:** models with strong vision or image generation.

**Group name:** `Multimodal Generation Groups`

**Recommended regex:**

```text
(?i)^(
  (google\/)?(gemini-2\.5-flash-image(-preview)?|gemini-.*-vision.*)
  |(xai\/)?grok-(vision-beta|2-vision(-latest|-1212)?)
  |(alibaba\/)?(qwen-vl-.*|qwen2-5-vl-.*|qwen3-vl-.*)
  |wan2\.2(-i2v|-t2v)?
  |sora
  |kling
  |runway
)$
```

### 6. Audio/Speech Group (ASR, TTS, live)

**Use‑case:** speech‑to‑text, text‑to‑speech, live translation.

**Group name:** `Audio/Speech Group`

**Recommended regex:**

```text
(?i)^(
  (google\/)?(gemini-.*-tts|gemini-live-.*)
  |(alibaba\/)?(qwen3-asr-flash|qwen3-livetranslate-flash-realtime)
  |cosyvoice
  |fish-speech
  |telespeech
  |sensevoice
)$
```

### 7. The MoE Safety Net (fallback pool)

**Use‑case:** diverse backup pool when primary groups fail.

**Group name:** `The MoE Safety Net`

**Recommended regex:**

```text
(?i)^(
  (alibaba\/)?(qwen2-5-.*|qwen3-(?!coder).*)
  |(zhipuai\/)?glm-4(\.5|\.6).*
  |mixtral
  |(minimax\/)?minimax-m2
  |(moonshotai\/)?kimi-k2-.*
)$
```

### 8. Embeddings‑Universal (for `/v1/embeddings`)

**Use‑case:** text embeddings for RAG, search, clustering.

**Group name:** `Embeddings-Universal`

**Actual embedding models present in this repo (from `presets.go`):**

- `text-embedding-3-small` (OpenAI)
- `text-embedding-3-large` (OpenAI)
- `text-embedding-ada-002` (OpenAI)
- `gemini-embedding-001` (Google)

**Recommended regex:**

```text
(?i)^(
  (openai\/)?text-embedding-(3-(small|large)|ada-002)
  |(google\/)?gemini-embedding-001
)$
```

Use this group as the `model` when calling `/v1/embeddings`.  
Make sure at least one **channel** in this group is configured with an **OpenAI Embeddings‑compatible endpoint** (channel type `OpenAIEmbedding` in the code).

---

## 🐛 Debugging `/v1/embeddings` Failures

The embedding path is handled by:

- Inbound: `InboundTypeOpenAIEmbedding` → `EmbeddingInbound`
- Outbound: only channels whose `Type` is `OutboundTypeOpenAIEmbedding`

A typical failure flow:

1. `model` (group name) is found.
2. Group items reference channels that are **chat‑only**, not embedding‑capable.
3. In `internal/relay/relay.go` the request is rejected for each channel:

   ```go
   if internalRequest.IsEmbeddingRequest() && !outbound.IsEmbeddingChannelType(channel.Type) {
       log.Warnf("channel type %d is not compatible with embedding request for channel: %s", channel.Type, channel.Name)
       // ...
   }
   ```

4. After all channels fail, Octopus returns `502 Bad Gateway`.

We improved the final error message to include the last error:

```go
// 所有通道都失败
metrics.Save(c.Request.Context(), false, lastErr)
msg := "all channels failed"
if lastErr != nil {
    msg = fmt.Sprintf("all channels failed: %v", lastErr)
}
resp.Error(c, http.StatusBadGateway, msg)
```

So now the JSON error body will help you distinguish:

- `no available channel` – group exists but has no enabled items.
- `model not supported` – API key restrictions or unsupported model name.
- `channel type X not compatible with embedding request` – group points only to chat channels.
- `upstream error: 4xx/5xx: ...` – provider‑side failure (bad key, quota exceeded, etc.).

To debug:

1. **Check logs** around the time of the request:
   - Look for `request model ... forwarding to channel ...` lines.
   - Look for `channel type X is not compatible with embedding request` or `upstream error: ...`.

2. **Confirm group wiring** in the Web UI:
   - Ensure `Embeddings-Universal` exists.
   - Ensure it has at least one **channel item** pointing at a real embedding model (`text-embedding-3-small`, etc.).
   - Ensure that channel’s **Type** is set to an embedding‑compatible type (OpenAI embeddings).

3. **Re‑test**:

   ```bash
   curl -sS "http://127.0.0.1:8080/v1/embeddings" \
     -H "Authorization: Bearer sk-octopus-xxxxxxxx" \
     -H "Content-Type: application/json" \
     -d '{
       "model": "Embeddings-Universal",
       "input": "diagnostic ping"
     }'
   ```

If it still fails, the log lines plus the enriched `all channels failed: ...` message will tell you exactly whether it's a routing issue (no embedding channel), configuration issue (wrong model name / group), or upstream HTTP error.

---

## 🤝 Acknowledgments

- 🙏 [looplj/axonhub](https://github.com/looplj/axonhub) - The LLM API adaptation module in this project is directly derived from this repository
- 📊 [sst/models.dev](https://github.com/sst/models.dev) - AI model database providing model pricing data

