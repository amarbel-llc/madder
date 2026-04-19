package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/charlie/tap_diagnostics"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/script_value"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
	"github.com/mattn/go-isatty"
)

func init() {
	utility.AddCmd("write", &Write{
		Format: OutputFormatAuto,
	})
}

// OutputFormat selects the encoding of write's per-blob result stream.
//
// auto (default): NDJSON when stdout is not a TTY, TAP otherwise.
// tap:            TAP format regardless of stdout.
// json:           NDJSON (one JSON object per blob) regardless of stdout.
type OutputFormat string

const (
	OutputFormatAuto = OutputFormat("auto")
	OutputFormatTAP  = OutputFormat("tap")
	OutputFormatJSON = OutputFormat("json")
)

func (f OutputFormat) String() string { return string(f) }

func (f *OutputFormat) Set(value string) error {
	clean := OutputFormat(strings.TrimSpace(strings.ToLower(value)))

	switch clean {
	case OutputFormatAuto, OutputFormatTAP, OutputFormatJSON:
		*f = clean
		return nil
	}

	return fmt.Errorf("unsupported output format: %q", value)
}

func (OutputFormat) GetCLICompletion() map[string]string {
	return map[string]string{
		OutputFormatAuto.String(): "TAP on a TTY, NDJSON when stdout is piped (default)",
		OutputFormatTAP.String():  "TAP format (human-readable)",
		OutputFormatJSON.String(): "NDJSON: one JSON object per blob",
	}
}

// resolveAuto collapses auto into tap or json based on whether stdout is a TTY.
func (f OutputFormat) resolveAuto(stdout *os.File) OutputFormat {
	if f != OutputFormatAuto {
		return f
	}

	fd := stdout.Fd()
	if isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd) {
		return OutputFormatTAP
	}

	return OutputFormatJSON
}

type Write struct {
	command_components.EnvBlobStore

	Check         bool
	Format        OutputFormat
	UtilityBefore script_value.Utility
	UtilityAfter  script_value.Utility
}

var (
	_ interfaces.CommandComponentWriter = (*Write)(nil)
	_ command.CommandWithParams         = (*Write)(nil)
)

func (cmd *Write) GetParams() []command.Param {
	return []command.Param{
		command.Arg[*values.String]{
			Name:        "args",
			Description: "file paths, '-' for stdin, or blob store IDs to switch the active store",
			Variadic:    true,
		},
	}
}

func (cmd Write) GetDescription() command.Description {
	return command.Description{
		Short: "write blobs to a store",
		Long: "Write files or stdin into the content-addressable blob store. " +
			"Each argument is a file path, '-' for stdin, or a blob store ID " +
			"that switches the active store for subsequent writes. " +
			"Output defaults to TAP on an interactive terminal and to NDJSON " +
			"(one JSON object per blob, suitable for programmatic consumers) " +
			"when stdout is piped; pass -format to force a specific encoding. " +
			"Each JSON record has fields \"id\", \"size\", and \"source\", plus " +
			"\"store\" when a non-default store is active, \"present\" under " +
			"-check, \"error\" on per-arg failures, and \"skipped\" for null " +
			"digests. Use -check to verify that files already exist in the " +
			"store without writing them.",
	}
}

func (cmd Write) Complete(
	req command.Request,
	envLocal env_local.Env,
	commandLine command.CommandLineInput,
) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	// args := commandLine.FlagsOrArgs[1:]

	// if commandLine.InProgress != "" {
	// 	args = args[:len(args)-1]
	// }

	for id, blobStore := range blobStores {
		envLocal.GetOut().Printf("%s\t%s", id, blobStore.GetBlobStoreDescription())
	}
}

func (cmd *Write) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(
		&cmd.Check,
		"check",
		false,
		"only check if the object already exists",
	)

	flagSet.Var(
		&cmd.Format,
		"format",
		"output format: auto (default), tap, or json (NDJSON)",
	)

	flagSet.Var(&cmd.UtilityBefore, "utility-before", "")
	flagSet.Var(&cmd.UtilityAfter, "utility-after", "")
}

// writeSink abstracts the per-arg output stream. TAP and NDJSON implementations
// let Run stay format-agnostic.
type writeSink interface {
	// success reports a blob either freshly written or already present. In
	// -check mode with the blob present, callers should pass present=true;
	// outside -check, present is meaningless and ignored by the JSON sink.
	success(id domain_interfaces.MarklId, size int64, source, store string)
	// checkMiss reports a -check miss: the digest was computed but the
	// blob is not in the store. Counts as a failure.
	checkMiss(id domain_interfaces.MarklId, size int64, source, store string)
	// failure reports a per-arg error that prevented digest computation
	// (resolve error, read error, etc.).
	failure(source string, err error)
	// skip reports a skipped arg (e.g. null digest).
	skip(source, reason string)
	// notice reports informational events (store switches, shadow
	// warnings). TAP emits as comments; JSON routes to stderr.
	notice(msg string)
	// finalize is called once after all args have been processed.
	finalize()
}

type tapSink struct {
	tw *tap.Writer
}

func newTapSink(w io.Writer) *tapSink {
	return &tapSink{tw: tap.NewWriter(w)}
}

func (s *tapSink) success(id domain_interfaces.MarklId, _ int64, source, _ string) {
	s.tw.Ok(fmt.Sprintf("%s %s", id, source))
}

func (s *tapSink) checkMiss(id domain_interfaces.MarklId, _ int64, source, _ string) {
	s.tw.NotOk(
		fmt.Sprintf("%s %s", id, source),
		map[string]string{
			"severity": "fail",
			"message":  "untracked",
		},
	)
}

func (s *tapSink) failure(source string, err error) {
	s.tw.NotOk(source, tap_diagnostics.FromError(err))
}

func (s *tapSink) skip(source, reason string) {
	s.tw.Skip(source, reason)
}

func (s *tapSink) notice(msg string) {
	s.tw.Comment(msg)
}

func (s *tapSink) finalize() {
	s.tw.Plan()
}

type jsonSink struct {
	enc     *json.Encoder
	errOut  io.Writer
	isCheck bool
}

// jsonRecord is the wire shape of a single NDJSON write record. Fields are
// omitempty so a pipeline consumer sees only the fields that apply to the
// event type (success, check miss, failure, skip).
type jsonRecord struct {
	Id      string `json:"id,omitempty"`
	Size    *int64 `json:"size,omitempty"`
	Source  string `json:"source,omitempty"`
	Store   string `json:"store,omitempty"`
	Present *bool  `json:"present,omitempty"`
	Error   string `json:"error,omitempty"`
	Skipped string `json:"skipped,omitempty"`
}

func newJsonSink(out, errOut io.Writer, isCheck bool) *jsonSink {
	return &jsonSink{
		enc:     json.NewEncoder(out),
		errOut:  errOut,
		isCheck: isCheck,
	}
}

func (s *jsonSink) emit(rec jsonRecord) {
	// json.Encoder already appends a newline, giving NDJSON framing.
	_ = s.enc.Encode(rec)
}

func (s *jsonSink) success(id domain_interfaces.MarklId, size int64, source, store string) {
	rec := jsonRecord{
		Id:     id.String(),
		Size:   &size,
		Source: source,
		Store:  store,
	}

	if s.isCheck {
		present := true
		rec.Present = &present
	}

	s.emit(rec)
}

func (s *jsonSink) checkMiss(id domain_interfaces.MarklId, size int64, source, store string) {
	present := false
	s.emit(jsonRecord{
		Id:      id.String(),
		Size:    &size,
		Source:  source,
		Store:   store,
		Present: &present,
	})
}

func (s *jsonSink) failure(source string, err error) {
	s.emit(jsonRecord{
		Source: source,
		Error:  err.Error(),
	})
}

func (s *jsonSink) skip(source, reason string) {
	s.emit(jsonRecord{
		Source:  source,
		Skipped: reason,
	})
}

func (s *jsonSink) notice(msg string) {
	fmt.Fprintln(s.errOut, msg)
}

func (s *jsonSink) finalize() {}

func (cmd Write) Run(req command.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStore := envBlobStore.GetDefaultBlobStore()

	format := cmd.Format.resolveAuto(os.Stdout)

	var sink writeSink
	switch format {
	case OutputFormatJSON:
		sink = newJsonSink(os.Stdout, os.Stderr, cmd.Check)
	default:
		sink = newTapSink(os.Stdout)
	}

	var failCount atomic.Uint32
	var blobStoreId blob_store_id.Id

	sawStdin := false

	for _, arg := range req.PopArgs() {
		switch {
		case arg == "-" && sawStdin:
			sink.notice("'-' passed in more than once. Ignoring")
			continue

		case arg == "-":
			sawStdin = true
		}

		resolved := command_components.ResolveFileOrBlobStoreId(arg)

		if resolved.Err != nil {
			sink.failure(arg, resolved.Err)
			failCount.Add(1)
			continue
		}

		if resolved.IsStoreSwitch {
			blobStoreId = resolved.BlobStoreId
			blobStore = envBlobStore.GetBlobStore(blobStoreId)
			sink.notice(fmt.Sprintf("switched to blob store: %s", blobStoreId))
			continue
		}

		// The arg resolved to a file. If any configured blob store shares
		// the arg's bare name (regardless of XDG scope), the caller
		// probably intended the store-switch — warn them and point at the
		// disambiguating forms. Prefixed names (/, ~, ., _, %) bypass the
		// filesystem lookup entirely so this only fires for unprefixed
		// args.
		var shadowedId blob_store_id.Id
		if err := shadowedId.Set(arg); err == nil {
			for _, store := range envBlobStore.GetBlobStores() {
				if store.Path.GetId().GetName() == shadowedId.GetName() {
					sink.notice(fmt.Sprintf(
						"warning: %q shadows blob store %q; use './%s' for the file or %q for the store",
						arg, store.Path.GetId(), arg, store.Path.GetId().String(),
					))
					break
				}
			}
		}

		blobId, size, err := cmd.doOne(blobStore, resolved.BlobReader)

		if err != nil {
			sink.failure(arg, err)
			failCount.Add(1)
			continue
		}

		if blobId.IsNull() {
			sink.skip(arg, "null digest")
			continue
		}

		storeName := ""
		if !blobStoreId.IsEmpty() {
			storeName = blobStoreId.String()
		}

		hasBlob := blobStore.HasBlob(blobId)

		if hasBlob {
			sink.success(blobId, size, arg, storeName)
		} else {
			if cmd.Check {
				sink.checkMiss(blobId, size, arg, storeName)
				failCount.Add(1)
			} else {
				sink.success(blobId, size, arg, storeName)
			}
		}
	}

	sink.finalize()

	fc := failCount.Load()

	if fc > 0 {
		errors.ContextCancelWithBadRequestf(
			req,
			"untracked objects: %d",
			fc,
		)
		return
	}
}

func (cmd Write) doOne(
	blobStore blob_stores.BlobStoreInitialized,
	blobReader domain_interfaces.BlobReader,
) (blobId domain_interfaces.MarklId, size int64, err error) {
	defer errors.DeferredCloser(&err, blobReader)

	var writeCloser domain_interfaces.BlobWriter

	if cmd.Check {
		{
			hash, hashRepool := blobStore.GetDefaultHashType().GetHash()
			var repool func()
			writeCloser, repool = markl_io.MakeWriterWithRepool(
				hash,
				nil,
			)
			defer func() { repool(); hashRepool() }()
		}
	} else {
		if writeCloser, err = blobStore.MakeBlobWriter(nil); err != nil {
			err = errors.Wrap(err)
			return blobId, size, err
		}
	}

	defer errors.DeferredCloser(&err, writeCloser)

	if size, err = io.Copy(writeCloser, blobReader); err != nil {
		err = errors.Wrap(err)
		return blobId, size, err
	}

	blobId = writeCloser.GetMarklId()

	return blobId, size, err
}
