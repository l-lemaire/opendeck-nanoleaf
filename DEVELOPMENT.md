# Development notes

Sister project of [opendeck-hue](https://github.com/l-lemaire/opendeck-hue),
built the same way. `internal/openaction`, `internal/secrets` and
`internal/config` are copies from there with names adapted; the DNS codec and
the SSE parser in `internal/nanoleaf` are copies too.

```
make build      # bin/nanoleaf
make check      # gofmt, go vet, tests
```
