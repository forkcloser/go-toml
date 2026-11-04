// Package toml reads TOML 1.1 into Go values, strictly, and keeps every byte of a
// document so that it can be edited and formatted without changing anything it was
// not asked to change.
//
// [Parse] returns a [Document] whose [Document.Bytes] is the input, byte for byte.
// [Decode] and [Document.Decode] read a document into a Go value: a key the value has
// no field for is an error, as is a value that does not fit its field. Every error
// wraps [ErrInvalid] (the input is not TOML 1.1), [ErrDecode] (it is, but does not
// fit the value) or, for a bug in this package rather than a fault of the input,
// [ErrInternal], and carries the line and column it concerns.
//
// [LoadSchema] reads a JSON Schema from a local file. [Document.Validate] checks a
// document against it and returns [Violations], each wrapping [ErrViolation] with
// the line, column and key of the value; [WithSchema] has [Document.Format] order
// each table's keys by the schema's x-tombi-table-keys-order annotations. A file
// LoadSchema cannot use is an error wrapping [ErrSchema].
//
// [Document.Set], [Document.SetTable], [Document.Append] and [Document.Delete] edit
// a document by byte range at a [Path]: every byte outside the edit is kept. Each
// edit is checked to mean what it says, the document decoded after equal to the
// document before with the edit applied, and leaves the document as it was when it
// fails: [ErrNotFound], [ErrConflict], [ErrValue].
package toml
