# gopickgo

`gopickgo` picks the "right" `go` (or `gofmt`) binary based on your current
directory, so you don't have to think about which Go version to use.

## Search order

1. **Project-local toolchain**: walks up from the current directory looking for
   a `go.mod`, then tries `./tool/go` relative to the module root (or
   `../bin/go` if you're inside the Go source tree itself).
2. **`$HOME/sdk/go`**: a bare `~/sdk/go` directory, if present.
3. **Highest versioned SDK**: globs `~/sdk/go*`, parses the version suffixes,
   and picks the highest semver (e.g. `~/sdk/go1.23.4` over `~/sdk/go1.22`).
4. **System Go**: `/usr/local/go/bin/go`, `/usr/local/bin/go`, `/usr/bin/go`.

The first candidate that exists on disk wins.

## Install

```
go install github.com/bradfitz/gopickgo@latest
```

Then symlink `go` and `gofmt` to `gopickgo` somewhere early in your `$PATH`:

```
ln -sf $(which gopickgo) ~/bin/go
ln -sf $(which gopickgo) ~/bin/gofmt
```

Make sure `~/bin` (or wherever you put the symlinks) appears in your `$PATH`
before `/usr/local/go/bin` or `/usr/bin`.

When invoked as `gofmt`, gopickgo finds the right `go`, resolves its
`GOTOOLDIR`, and execs the corresponding `gofmt`.

## Debugging

To see which `go` binary gopickgo would pick:

```
gopickgo pick
```

or equivalently (if you've symlinked `go` to `gopickgo`):

```
go pick
```
