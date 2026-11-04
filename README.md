# go-toml

TOML 1.1 for Go: strict decoding into Go values, and a syntax tree that keeps every
byte of a document, so that a document can be edited and formatted without changing
what it was not asked to.

```go
var config Config
if err := toml.Decode(data, &config); err != nil {
	// errors.Is(err, toml.ErrInvalid): not TOML 1.1
	// errors.Is(err, toml.ErrDecode): valid TOML that does not fit Config
}

doc, err := toml.Parse(data) // doc.Bytes() is data, byte for byte
```

Parsing and decoding are built; editing, formatting (`tomlfmt`) and schemas are
specified in [docs/SPEC.md](docs/SPEC.md) and come next.
