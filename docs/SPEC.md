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
  call it covers, and `mycophonic/primordium`'s `filesystem.WriteFile` to write a
  file atomically. The library's two dependencies, pinned: `pelletier/go-toml/v2` (its decoder) and
  `santhosh-tekuri/jsonschema/v6` (validation), with `golang.org/x/text`, which
  jsonschema requires, for its messages; any other is argued in the pull request
  that adds it. Tests also use `toml-lang/toml-test/v2`, the official
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
- **Formatting.** `doc.Format(...Option) ([]byte, error)` applies the canonical
  layout to the whole document; `WithSchema(schema)` adds the schema's key order.
- **Schemas.** `LoadSchema(path)` reads a JSON Schema; `doc.Validate(schema)` returns
  nil or `Violations`, each an `Error` with key path, line and column;
  `doc.SchemaDirective()` returns what a `#:schema` first line names.
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
  spaces, with a trailing comma. A comma after the last element is the author's word
  that the collection is one element per line, and keeps it so even once it would
  fit (the "magic trailing comma" of Black, ruff and tombi): a list laid out for
  clean diffs as it grows is never collapsed.
- Comments kept, attached to the item they precede or follow on the same line, one
  space before a trailing `#`. Their text is kept as written: a comment is free text,
  and making it start with `# ` would rewrite `#:schema` directives, banners of `#`
  and commented-out code alike.
- String forms, number and date spellings: kept as written.
- Key order: kept as written, unless a schema orders it (below).

## Schemas: validation and key order

A JSON Schema (draft 2020-12, or the draft its `$schema` names) does two things for a
file, and only when one is given: `--schema <file>`, or a `#:schema <path>` directive
on the file's first line, resolved relative to the file; `--schema`, the explicit
word, wins over the directive. A schema is a local file, and so is every file its
references name: a directive or a reference naming a URL is an error, never a
download.

- **Validation.** The document, decoded, is validated against the schema, read as
  JSON reads: a table is an object, an array an array; a date-time, a date or a time
  is a string, its RFC 3339 text (format assertion is the schema's choice, off by
  default in 2020-12); `nan` and `inf` are no JSON number, so a schema cannot describe
  them, and a document holding one breaks any schema. Each violation is reported
  with its key path, line and column: the key that writes the value, the header of a
  table, the element of an array, the first key inside a table that is only implied;
  an additional property at its own key; a failed `anyOf` or `oneOf` once, saying
  why each branch failed. A file with a violation is left unchanged.
- **Key order.** The schema's `x-tombi-table-keys-order` annotations, in the
  vocabulary [tombi](https://github.com/tombi-toml/tombi) documents (strategies
  `schema`, `ascending`, `descending`, `version-sort`, per group of `properties`,
  `patternProperties` and `additionalProperties`), order a table's keys:
  - the annotation is one strategy for every key, or an object of groups to
    strategies, applied in the order the object lists them; `schema` is the order
    `properties` declares, and orders only that group; `ascending` and `descending`
    compare bytes; `version-sort` is the
    [Rust style guide's](https://doc.rust-lang.org/nightly/style-guide/#sorting);
  - declared keys in the order the annotation gives; any other key after them, in
    the order written;
  - the subschemas that apply to a table are its schema's references, `allOf`, the
    `anyOf` and `oneOf` branches the table matches, and the `then` or `else` its `if`
    chooses; the first annotation among them is the table's;
  - keys move within a run of lines between blank lines, never across one (a blank
    line is the author's grouping), and never past a table header: a table's
    sections stay where they are written; dotted keys sharing a first part are
    gathered at the place of the first, then ordered by the subtable's own schema;
    an inline table's keys are ordered the same way;
  - the elements of an array, and of an array of tables, are never reordered: their
    order is meaning;
  - a key moves with its comments: those on the lines directly above it, and the one
    trailing it; a `#:schema` directive stays the first line;
  - without a schema, or without an annotation for a table, keys stay as written:
    tomlfmt never guesses an order.

The same order is what a program writing through the library gets (`Format` and the
editing operations take the schema), so a file written by a program and the same file
formatted by tomlfmt are the same bytes.

## The command

```
tomlfmt [--check] [--schema <file>] [<file>…]
tomlfmt --version
```

- With files: format each in place, atomically (primordium's `filesystem.WriteFile`:
  a temporary file beside it, synced, then renamed over it), the file's mode set
  again after, since the umask trims the mode of a file made anew. Paths name files: which files of a tree
  to format is the caller's decision (limen's recipes list them through git), not a
  walk tomlfmt makes and every caller then has to steer.
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
- Schemas: what is ours, not the validator's (jsonschema runs the JSON Schema test
  suite itself, and running it again through TOML documents would test it, not us):
  the data model TOML values are read as, where each violation is placed, and
  ordering, on golden files for each strategy, group, combinator, comment
  placement, dotted key and inline table; fuzzing holds `Validate` to placing every
  violation inside the document; version sort on the style guide's own example.
- Edits: each operation leaves every byte outside its range unchanged, checked on
  real-world files (`Cargo.toml`, `pyproject.toml`, hermetic manifests).

## Not in scope

A linter beyond schema validation, a language server, fetching schemas, configuration
of the layout, and any knowledge of a particular file's meaning beyond its schema
(hermetic's other manifest rules stay in hermetic). The limen lane that runs `tomlfmt` is a limen change, after a first
release.
