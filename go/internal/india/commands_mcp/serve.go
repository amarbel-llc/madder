package commands_mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"code.linenisgreat.com/madder/go/internal/0/buildinfo"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/protocol"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/server"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/transport"
)

// URI scheme served by this MCP server.
//
//	madder://blobs                       — JSON list of every blob digest
//	                                       across all configured stores,
//	                                       paginated via ?limit=&offset=.
//	madder://blobs/{digest}              — raw bytes of one blob.
//	madder://stores                      — JSON list of configured stores.
//	madder://stores/{store_id}/blobs     — JSON list of blob digests in
//	                                       one store, paginated.
//
// dewey's ResourceRegistry.RegisterTemplate silently drops the reader
// argument and its ReadResource is exact-match only, so this server
// implements server.ResourceProvider directly with prefix-based dispatch.
const (
	uriBlobs           = "madder://blobs"
	uriStores          = "madder://stores"
	prefixBlob         = "madder://blobs/"
	prefixStore        = "madder://stores/"
	suffixStoreBlobs   = "/blobs"
	templateBlob       = "madder://blobs/{digest}"
	templateStoreBlobs = "madder://stores/{store_id}/blobs"

	defaultListLimit = 100
)

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
		Long: "Speaks JSON-RPC MCP over stdio. Exposes static resources " +
			"madder://blobs and madder://stores, plus templates " +
			"madder://blobs/{digest} (raw bytes) and " +
			"madder://stores/{store_id}/blobs (per-store digest list). " +
			"List resources accept ?limit=&offset= query parameters " +
			"with a default limit of " + strconv.Itoa(defaultListLimit) + ". " +
			"Intended to be invoked by clown-stdio-bridge as declared " +
			"in plugins/madder/clown.json.",
	}
}

func (cmd Serve) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	srv, err := server.New(
		transport.NewStdio(os.Stdin, os.Stdout),
		server.Options{
			ServerName:    "madder",
			ServerVersion: buildinfo.Version,
			Resources:     &resourceProvider{env: envBlobStore},
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

// resourceProvider implements server.ResourceProvider for the madder MCP
// server. ReadResource dispatches by URI shape: exact match for the two
// static list resources, prefix match for the two templates. Each list
// resource accepts ?limit= and ?offset= query parameters.
type resourceProvider struct {
	env command_components.BlobStoreEnv
}

var _ server.ResourceProvider = (*resourceProvider)(nil)

func (p *resourceProvider) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return []protocol.Resource{
		{
			URI:  uriBlobs,
			Name: "Madder blobs",
			Description: "Paginated JSON list of every blob digest in any " +
				"configured blob store. Accepts ?limit= and ?offset= " +
				"query parameters. Each entry includes a uri field " +
				"pointing at madder://blobs/{digest}.",
			MimeType: "application/json",
		},
		{
			URI:  uriStores,
			Name: "Madder blob stores",
			Description: "JSON list of every configured blob store with id, " +
				"description, and a blobs_uri pointing at " +
				"madder://stores/{store_id}/blobs.",
			MimeType: "application/json",
		},
	}, nil
}

func (p *resourceProvider) ListResourceTemplates(_ context.Context) ([]protocol.ResourceTemplate, error) {
	return []protocol.ResourceTemplate{
		{
			URITemplate: templateBlob,
			Name:        "Madder blob",
			Description: "Raw bytes of one blob, addressed by digest.",
			MimeType:    "application/octet-stream",
		},
		{
			URITemplate: templateStoreBlobs,
			Name:        "Madder blobs in one store",
			Description: "Paginated JSON list of blob digests in the named " +
				"store. Accepts ?limit= and ?offset= query parameters. " +
				"Each entry includes a uri field pointing at " +
				"madder://blobs/{digest}.",
			MimeType: "application/json",
		},
	}, nil
}

func (p *resourceProvider) ReadResource(ctx context.Context, uri string) (result *protocol.ResourceReadResult, err error) {
	// Request-boundary recover: BlobStore methods without context
	// arguments (HasBlob, GetBlobIOWrapper, GetDefaultHashType, ...)
	// surface backend errors via panic — see issue #134 and the
	// SFTP store's initializeOnce. CLI commands wrap that
	// convention in dewey's Run frame; an MCP server's per-request
	// handlers do not, so we catch here and convert to a JSON-RPC
	// error rather than crashing the server goroutine. Inner
	// recovers (e.g. iterStoreDigests, openBlobAcrossStores) can
	// still convert per-store panics into partial-success payloads;
	// this outer net only fires for panics they didn't expect.
	defer func() {
		if r := recover(); r != nil {
			err = panicToError(r, "reading "+uri)
		}
	}()

	return p.dispatchReadResource(ctx, uri)
}

func (p *resourceProvider) dispatchReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	base, query, err := splitURIQuery(uri)
	if err != nil {
		return nil, err
	}

	switch {
	case base == uriBlobs:
		return p.readBlobsList(uri, query)

	case base == uriStores:
		return p.readStoresList(uri)

	case strings.HasPrefix(base, prefixBlob):
		digest := strings.TrimPrefix(base, prefixBlob)
		return p.readBlob(uri, digest)

	case strings.HasPrefix(base, prefixStore):
		rest := strings.TrimPrefix(base, prefixStore)
		storeId, ok := strings.CutSuffix(rest, suffixStoreBlobs)
		if !ok || storeId == "" {
			return nil, errors.Errorf(
				"uri does not match %s: %q", templateStoreBlobs, uri,
			)
		}
		return p.readStoreBlobsList(uri, storeId, query)
	}

	return nil, errors.Errorf("unknown resource: %q", uri)
}

// panicToError converts a recovered panic value into a wrapped error
// suitable for returning to a caller that expects an error result.
// Use at request boundaries where panics should not escape into the
// surrounding server loop. fmt.Errorf is used (rather than dewey's
// errors.Wrapf) so the formatted prefix surfaces in .Error() —
// dewey's wrapper attaches stack frames but does not splice the
// format into the rendered message.
func panicToError(r any, context string) error {
	if e, ok := r.(error); ok {
		return fmt.Errorf("%s panicked: %w", context, e)
	}
	return fmt.Errorf("%s panicked: %v", context, r)
}

// splitURIQuery splits a URI into the part before the first `?` and the
// parsed query parameters. The base is matched against URI patterns; the
// query carries pagination.
func splitURIQuery(uri string) (string, url.Values, error) {
	base, raw, hasQuery := strings.Cut(uri, "?")
	if !hasQuery {
		return base, url.Values{}, nil
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return "", nil, errors.Wrapf(err, "invalid query string in %q", uri)
	}
	return base, q, nil
}

func (p *resourceProvider) readBlob(uri, digest string) (_ *protocol.ResourceReadResult, err error) {
	var id markl.Id
	if err := id.Set(digest); err != nil {
		return nil, errors.Wrapf(err, "invalid digest %q", digest)
	}

	reader, err := p.env.OpenBlob(&id)
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

// blobEntry is one row in a paginated blob list. Each entry includes the
// resource URI for the blob's bytes so a client can chase the link
// without having to know the URI scheme up front.
type blobEntry struct {
	Digest string `json:"digest"`
	URI    string `json:"uri"`
}

type blobListResult struct {
	Limit       int               `json:"limit"`
	Offset      int               `json:"offset"`
	Total       int               `json:"total"`
	Blobs       []blobEntry       `json:"blobs"`
	StoreErrors []storeErrorEntry `json:"store_errors,omitempty"`
}

// storeErrorEntry reports a per-store failure that was isolated during a
// cross-store aggregation. Errors include panics from store backends that
// fail to initialize (TODO(#134): unreachable SFTP stores currently panic
// in initializeOnce). The aggregator returns a partial result rather
// than failing the whole request.
type storeErrorEntry struct {
	Id    string `json:"id"`
	Error string `json:"error"`
}

type storeEntry struct {
	Id          string `json:"id"`
	Description string `json:"description"`
	BlobsURI    string `json:"blobs_uri"`
}

type storeListResult struct {
	Stores []storeEntry `json:"stores"`
}

func (p *resourceProvider) readBlobsList(uri string, query url.Values) (*protocol.ResourceReadResult, error) {
	digests, storeErrors := collectAllDigests(p.env.GetBlobStores())
	page := paginateDigests(digests, query)
	page.StoreErrors = storeErrors
	return jsonResourceResult(uri, page)
}

func (p *resourceProvider) readStoreBlobsList(uri, storeId string, query url.Values) (*protocol.ResourceReadResult, error) {
	stores := p.env.GetBlobStores()
	store, ok := stores[storeId]
	if !ok {
		available := slices.Sorted(maps.Keys(stores))
		return nil, errors.Errorf(
			"blob store %q not found (available: %v)", storeId, available,
		)
	}

	digests, err := collectStoreDigests(store)
	if err != nil {
		return nil, errors.Wrapf(err, "store %q", storeId)
	}
	return jsonResourceResult(uri, paginateDigests(digests, query))
}

func (p *resourceProvider) readStoresList(uri string) (*protocol.ResourceReadResult, error) {
	stores := p.env.GetBlobStores()
	ids := slices.Sorted(maps.Keys(stores))

	result := storeListResult{Stores: make([]storeEntry, 0, len(ids))}
	for _, id := range ids {
		store := stores[id]
		result.Stores = append(result.Stores, storeEntry{
			Id:          id,
			Description: store.GetBlobStoreDescription(),
			BlobsURI:    prefixStore + id + suffixStoreBlobs,
		})
	}
	return jsonResourceResult(uri, result)
}

// collectAllDigests gathers digest strings across every store in the map,
// deduplicating across stores (a digest can appear in multiple stores)
// and sorting for deterministic pagination. Per-store errors — including
// panics from store backends that fail to initialize — are isolated and
// returned in storeErrors instead of failing the whole call. Iteration
// runs in a deterministic order so a partial result is reproducible.
func collectAllDigests(
	stores blob_stores.BlobStoreMap,
) (digests []string, storeErrors []storeErrorEntry) {
	ids := slices.Sorted(maps.Keys(stores))
	seen := make(map[string]struct{})

	for _, id := range ids {
		store := stores[id]
		if err := iterStoreDigests(store, func(s string) {
			seen[s] = struct{}{}
		}); err != nil {
			storeErrors = append(storeErrors, storeErrorEntry{
				Id: id, Error: err.Error(),
			})
		}
	}

	digests = make([]string, 0, len(seen))
	for s := range seen {
		digests = append(digests, s)
	}
	slices.Sort(digests)
	return digests, storeErrors
}

// collectStoreDigests gathers digest strings from one store, sorting for
// deterministic pagination. Used by per-store reads where a failure is
// surfaced as the resource_read error (callers asked for this specific
// store; aborting is the correct response).
func collectStoreDigests(store blob_stores.BlobStoreInitialized) ([]string, error) {
	var out []string
	if err := iterStoreDigests(store, func(s string) {
		out = append(out, s)
	}); err != nil {
		return nil, err
	}
	slices.Sort(out)
	return out, nil
}

// iterStoreDigests walks one store's AllBlobs iterator, invoking emit for
// each digest. Iterator errors and panics from the store backend
// (e.g. SFTP initialize-once failures, TODO(#134)) are caught and returned
// as ordinary errors so callers can decide whether to abort or skip.
func iterStoreDigests(
	store blob_stores.BlobStoreInitialized,
	emit func(string),
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = errors.Wrapf(e, "store iteration panicked")
			} else {
				err = errors.Errorf("store iteration panicked: %v", r)
			}
		}
	}()

	for id, iterErr := range store.AllBlobs() {
		if iterErr != nil {
			return errors.Wrap(iterErr)
		}
		emit(id.String())
	}
	return nil
}

// paginateDigests slices a sorted digest list according to ?limit= and
// ?offset= query parameters, defaulting to limit=defaultListLimit and
// offset=0. Each entry is paired with the madder://blobs/{digest} URI a
// client would use to read the blob bytes.
func paginateDigests(digests []string, query url.Values) blobListResult {
	limit := defaultListLimit
	if v := query.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}

	offset := 0
	if v := query.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	total := len(digests)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	page := digests[start:end]
	entries := make([]blobEntry, 0, len(page))
	for _, d := range page {
		entries = append(entries, blobEntry{
			Digest: d,
			URI:    prefixBlob + d,
		})
	}

	return blobListResult{
		Limit:  limit,
		Offset: offset,
		Total:  total,
		Blobs:  entries,
	}
}

func jsonResourceResult(uri string, payload any) (*protocol.ResourceReadResult, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{{
			URI:      uri,
			MimeType: "application/json",
			Text:     string(data),
		}},
	}, nil
}

// Blob lookup across the configured stores (default-then-remaining,
// with per-store panic tolerance) lives on the shared BlobStoreEnv as
// OpenBlob — see blob_store_env. readBlob above calls p.env.OpenBlob.
