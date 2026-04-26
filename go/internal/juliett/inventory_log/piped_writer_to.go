package inventory_log

import "io"

// pipedWriterTo bridges asynchronous Emit-driven writes to the synchronous
// io.WriterTo handed to hyphence.Writer's Blob slot. The hyphence Writer
// call blocks for the lifetime of the session, copying pipe bytes into the
// destination file; Emit writes encoded NDJSON lines into the embedded
// io.PipeWriter; closing the writer signals EOF and unblocks WriteTo.
//
// TODO(amarbel-llc/purse-first#64) replace with dewey/ohio.MakePipedWriterTo
// once that primitive lands. The internals here mirror it verbatim.
type pipedWriterTo struct {
	*io.PipeWriter
	pr *io.PipeReader
}

func newPipedWriterTo() *pipedWriterTo {
	pr, pw := io.Pipe()
	return &pipedWriterTo{PipeWriter: pw, pr: pr}
}

func (p *pipedWriterTo) WriteTo(out io.Writer) (int64, error) {
	return io.Copy(out, p.pr)
}
