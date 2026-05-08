# OpenAI-Compatible Endpoints — Adoption & Configuration

boabot's `openai` provider type works with any HTTP endpoint that implements the OpenAI Chat Completions API (`POST /v1/chat/completions`). This covers:

- **OpenAI** — GPT-4o, GPT-4o-mini, o1, o3, etc.
- **Ollama** — local models (Llama 3, Mistral, Qwen, Phi, etc.) with no API key required
- **vLLM** — self-hosted open-weight models at scale
- **OpenRouter** — unified gateway to many providers under a single key
- **Azure OpenAI** — Microsoft-hosted OpenAI models
- **LM Studio**, **LocalAI**, and other OpenAI-compatible local runtimes

The provider also supports `reasoning_content` in responses, so thinking models (Qwen3, DeepSeek) surface their chain-of-thought output correctly.

---

## Configuration shape

```yaml
models:
  default: my-provider

  providers:
    - name: my-provider
      type: openai
      endpoint: "https://api.openai.com/v1"   # base URL, no trailing slash
      model_id: "gpt-4o"
```

`endpoint` is the base URL. The provider appends `/chat/completions` automatically.

---

## OpenAI (api.openai.com)

```yaml
providers:
  - name: openai-gpt4o
    type: openai
    endpoint: "https://api.openai.com/v1"
    model_id: "gpt-4o"
```

Set the API key as an environment variable or in the credentials file:

```bash
export OPENAI_API_KEY=sk-...
```

```yaml
# ~/.boabot/credentials.yaml  (chmod 600)
openai_api_key: "sk-..."
```

The provider reads `OPENAI_API_KEY` automatically via the standard `Authorization: Bearer` header.

---

## Ollama (local, no API key)

Run models locally with zero cloud dependency. Ollama serves an OpenAI-compatible API on `localhost:11434`.

```bash
# Install Ollama, then pull a model
ollama pull llama3.2
ollama serve   # starts on http://localhost:11434
```

```yaml
providers:
  - name: ollama-llama3
    type: openai
    endpoint: "http://localhost:11434/v1"
    model_id: "llama3.2"
```

No API key is needed for local Ollama. Set `OPENAI_API_KEY=ollama` if a tool requires it to be non-empty — Ollama ignores the value.

---

## vLLM (self-hosted)

vLLM exposes an OpenAI-compatible API on port 8000 by default.

```yaml
providers:
  - name: vllm-mistral
    type: openai
    endpoint: "http://vllm-host:8000/v1"
    model_id: "mistralai/Mistral-7B-Instruct-v0.3"
```

Set an API key if your vLLM instance is configured with `--api-key`:

```bash
export OPENAI_API_KEY=your-vllm-key
```

---

## OpenRouter

OpenRouter provides a single endpoint with access to many models from different providers.

```yaml
providers:
  - name: openrouter-claude
    type: openai
    endpoint: "https://openrouter.ai/api/v1"
    model_id: "anthropic/claude-sonnet-4-6"

  - name: openrouter-llama
    type: openai
    endpoint: "https://openrouter.ai/api/v1"
    model_id: "meta-llama/llama-3.1-70b-instruct"
```

```bash
export OPENAI_API_KEY=sk-or-...   # your OpenRouter key
```

---

## Azure OpenAI

Azure OpenAI uses a different URL structure: the model is identified by a deployment name, not a model ID, and the endpoint is scoped to your Azure resource.

```yaml
providers:
  - name: azure-gpt4o
    type: openai
    endpoint: "https://<your-resource>.openai.azure.com/openai/deployments/<deployment-name>"
    model_id: "gpt-4o"   # must match the deployment's base model
```

```bash
export OPENAI_API_KEY=<azure-api-key>
```

Note: Azure requires `api-version` as a query parameter, which the current provider does not append. Azure OpenAI works best when accessed through OpenAI-compatible proxies that handle the versioning (e.g., LiteLLM proxy).

---

## Embeddings (vector search)

The `openai` provider type also supports embeddings for the vector memory store. Configure it separately under `memory.embedder`:

```yaml
memory:
  embedder: embeddings-provider   # must match the name of an openai-type provider

models:
  providers:
    - name: embeddings-provider
      type: openai
      endpoint: "https://api.openai.com/v1"
      model_id: "text-embedding-3-small"
```

Only `openai`-type providers support embeddings. If `memory.embedder` is not set or refers to a non-openai provider, boabot falls back to the built-in BM25 embedder (no API calls required).

---

## Multiple providers and routing

Mix and match providers to balance cost, latency, and capability:

```yaml
models:
  default: claude-sonnet          # heavy reasoning tasks use Claude via Anthropic
  chat_provider: ollama-llama3    # interactive chat uses a local model

  providers:
    - name: claude-sonnet
      type: anthropic
      model_id: claude-sonnet-4-6

    - name: ollama-llama3
      type: openai
      endpoint: "http://localhost:11434/v1"
      model_id: "llama3.2"
```

`chat_provider` overrides `default` for tasks sourced from the chat interface. This lets you run a fast local model for conversational interaction while keeping a capable cloud model for background tasks and board work.
