# Development notes

For people changing the code. Users should read the README.

Sister project of [opendeck-hue](https://github.com/l-lemaire/opendeck-hue),
built the same way. `internal/openaction`, `internal/secrets` and
`internal/config` are copies from there with names adapted, as are the DNS
codec, the SSE parser and the HTTP debug logger in `internal/nanoleaf`.
Shared code is copied, not imported, so each plugin stays self-contained.

## Layout

```
cmd/nanoleaf/            the CLI
cmd/opendeck-nanoleaf/   the plugin: settings, inspector requests, toggling, live state
plugin/                  manifest, SVG icons, property inspector (HTML/CSS/JS, no build)
internal/nanoleaf/       device client: mDNS discovery, token request, state, events
internal/nanoleaf/nanoleaftest/  a fake device (HTTP, pairing mode, state, events)
internal/pairing/        the pairing flow shared by CLI and plugin
internal/openaction/     plugin-side client for the OpenDeck / Stream Deck protocol
internal/secrets/        credential store: keyring with a 0600 file fallback
internal/config/         ~/.config/nanoleaf/config.json: devices, default
```

Dependencies: `github.com/zalando/go-keyring` and `github.com/coder/websocket`.
Everything else is standard library. Binaries are static (CGO disabled).

## Make targets

```
make build              # bin/nanoleaf for this machine
make check              # gofmt, go vet, all tests (no device or deck needed)
make plugin-install     # build the plugin and copy it into ~/.config/opendeck/plugins
make opendeck-restart   # OpenDeck loads plugins at startup
make plugin-log         # follow OpenDeck's log and the plugin's log
make plugin-release     # plugin zip + CLI archives for every platform; version from the git tag
```

## The Nanoleaf local API, as used here

- Discovery: mDNS `_nanoleafapi._tcp`; TXT has `id` (controller MAC), `md`
  (model), `srcvers` (firmware). The `id` keys the token and the config.
- No TLS, port 16021. The token is a URL path segment, so `RedactPath`
  rewrites it in every debug line.
- Pairing: `POST /api/v1/new` answers 403 until the power button has been
  held 5–7 s, then 200 `{"auth_token": ...}` for about 30 s.
- State: `GET /api/v1/<token>/state/on`, `PUT /api/v1/<token>/state`
  with `{"on":{"value":bool}}` (204).
- Events: `GET /api/v1/<token>/events?id=1,3`, Server-Sent Events where the
  SSE `id` is the category (1 state, 3 effects) and `data` is
  `{"events":[{"attr":N,"value":V}]}`; state attr 1 = on, 2 = brightness.
  Verified on Light Panels firmware 5.3.2.
- `DELETE /api/v1/<token>` revokes the token; `auth forget` uses it.

## Debugging

- `nanoleaf --debug <command>` prints every mDNS packet and HTTP exchange.
- `nanoleaf plugin debug on` makes the plugin write the same, plus every
  protocol message, to `~/.local/state/opendeck-nanoleaf/plugin.log`.
- OpenDeck's own log is `~/.local/share/opendeck/logs/opendeck.log`, in UTC.
- `nanoleaf watch` shows exactly the events the plugin reacts to.

## Release

```
git tag -a vX.Y.Z -m "..."
make plugin-release     # dist/opendeck-nanoleaf-X.Y.Z.streamDeckPlugin + nanoleaf-X.Y.Z-<triple> archives
```
