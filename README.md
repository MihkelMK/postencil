# postencil

A lightweight, transparent webhook proxy that renders [Go templates](https://pkg.go.dev/text/template) in request fields before forwarding to a target URL.

By default postencil is a pure pass-through proxy. It touches nothing.\
Template rendering is opt-in, per field, via environment variables.\
Every feature that could affect your webhooks requires an explicit decision to enable it.

---

## Use case

Some notification services (like [ntfy](https://ntfy.sh)) support template rendering in certain fields but not others.\
postencil fills that gap by rendering templates in any field you choose before the request reaches its destination.

### Example: Forgejo → ntfy with updating notifications

ntfy can update an existing notification in-place if you send the same `X-ID`.\
By putting a template in the webhook URL's query params, each PR gets a stable, deterministic ID.\
This way updates replace the existing notification rather than creating a new one, and closing a PR can delete it.

```
# Forgejo webhook URL
http://postencil:8080/my-topic?X-ID=forgejo-pr-{{.repository.full_name}}-{{.pull_request.number}}
```

```yaml
# postencil config (only X-ID is rendered, everything else is untouched)
TEMPLATE_QUERY_PARAMS: "X-ID"
TARGET_URL: "https://ntfy.example.com"
```

**postencil is target-agnostic**. It knows nothing about Forgejo or ntfy specifically.

---

## How it works

```
Webhook source → postencil → target
                    │
                    ├── parse JSON body (only if templating is enabled)
                    ├── render templates in configured query params
                    ├── render templates in configured headers
                    ├── render templates in body (if enabled)
                    └── forward with original method, path, and response streamed back
```

Templates use Go's `text/template` syntax, the same engine ntfy uses for its own title/body templating, so any template that works there works identically here.

---

## Getting started

### Docker Compose

```yaml
services:
  postencil:
    image: ghcr.io/MihkelMK/postencil:latest
    ports:
      - "8080:8080"
    environment:
      TARGET_URL: "https://ntfy.example.com"
      TEMPLATE_QUERY_PARAMS: "X-ID"
```

### Build from source

```bash
git clone https://github.com/MihkelMK/postencil
cd postencil
make build
./bin/postencil
```

Requires Go 1.26+.

---

## Configuration

All configuration is via environment variables. The only required variable is `TARGET_URL`.

### Core

| Variable      | Default | Description                                    |
| ------------- | ------- | ---------------------------------------------- |
| `TARGET_URL`  | -       | **Required.** Base URL to forward requests to. |
| `LISTEN_ADDR` | `:8080` | Address and port to listen on.                 |

### Template rendering

All template options default to `false`. postencil is fully transparent until you opt in.

Each field-level option accepts one of three values:

| Value         | Behaviour                                                              |
| ------------- | ---------------------------------------------------------------------- |
| `false`       | Disabled. The field is forwarded untouched.                            |
| `true`        | All keys in this field are rendered as Go templates.                   |
| `"Key1,Key2"` | Only the named keys are rendered. Case-sensitive, no alias resolution. |

| Variable                | Default | Description                                |
| ----------------------- | ------- | ------------------------------------------ |
| `TEMPLATE_QUERY_PARAMS` | `false` | Query parameters to render.                |
| `TEMPLATE_HEADERS`      | `false` | Request headers to render.                 |
| `TEMPLATE_BODY`         | `false` | Whether to render the entire request body. |

**On alias resolution:**\
ntfy and other services often have aliases for the same field (e.g. `X-ID`, `id`, `x-id`).\
postencil does not resolve these. If you use `X-ID` in your webhook URL, put `X-ID` in the env var.

### Error handling

| Variable                     | Default | Description                                                                                                              |
| ---------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------ |
| `TEMPLATE_ERROR_PASSTHROUGH` | `true`  | On template failure, log a warning and forward the original unmodified value. Set to `false` to return HTTP 400 instead. |

The default preserves the transparent-by-default principle: a broken template should not silently drop your webhook.

### Logging

| Variable                | Default         | Description                                                                               |
| ----------------------- | --------------- | ----------------------------------------------------------------------------------------- |
| `LOG_LEVEL`             | `info`          | Minimum log level. One of `debug`, `info`, `warn`, `error`.                               |
| `CENSOR_AUTH_TOKENS`    | `true`          | Redact auth-related headers and query params from logs.                                   |
| `CENSORED_HEADERS`      | `Authorization` | Comma-separated list of headers to redact. Only used when `CENSOR_AUTH_TOKENS=true`.      |
| `CENSORED_QUERY_PARAMS` | `auth,token`    | Comma-separated list of query params to redact. Only used when `CENSOR_AUTH_TOKENS=true`. |

At `info` level, postencil logs incoming request URL and sender, and the target's response status.\
At `debug` level, request and response headers are also logged (subject to censoring).

---

## Template syntax

Templates use Go's [`text/template`](https://pkg.go.dev/text/template) package with [Sprig](https://masterminds.github.io/sprig/) functions available.\
The dot context (`.`) is the parsed JSON body of the incoming request.

```go
# Simple field access
{{.field}}

# Nested field
{{.repository.full_name}}

# With conditionals
{{if eq .action "opened"}}opened{{else}}updated{{end}}
```

If a template references a key that doesn't exist in the JSON body, rendering fails and postencil falls back to the original value (with `TEMPLATE_ERROR_PASSTHROUGH=true`) or returns a 400 (with `TEMPLATE_ERROR_PASSTHROUGH=false`).

---

## Development

```bash
make build        # compile to bin/postencil
make test         # run all tests
make test-verbose # run tests with output
make lint         # run golangci-lint
make fmt          # run goimports
make docker       # build docker image
```

### Project layout

```
postencil/
├── cmd/postencil/      # entrypoint only
├── internal/
│   ├── config/         # env var loading and validation
│   ├── fieldset/       # none / all / specific-keys type
│   ├── proxy/          # HTTP handler
│   └── tmpl/           # Go template rendering
├── Dockerfile
└── Makefile
```

---

## License

[GPLv3](./LICENSE)
