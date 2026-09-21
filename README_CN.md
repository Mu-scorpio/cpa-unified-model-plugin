# Unified Model · CLIProxyAPI 统一模型插件

[English](README.md) · [简体中文](README_CN.md)

**给每个客户端一个稳定的模型名，并把其余的全部藏起来。**

这是一个即插即用的 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 插件：把杂乱的原生目录（`gpt-5.6-luna`、`qwen3.8-flash`、`claude-sonnet-4` ……）收成你自己起的别名。客户端永远请求 `Port-pro`；背后用哪家凭证、哪个上游模型、多大上下文窗口，都由你决定。

[![Release](https://img.shields.io/github/v/release/Mu-scorpio/cpa-unified-model-plugin?style=flat-square)](https://github.com/Mu-scorpio/cpa-unified-model-plugin/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)

<p align="center">
  <img src="docs/screenshots/management-light.png" alt="统一模型管理页 · 浅色" width="900" />
</p>

<p align="center"><em>管理页 · 浅色。供应商色条、实时凭证下拉、一键增强模式。</em></p>

<p align="center">
  <img src="docs/screenshots/management-dark.png" alt="统一模型管理页 · 深色" width="900" />
</p>

---

## 它解决什么

| 没有本插件 | 有了本插件 |
| --- | --- |
| 客户端看到 40+ 个原生模型 | 客户端只看到 `Port-pro`、`Port-flash`…… |
| Codex 挂了就要改每个客户端 | 改个别名的目标模型，客户端继续叫 `Port-pro` |
| 在所有 Codex 登录之间轮询 | 把别名钉到某一个账号 |
| `/v1/models` 泄漏全部上游名字 | **增强模式**隐藏原生模型，只露出别名 |

不改 CLIProxyAPI 核心，只用公开的插件 ABI。

---

## 亮点

<p align="center">
  <img src="docs/screenshots/toolbar.png" alt="增强模式开关" width="900" />
</p>

**增强模式** —— 一个勾选框。OpenAI `/v1/models`、Anthropic `/v1/models`、Gemini 列表、Codex 客户端目录、Grok Shell 全部收缩成你配置的别名。原生模型仍可用于*路由*，只是不再对外展示。

<p align="center">
  <img src="docs/screenshots/mapping-card.png" alt="别名卡片" width="900" />
</p>

**每条映射一张卡片**

- **供应商 / 凭证** —— 只列出实际已登录的 OAuth 账号、API Key、OpenAI 兼容提供商。没配凭证的供应商不会出现。
- **钉死凭证** —— 选 `codex · user@example.com` 就只用这一份登录；选 `codex（全部凭证）` 则走 CPA 原生轮询。
- **上下文长度** —— 内置模型表自动填充，可按别名覆盖，也可写回共享表。
- **输入模态** —— 独立声明 `文本` / `图像` / `音频` / `视频`。
- **思考后缀** —— `Port-pro(high)` 仍然匹配 `Port-pro`，并转发给 `gpt-5.6-luna(high)`。

完整字段说明、安装步骤与可移植性见 [英文 README](README.md)。

## 最快安装

从 [最新 Release](https://github.com/Mu-scorpio/cpa-unified-model-plugin/releases/latest) 下载对应平台的资源，放到：

```text
<CPA>/plugins/<GOOS>/<GOARCH>/unified-model-v0.4.1.<so|dylib|dll>
```

文件名必须带版本号，CPA 才会热重载。然后在 `config.yaml` 里启用：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    unified-model:
      enabled: true
      enhanced-mode: true
      mappings:
        - name: Port-pro
          target: gpt-5.6-luna
          provider: codex
```

打开管理中心的 **统一模型** 页面即可增删改别名。
