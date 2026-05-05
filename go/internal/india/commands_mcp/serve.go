package commands_mcp

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/amarbel-llc/madder/go/internal/0/buildinfo"
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/protocol"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/server"
	"github.com/amarbel-llc/purse-first/libs/dewey/golf/transport"
)

// blobURIPrefix is the fixed prefix of every URI this server serves.
// The full template is registered as `madder://blobs/{digest}`.
const blobURIPrefix = "madder://blobs/"

func init() {
	utility.AddCmd("serve", &Serve{
		EnvBlobStore: command_components.EnvBlobStore{BlobStoreXDGScope: "madder"},
	})
}

type Serve struct {
	command_components.EnvBlobStore
}

var _ futility.CommandWithDescription = Serve{}

func (cmd Serve) GetDescription() futility.Description {
	return futility.Description{
		Short: "run the madder MCP server on stdio",
		Long: "Speaks JSON-RPC MCP over stdio. Exposes one resource " +
			"template, madder://blobs/{digest}, that reads a blob from " +
			"madder's configured blob stores by content-addressable " +
			"digest. Intended to be invoked by clown-stdio-bridge as " +
			"declared in plugins/madder/clown.json.",
	}
}

func (cmd Serve) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	registry := server.NewResourceRegistry()
	registry.RegisterTemplate(
		protocol.ResourceTemplate{
			URITemplate: blobURIPrefix + "{digest}",
			Name:        "Madder blob",
			Description: "Raw bytes of a madder blob, addressed by digest.",
			MimeType:    "application/octet-stream",
		},
		makeBlobReader(envBlobStore),
	)

	srv, err := server.New(
		transport.NewStdio(os.Stdin, os.Stdout),
		server.Options{
			ServerName:    "madder",
			ServerVersion: buildinfo.Version,
			Resources:     registry,
		},
	)
	if err != nil {
		errors.ContextCancelWithError(req, errors.Wrap(err))
		return
	}

	req.SetCancelOnSignals(syscall.SIGINT, syscall.SIGTERM)

	if err := srv.Run(req.Context); err != nil {
		errors.ContextCancelWithError(req, errors.Wrap(err))
	}
}

// makeBlobReader returns a server.ResourceReader closure that resolves
// any madder://blobs/<digest> URI to the blob's raw bytes. The closure
// captures the BlobStoreEnv built once at server start; per-request
// store discovery happens inside.
func makeBlobReader(env command_components.BlobStoreEnv) server.ResourceReader {
	return func(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
		id, err := parseBlobURI(uri)
		if err != nil {
			return nil, err
		}

		reader, err := openBlob(env, &id)
		if err != nil {
			return nil, err
		}
		defer errors.DeferredCloser(&err, reader)

		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, errors.Wrap(err)
		}

		return &protocol.ResourceReadResult{
			Contents: []protocol.ResourceContent{{
				URI:      uri,
				MimeType: "application/octet-stream",
				Blob:     base64.StdEncoding.EncodeToString(data),
			}},
		}, nil
	}
}

// parseBlobURI extracts the digest segment from a madder://blobs/<digest>
// URI and parses it into a markl.Id. Returns a wrapped error if the
// URI is shaped wrong or the digest is malformed; callers map both
// cases onto MCP invalid-resource-URI responses.
func parseBlobURI(uri string) (markl.Id, error) {
	var id markl.Id

	digest, ok := strings.CutPrefix(uri, blobURIPrefix)
	if !ok {
		return id, errors.Errorf(
			"uri does not match %s{digest}: %q", blobURIPrefix, uri,
		)
	}

	if err := id.Set(digest); err != nil {
		return id, errors.Wrapf(err, "invalid digest %q", digest)
	}

	return id, nil
}

// openBlob walks the default blob store first, then any remaining
// configured stores, returning a reader for the first store that
// reports HasBlob. Returns a not-found error if no store has it.
func openBlob(
	env command_components.BlobStoreEnv,
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	def, remaining := env.GetDefaultBlobStoreAndRemaining()
	if def.HasBlob(id) {
		return def.MakeBlobReader(id)
	}
	for _, s := range remaining {
		if s.HasBlob(id) {
			return s.MakeBlobReader(id)
		}
	}
	return nil, errors.MakeErrNotFoundString(
		"blob not found in any blob store: " + id.String(),
	)
}

