# Backend Configuration

## LiteLLM Proxy

`litellm.example.yaml` captures the MVP baseline for the LiteLLM Proxy backend:

- `store_model_in_db: true` is required so runtime model updates can be coordinated through LiteLLM's database-backed `/model/new` API.
- LiteLLM remains a Python + PostgreSQL dependency and is not part of the Go single-binary target.
- For the private/local deployment path, LiteLLM is expected to退场 in favor of the Go direct backend described in ADR-0001 D3.
