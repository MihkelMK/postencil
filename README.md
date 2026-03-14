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

ntfy can update an existing notification in-place if you send the same `sequence-id`.\
By putting a template in the webhook URL's query params, each PR gets a stable, deterministic ID.\
This way updates replace the existing notification rather than creating a new one, and closing a PR can delete it.

```
# Forgejo webhook URL
http://postencil:8080/my-topic?sequence-id=forgejo-pr-{{ .repository.full_name | replace "/" "_" }}-{{ .pull_request.number }}
```

The `replace` is needed for ntfy. Having a literal or URL encoded `/` for `sequence-id` results in a failed request.

```yaml
# postencil config (only sequence-id is rendered, everything else is untouched)
TEMPLATE_QUERY_PARAMS: "sequence-id"
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
                    ├── render method template (if set)
                    ├── render path template (if set)
                    └── forward with response streamed back
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
      TEMPLATE_QUERY_PARAMS: "sequence-id"
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

| Variable                | Default | Description                                                                                                             |
| ----------------------- | ------- | ----------------------------------------------------------------------------------------------------------------------- |
| `TEMPLATE_QUERY_PARAMS` | `false` | Query parameters to render.                                                                                             |
| `TEMPLATE_HEADERS`      | `false` | Request headers to render.                                                                                              |
| `TEMPLATE_BODY`         | `false` | Whether to render the entire request body.                                                                              |
| `TEMPLATE_METHOD`       | -       | Go template for the forwarded HTTP method. Empty means use the incoming method.                                         |
| `TEMPLATE_PATH`         | -       | Go template for the forwarded request path. Empty means use the incoming path. Must render to a path starting with `/`. |

**On alias resolution:**\
ntfy and other services often have aliases for the same field (e.g. querry param `sequence-id` and headers `X-Sequence-ID`, `SEQUENCE-ID`, `SID`).\
postencil does not resolve these. If you use `sequence-id` in your webhook URL, put `sequence-id` in the env var.

### Error handling

| Variable          | Default | Description                                                                                                                |
| ----------------- | ------- | -------------------------------------------------------------------------------------------------------------------------- |
| `TEMPLATE_STRICT` | `false` | On template failure, log a warning and forward the original unmodified value.<br>Set to `true` to return HTTP 400 instead. |

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

```
{{.field}}                                            # simple field access
{{.repository.full_name}}                             # nested field
{{.repository.full_name | replace "/" "_"}}           # Sprig function
{{if eq .action "opened"}}opened{{else}}updated{{end}} # conditional
```

If a template references a key that doesn't exist in the JSON body, rendering fails and postencil falls back to the original value (with `TEMPLATE_STRICT=false`) or returns a 400 (with `TEMPLATE_STRICT=true`).

### Request metadata

When any templating is enabled, postencil injects the following under the top-level `.request` key, accessible in any template regardless of which field is being rendered:

| Key                            | Value                                          |
| ------------------------------ | ---------------------------------------------- |
| `.request.method`              | Incoming HTTP method (`GET`, `POST`, …)        |
| `.request.path`                | Incoming request path, URL-decoded             |
| `.request.params.KEY`          | Query parameter KEY (first value, URL-decoded) |
| `.request.headers.Header-Name` | Request header value                           |

Example — override the HTTP method based on a query parameter:

```yaml
# Forgejo sends all webhook events as POST. ntfy DISMISS requires DELETE.
# Use TEMPLATE_METHOD to map the Forgejo "action" param to the right method.
TEMPLATE_METHOD: '{{if eq .request.params.action "close"}}DELETE{{else}}POST{{end}}'
```

> **Conflict:** if the JSON body has a top-level `"request"` key it is overwritten by the injected metadata. postencil logs a warning. With `TEMPLATE_STRICT=true` it returns HTTP 400 instead.

### Limitation: quoted string literals inside JSON body templates

When `TEMPLATE_BODY=true`, the request body is rendered as a template. Template actions inside JSON string values cannot contain quoted string literals, because inner `"` characters would break JSON syntax.

```
# This is invalid JSON — the inner quotes break the string:
{"topic":"{{index .request.params "event"}}"}

# Use dot notation instead (works when the key has no hyphens):
{"topic":"{{.request.params.event}}"}
```

For hyphenated param names (e.g. `sequence-id`) inside a body template, use `TEMPLATE_QUERY_PARAMS` to render the value as a query param rather than embedding it in the body.

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
