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

```go
schema, err := toml.LoadSchema("config.schema.json") // a local JSON Schema
err = doc.Validate(schema)                           // toml.Violations, each with line and column
formatted, err := doc.Format(toml.WithSchema(schema)) // keys in the schema's order
```

```sh
tomlfmt config.toml                         # format in place
tomlfmt --check *.toml                      # print the diff of what is not formatted, exit 1
tomlfmt --schema config.schema.json a.toml  # validate (exit 4), and order keys by the schema
```

```go
doc.Set(toml.Key("tools", "fmt", "version"), "v1.5.0")         // only the value's bytes change
doc.SetTable(toml.Key("tools", "jq"), tool, toml.WithSchema(s)) // a section, where the schema orders it
doc.Append(toml.Key("include"), include)                        // an [[include]] element
doc.Delete(toml.Key("tools", "query"))                          // its sections and comments
```

A file whose first line is `#:schema <path>` names its own schema, relative to it.
[docs/SPEC.md](docs/SPEC.md) is the specification.
