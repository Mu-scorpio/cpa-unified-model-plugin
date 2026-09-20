# Unified Model Plugin for CLIProxyAPI

This plugin adds stable client-facing model aliases to CLIProxyAPI. It registers the following mapping by default:

```yaml
mappings:
  - name: Port
    target: gpt-5.5
    provider: codex
```

Clients can always request `Port`. When `provider` is set to an available built-in provider or OpenAI-compatible provider, the plugin uses a direct model-router decision: CLIProxyAPI keeps its native OAuth/auth, protocol translation, and usage path, while the request model is replaced with the target model without a nested plugin execution callback or second upstream request. The provider value is optional; leaving it empty keeps the provider-agnostic compatibility fallback, which uses the host model callback.

## Build and Installation

### Option 1: Using `build.sh`

Build the plugin and automatically install it into CPA (assuming CPA is located at `../CPA` or set via `CPA_DIR`):

```bash
./build.sh
```

### Option 2: Manual Build

Build the macOS Apple Silicon plugin:

```bash
mkdir -p bin/darwin/arm64
(cd go && go build -buildmode=c-shared -o ../bin/darwin/arm64/unified-model.dylib .)
```

Then copy `unified-model.dylib` to your CPA's `plugins/<GOOS>/<GOARCH>/` directory.

## Configuration

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

## Management UI

After restarting CLIProxyAPI, open `Unified Model` from the plugin resource menu in Management Center. The page automatically reads available providers and models from CLIProxyAPI, allowing you to select a provider first and then choose from its available models (or enter custom ones). The page can add, remove, and update aliases; saving a change takes effect immediately without requiring a restart.

The plugin uses the native CLIProxyAPI plugin ABI, so the build GOOS/GOARCH must match the server. The resource page saves through the Management API and therefore requires a valid Management key.
