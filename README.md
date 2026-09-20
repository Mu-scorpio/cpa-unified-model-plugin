# Unified Model Plugin

This plugin adds stable client-facing model aliases to CLIProxyAPI. It registers the following mapping by default:

```yaml
mappings:
  - name: Port
    target: gpt-5.5
    provider: codex
```

Clients can always request `Port`. When `provider` is set to an available built-in provider, the plugin uses a direct model-router decision: CLIProxyAPI keeps its native OAuth/auth, protocol translation, and usage path, while the request model is replaced with `gpt-5.5` without a nested plugin execution callback or second upstream request. The provider value is optional; leaving it empty keeps the provider-agnostic compatibility fallback, which uses the host model callback.

## Installation

Build the macOS Apple Silicon plugin from the repository root:

```bash
mkdir -p plugins/darwin/arm64
(cd plugins/unified-model/go && go build -buildmode=c-shared -o ../../darwin/arm64/unified-model.dylib .)
```

Enable plugins and add the mapping in `config.yaml`:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    unified-model:
      enabled: true
      priority: 100
      mappings:
        - name: Port
          target: gpt-5.5
          provider: codex
```

After restarting CLIProxyAPI, open `Unified Model` from the plugin resource menu in Management Center. The page can add, remove, and update aliases; saving a change does not require a restart. Provider keys are the built-in CLIProxyAPI keys such as `codex`, `claude`, `gemini`, or an available `openai-compatible-*` provider.

The plugin uses the native CLIProxyAPI plugin ABI, so the build GOOS/GOARCH must match the server. The resource page saves through the Management API and therefore requires a valid Management key.
