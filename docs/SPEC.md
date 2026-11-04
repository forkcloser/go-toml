# tomlfmt: specification

A Go module that reads, edits and formats TOML 1.1, losing no byte it was not asked
to change, and a `tomlfmt` command on top of it: the fleet's one TOML library. hermetic
reads and writes its manifest through it; the fleet uses the command to format any
TOML file, as a limen lint and fix lane.

## Repository and layout

- Repository `forkcloser/go-toml`, module `github.com/forkcloser/go-toml`, under the
  fleet's tooling (limen) like every other repository.
- The repository root (package `toml`): the library. `cmd/tomlfmt/`: the command.
- Go, the book's baseline `go` directive; `github.com/forkcloser/xos` for every file
  call it covers. Two dependencies, pinned: `pelletier/go-toml/v2` (its decoder) and
  `santhosh-tekuri/jsonschema/v6` (validation); any other is argued in the pull
  request that adds it. Tests also use `toml-lang/toml-test/v2`, the official
  suite, which embeds its cases.

## The library

- **A lossless syntax tree.** `Parse([]byte) (*Document, error)` keeps every byte:
  comments, blank lines, whitespace, string forms (basic, literal, multi-line), key
  quoting, number and date spellings. `doc.Bytes()` on an unmodified document returns
  the input exactly. The tree is built by a parser of our own: go-toml's `unstable`
  AST parser gives no byte range to tables, arrays of tables and arrays, only the
  opening brace to inline tables, and drops comments that trail an expression. Our
  parser runs only on documents go-toml's decoder has accepted, which decides
  validity: the parser must find where each token starts and ends, and nothing more,
  and a fuzz test holds the two to the same answer on every input.
- **Reading into Go values.** `Decode(data []byte, v any) error`, and
  `doc.Decode(v any) error` on an already parsed document, so a caller reads and
  edits the same parse. Strict by policy, with no options: an unknown key is an
  error, as is a value its field cannot hold (an integer into a string or a boolean,
  an integer out of its field's range); an integer is accepted into a float field.
  Errors carry line and column. Built on
  `pelletier/go-toml/v2`'s decoder; consumers never import go-toml themselves, so it
  can be replaced behind the API.
- **TOML 1.1, fully**, reading and writing. Invalid input is an error with line,
  column and what was expected.
- **Editing by byte range.** Find a table, a key or an array-of-tables element by
  path; replace its value, insert a key or a table, remove one. Every byte outside
  the edited range is unchanged. A new node is written in the canonical layout
  (below).
- **Formatting.** `Format(doc, opts) ([]byte, error)` applies the canonical layout to
  the whole document; with a schema in `opts`, the key order too.
- **Schemas.** `LoadSchema(path)` reads a JSON Schema; `doc.Validate(schema)` returns
  the violations, each with key path, line and column.
- **Values never change.** Formatting and editing preserve meaning: the document,
  decoded by a full TOML 1.1 decoder before and after, is equal (an edit differs
  exactly by the edit). The library checks this itself on `Format` and fails rather
  than return a document whose meaning moved.

## The canonical layout

Decided once, with no options beyond those listed:

- UTF-8, LF line endings, one final newline, no trailing whitespace, no tabs outside
  strings.
- One space on each side of `=`; no alignment of `=` or of values across lines (an
  added longer key must not rewrite its neighbours in a diff).
- One blank line before each table header and array-of-tables header except the
  first; runs of blank lines collapsed to one; none at the start of the file.
- Keys bare where TOML allows, quoted otherwise; quoting style otherwise kept.
- Inline tables and arrays on one line when they fit in 100 columns, as
  `{ a = 1, b = 2 }` and `[1, 2]`; otherwise one element per line, indented two
  spaces, with a trailing comma.
- Comments kept, attached to the item they precede or follow on the same line; one
  space before a trailing `#`, and one after it when the comment is not empty.
- String forms, number and date spellings: kept as written.
- Key order: kept as written, unless a schema orders it (below).

## Schemas: validation and key order

A JSON Schema (draft 2020-12) does two things for a file, and only when one is given:
`--schema <file>`, or a `#:schema <path>` directive on the file's first line, resolved
relative to the file. A schema is a local file; tomlfmt never fetches one.

- **Validation.** The document, decoded, is validated against the schema; each
  violation is reported with its key path, line and column, and the file is left
  unchanged.
- **Key order.** The schema's `x-tombi-table-keys-order` annotations, in the
  vocabulary [tombi](https://github.com/tombi-toml/tombi) documents (strategies
  `schema`, `ascending`, `descending`, `version-sort`, per group of `properties`,
  `patternProperties` and `additionalProperties`), order a table's keys:
  - declared keys in the order the annotation gives; any other key after them, in
    the order written;
  - the elements of an array, and of an array of tables, are never reordered: their
    order is meaning;
  - a key moves with its comments: those on the lines directly above it, and the one
    trailing it;
  - without a schema, or without an annotation for a table, keys stay as written:
    tomlfmt never guesses an order.

The same order is what a program writing through the library gets (`Format` and the
editing operations take the schema), so a file written by a program and the same file
formatted by tomlfmt are the same bytes.

## The command

```
tomlfmt [--check] [--schema <file>] [<path>…]
```

- With paths: format each file in place, atomically (temporary file beside it, sync,
  rename). A directory is walked for `*.toml`, honouring `.gitignore`.
- Without paths: read standard input, write standard output.
- `--check`: write nothing; list each file whose formatted form differs, with a
  unified diff, and exit 1 if any does.
- Exit codes: 0 done (or nothing to change), 1 `--check` found differences, 2 invalid
  input or usage, 3 an I/O error, 4 a schema violation. Errors name the file, the line
  and the column.
- No configuration file. The layout is the canonical one.

## Tests

- Black-box, from the package's outside and through the built command as a child
  process.
- Golden files: input and expected output pairs for every layout rule, TOML 1.1
  constructs included.
- The official TOML test suite (`toml-test`), through its own runner: every valid
  case parses, round-trips byte for byte and decodes to the suite's values; every
  invalid case is refused.
- Fuzzing: `Parse` accepts exactly what go-toml's decoder accepts, and `Parse` then
  `Bytes` is the identity; `Format` is idempotent; `Format`
  preserves meaning (decode before and after), with and without a schema's ordering.
- Schemas: validation results against the JSON Schema test suite's draft 2020-12
  cases, through TOML documents expressing them; ordering on each strategy, with
  comments.
- Edits: each operation leaves every byte outside its range unchanged, checked on
  real-world files (`Cargo.toml`, `pyproject.toml`, hermetic manifests).

## Not in scope

A linter beyond schema validation, a language server, fetching schemas, configuration
of the layout, and any knowledge of a particular file's meaning beyond its schema
(hermetic's other manifest rules stay in hermetic). The limen lane that runs `tomlfmt` is a limen change, after a first
release.
