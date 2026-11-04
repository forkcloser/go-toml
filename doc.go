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
package toml
