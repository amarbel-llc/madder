// Package output_format provides a shared -format flag for madder commands
// that stream per-record results.
//
// The Format type is a flag.Value with values auto, tap, json, ndjson,
// and crap. For streaming commands (sync, fsck, write, pack-blobs) json
// and ndjson are aliases — both emit NDJSON, one JSON object per record.
// For commands that have a meaningful single-document shape (notably
// list) the two differ: json is a single JSON document and ndjson is the
// per-record stream. crap emits ndjson-crap result-family records
// (go-crap v2). auto is the default; Resolve collapses it to tap on a
// TTY and to ndjson when stdout is piped, while ResolvePipedDefault lets
// a command choose its own piped default (sync passes crap).
//
// Each consuming command defines its own sink interface (TAP vs NDJSON)
// because per-command event shapes differ. This package only supplies the
// flag type and auto-detect helper; it does not prescribe a sink shape.
package output_format

//go:generate dagnabit export

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// Format selects the encoding of a command's per-record result stream.
//
// auto (default): ndjson when stdout is not a TTY, TAP otherwise.
// tap:            TAP format regardless of stdout.
// json:           single JSON document (command-specific shape).
//
//	For streaming commands this is an alias for ndjson.
//
// ndjson:         one JSON object per line.
// crap:           ndjson-crap records, one JSON record per line (go-crap v2).
type Format string

const (
	FormatAuto   = Format("auto")
	FormatTAP    = Format("tap")
	FormatJSON   = Format("json")
	FormatNDJSON = Format("ndjson")
	FormatCRAP   = Format("crap")
)

// Default is the value to initialize a flag field with.
const Default = FormatAuto

// FlagDescription is a suggested description for use with flag.Var so all
// commands present the same help text.
const FlagDescription = "output format: auto (default), tap, json, ndjson, or crap"

func (f Format) String() string { return string(f) }

func (f *Format) Set(value string) error {
	clean := Format(strings.TrimSpace(strings.ToLower(value)))

	switch clean {
	case FormatAuto, FormatTAP, FormatJSON, FormatNDJSON, FormatCRAP:
		*f = clean
		return nil
	}

	return fmt.Errorf("unsupported output format: %q", value)
}

func (Format) GetCLICompletion() map[string]string {
	return map[string]string{
		FormatAuto.String():   "TAP on a TTY, NDJSON when stdout is piped (default)",
		FormatTAP.String():    "TAP format (human-readable)",
		FormatJSON.String():   "single JSON document (alias for ndjson on streaming commands)",
		FormatNDJSON.String(): "NDJSON: one JSON object per record",
		FormatCRAP.String():   "ndjson-crap: one JSON record per line (see crap-present(1))",
	}
}

// Resolve collapses auto into tap or ndjson based on whether stdout is a
// terminal. Non-auto values are returned unchanged. Resolve picks ndjson
// rather than json for the piped case so streaming consumers get the
// per-record shape by default and so a command's `json` case can mean
// "single document" without collisions.
func (f Format) Resolve(stdout *os.File) Format {
	return f.ResolvePipedDefault(stdout, FormatNDJSON)
}

// ResolvePipedDefault collapses auto into tap on a TTY and into piped
// otherwise. Non-auto values are returned unchanged. Commands whose piped
// default has migrated to ndjson-crap pass FormatCRAP; Resolve keeps the
// legacy ndjson piped default for everyone else.
func (f Format) ResolvePipedDefault(stdout *os.File, piped Format) Format {
	fd := stdout.Fd()
	isTTY := isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
	return f.resolveFor(isTTY, piped)
}

func (f Format) resolveFor(isTTY bool, piped Format) Format {
	if f != FormatAuto {
		return f
	}

	if isTTY {
		return FormatTAP
	}

	return piped
}
