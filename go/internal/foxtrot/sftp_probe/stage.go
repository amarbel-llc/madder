package sftp_probe

// Stage labels where in the read pipeline a verification attempt
// landed. StageOK means VerifySample produced a digest matching the
// path-name. The failure stages classify the cause: a decrypt stage
// error usually means the wrong encryption key (or no encryption
// when one was attempted); a decompress stage error means the wrong
// codec; a hash-mismatch means every layer accepted the bytes but
// the recovered content didn't reproduce the path digest. A
// layout-mismatch is a non-pipeline failure: the candidate's
// declared directory layout (multi-hash vs single-hash) disagrees
// with the on-disk layout the probe walked, so any per-byte
// verification would be a misleading "ok."
type Stage int

const (
	StageOK Stage = iota
	StageDecrypt
	StageDecompress
	StageHashMismatch
	StageLayoutMismatch
)

func (s Stage) String() string {
	switch s {
	case StageOK:
		return "ok"
	case StageDecrypt:
		return "decrypt"
	case StageDecompress:
		return "decompress"
	case StageHashMismatch:
		return "hash_mismatch"
	case StageLayoutMismatch:
		return "layout_mismatch"
	default:
		return "unknown"
	}
}
