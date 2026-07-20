package blob_io

import (
	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/files"
)

// TemporaryFS is the temp-fs handle MoveOptions embeds. Aliased
// from dewey/delta/files so callers may pass an env_dir.TemporaryFS
// (also a files.TemporaryFS alias) directly into a blob_io.MoveOptions
// without conversion.
type TemporaryFS = files.TemporaryFS

func MakeHashBucketPathFromMerkleId(
	id domain_interfaces.MarklId,
	buckets []int,
	multiHash bool,
	pathComponents ...string,
) string {
	if multiHash {
		pathComponents = append(
			pathComponents,
			id.GetMarklFormat().GetMarklFormatId(),
		)
	}

	return files.MakeHashBucketPath(
		[]byte(markl.FormatBytesAsHex(id)),
		buckets,
		pathComponents...,
	)
}

var MakeHashBucketPath = files.MakeHashBucketPath

var PathFromHeadAndTail = files.PathFromHeadAndTail

var MakeHashBucketPathJoinFunc = files.MakeHashBucketPathJoinFunc

var MakeDirIfNecessary = files.MakeDirIfNecessary

var MakeDirIfNecessaryForStringerWithHeadAndTail = files.MakeDirIfNecessaryForStringerWithHeadAndTail
