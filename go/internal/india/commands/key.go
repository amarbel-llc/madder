package commands

import (
	"fmt"

	"code.linenisgreat.com/madder/go/internal/charlie/store_key"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/piggy/go/pkgs/pigpen"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/values"
)

func init() {
	utility.AddCmd("key-status", &KeyStatus{})
	utility.AddCmd("key-reseal", &KeyReseal{})
}

// sealedKeyStore is a sealed-key store (FDR 0011) opened for a key
// command: the store's sidecar access plus the sidecar as it is now.
type sealedKeyStore struct {
	id      string
	access  blob_stores.StoreKeySidecarStore
	sidecar blob_store_configs.StoreKey
	sealed  []markl.Id
}

// openSealedKeyStore resolves storeId and reads its sidecar. ok is false
// when the request was cancelled with an error.
func openSealedKeyStore(
	env command_components.BlobStoreEnv,
	blobStore blob_stores.BlobStoreInitialized,
	storeId string,
) (store sealedKeyStore, ok bool) {
	if blobStore.BlobStore == nil {
		return store, false
	}

	store.id = storeId

	_, isSealedKey := blobStore.BlobStore.GetBlobStoreConfig().(blob_store_configs.ConfigSealedKey)

	if store.access, ok = blobStore.BlobStore.(blob_stores.StoreKeySidecarStore); !ok || !isSealedKey {
		errors.ContextCancelWithBadRequestf(
			env,
			"blob store %q is not a sealed-key store (it was not created with -pigpen)",
			storeId,
		)

		return store, false
	}

	raw, err := store.access.ReadStoreKeySidecar()
	if err != nil {
		env.Cancel(errors.Wrapf(err, "reading the store key sidecar of %q", storeId))
		return store, false
	}

	if store.sidecar, err = blob_store_configs.DecodeStoreKey(raw); err != nil {
		env.Cancel(errors.Wrapf(err, "store key sidecar of %q", storeId))
		return store, false
	}

	if store.sealed, err = store_key.SealedRecipients(
		[]byte(store.sidecar.Sealed.Document),
	); err != nil {
		env.Cancel(errors.Wrapf(err, "store key sidecar of %q", storeId))
		return store, false
	}

	return store, true
}

func describeRecipientSet(recipients []markl.Id) (string, error) {
	digest, err := store_key.RecipientSetDigest(recipients)
	if err != nil {
		return "", err
	}

	noun := "recipients"
	if len(recipients) == 1 {
		noun = "recipient"
	}

	return fmt.Sprintf("%d %s (%s)", len(recipients), noun, digest), nil
}

// recipientsMissingFrom returns the members of recipients that are not in
// other, in recipients' order.
func recipientsMissingFrom(recipients, other []markl.Id) (missing []string) {
	have := make(map[string]struct{}, len(other))

	for _, id := range other {
		have[id.String()] = struct{}{}
	}

	seen := make(map[string]struct{}, len(recipients))

	for _, id := range recipients {
		key := id.String()

		if _, ok := have[key]; ok {
			continue
		}

		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		missing = append(missing, key)
	}

	return missing
}

type KeyStatus struct {
	command_components.EnvBlobStore
	command_components.BlobStore
}

var _ futility.CommandWithParams = (*KeyStatus)(nil)

func (cmd *KeyStatus) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "store-id",
			Description: "sealed-key blob store to report on",
			Required:    true,
		},
	}
}

func (cmd KeyStatus) GetDescription() futility.Description {
	return futility.Description{
		Short: "compare a sealed-key store's recipients with its pigpen",
		Long: "For a blob store created with -pigpen, report the recipient " +
			"set the store key is sealed to, re-read the pigpen it was " +
			"sealed against, and report whether the two differ, listing " +
			"recipients added to or removed from the pigpen since. Needs " +
			"no key agent: nothing is opened. A pigpen behind a pointer is " +
			"fetched afresh, which also refreshes the copy cached on this " +
			"machine that ordinary commands check against. Exits non-zero " +
			"if the pigpen cannot be read. Use key-reseal to seal the " +
			"store key to the pigpen's current recipients.",
	}
}

func (cmd KeyStatus) Run(req futility.Request) {
	storeId := req.PopArg("store-id")
	req.AssertNoMoreArgs()

	env := cmd.MakeEnvBlobStore(req)

	store, ok := openSealedKeyStore(
		env,
		cmd.MakeBlobStoreFromIdString(env, storeId),
		storeId,
	)
	if !ok {
		return
	}

	out := env.GetUIFile()

	sealedDescription, err := describeRecipientSet(store.sealed)
	if err != nil {
		env.Cancel(err)
		return
	}

	source := blob_store_configs.StoreKeySource(store.sidecar)

	fmt.Fprintf(out, "pigpen:      %s\n", source)
	fmt.Fprintf(out, "sealed to:   %s\n", sealedDescription)

	// Live: this is the command that asks "what does the pigpen say now",
	// and it refreshes the cache ordinary commands compare against.
	current, err := source.Recipients(true)
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	currentDescription, err := describeRecipientSet(current)
	if err != nil {
		env.Cancel(err)
		return
	}

	fmt.Fprintf(out, "pigpen now:  %s\n", currentDescription)

	added := recipientsMissingFrom(current, store.sealed)
	removed := recipientsMissingFrom(store.sealed, current)

	for _, id := range added {
		fmt.Fprintf(out, "added:       %s\n", id)
	}

	for _, id := range removed {
		fmt.Fprintf(out, "removed:     %s\n", id)
	}

	if pigpen.SameRecipientSet(current, store.sealed) {
		fmt.Fprintf(out, "status:      in sync\n")
	} else {
		fmt.Fprintf(
			out,
			"status:      changed; run `madder key-reseal %s`\n",
			storeId,
		)
	}
}

const pigpenKindDescription = "-pigpen-kind says how -pigpen is read: " +
	"'path' (the default) is a local piggy-ids file, whose path is " +
	"recorded; 'papi' is a PAPI identity domain whose published pigpen " +
	"is fetched by pigpen-resolver-papi-http, so nothing specific to one " +
	"machine is recorded; 'embedded' is a file whose contents are copied " +
	"into blob_store-key and become the store's pigpen, edited there " +
	"from then on. A fetched pigpen is cached per machine: ordinary " +
	"commands compare against the cache and make no network request, " +
	"and key-status and key-reseal fetch afresh."

func setPigpenKindFlagDefinition(
	flagDefinitions interfaces.CLIFlagDefinitions,
	pigpenKind *string,
) {
	flagDefinitions.StringVar(
		pigpenKind,
		"pigpen-kind",
		store_key.SourceKindPath,
		"how -pigpen is read: 'path' (a local piggy-ids file), 'papi' (a "+
			"PAPI identity domain), or 'embedded' (a file whose contents "+
			"are copied into the store's blob_store-key)",
	)
}

type KeyReseal struct {
	pigpen     string
	pigpenKind string
	confirm    bool

	command_components.EnvBlobStore
	command_components.BlobStore
}

var (
	_ futility.CommandWithParams        = (*KeyReseal)(nil)
	_ interfaces.CommandComponentWriter = (*KeyReseal)(nil)
)

func (cmd *KeyReseal) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "store-id",
			Description: "sealed-key blob store to re-seal",
			Required:    true,
		},
	}
}

func (cmd *KeyReseal) SetFlagDefinitions(
	flagDefinitions interfaces.CLIFlagDefinitions,
) {
	flagDefinitions.StringVar(
		&cmd.pigpen,
		"pigpen",
		"",
		"seal to the recipients of this pigpen instead of the one recorded "+
			"at init, and record it as the store's pigpen from now on (for "+
			"a pigpen that has moved, or to change its kind)",
	)

	setPigpenKindFlagDefinition(flagDefinitions, &cmd.pigpenKind)

	flagDefinitions.BoolVar(
		&cmd.confirm,
		"confirm",
		false,
		"seal the store key to a recipient set that differs from the one "+
			"it is sealed to now. Without it, a changed set is listed and "+
			"nothing is written",
	)
}

func (cmd KeyReseal) GetDescription() futility.Description {
	return futility.Description{
		Short: "seal a store's key to its pigpen's current recipients",
		Long: "For a blob store created with -pigpen, open the sealed store " +
			"key through the key agent, re-read the pigpen, seal the SAME " +
			"store key to the pigpen's current recipients, and replace the " +
			"blob_store-key sidecar. No blob is read or rewritten. With " +
			"-pigpen <value> it seals to that pigpen instead and records " +
			"it as the store's pigpen, for one that has moved. " +
			pigpenKindDescription + " " +
			"If the recipient set would change, key-reseal lists what " +
			"would be added and removed and writes nothing unless " +
			"-confirm is given: the pigpen is trusted only as far as " +
			"wherever it was read from, and whoever it names gets the " +
			"store key. Adding " +
			"a recipient this way lets it read every existing blob. " +
			"Removing one only stops it opening the new sidecar: anyone " +
			"who kept the old sidecar or the store key can still read " +
			"every blob, so removal is not revocation. Real revocation " +
			"needs a new store and a sync.",
	}
}

func (cmd KeyReseal) Run(req futility.Request) {
	storeId := req.PopArg("store-id")
	req.AssertNoMoreArgs()

	env := cmd.MakeEnvBlobStore(req)

	store, ok := openSealedKeyStore(
		env,
		cmd.MakeBlobStoreFromIdString(env, storeId),
		storeId,
	)
	if !ok {
		return
	}

	source := blob_store_configs.StoreKeySource(store.sidecar)

	if cmd.pigpen != "" {
		var err error

		if source, err = store_key.MakeSource(cmd.pigpenKind, cmd.pigpen); err != nil {
			errors.ContextCancelWithBadRequestError(env, err)
			return
		}
	}

	// Live: the key is about to be sealed to this answer.
	recipients, err := source.Recipients(true)
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	added := recipientsMissingFrom(recipients, store.sealed)
	removed := recipientsMissingFrom(store.sealed, recipients)

	// Same test as the drift warning: piggy's set equality, not the ids'
	// spelling.
	if !pigpen.SameRecipientSet(recipients, store.sealed) && !cmd.confirm {
		out := env.GetUIFile()

		for _, id := range added {
			fmt.Fprintf(out, "added:       %s\n", id)
		}

		for _, id := range removed {
			fmt.Fprintf(out, "removed:     %s\n", id)
		}

		errors.ContextCancelWithBadRequestf(
			env,
			"the recipient set would change; re-run with -confirm to seal "+
				"the store key of %q to the set above. Nothing was written",
			storeId,
		)

		return
	}

	resealed, err := store_key.Reseal(
		[]byte(store.sidecar.Sealed.Document),
		store_key.AgentOpener,
		recipients,
	)
	if err != nil {
		env.Cancel(errors.Wrapf(err, "re-sealing the store key of %q", storeId))
		return
	}

	storeKey, err := blob_store_configs.MakeStoreKey(source, recipients, resealed)
	if err != nil {
		env.Cancel(err)
		return
	}

	sidecar, err := blob_store_configs.EncodeStoreKey(storeKey)
	if err != nil {
		env.Cancel(err)
		return
	}

	if err = store.access.WriteStoreKeySidecar(sidecar); err != nil {
		env.Cancel(errors.Wrapf(err, "writing the store key sidecar of %q", storeId))
		return
	}

	description, err := describeRecipientSet(recipients)
	if err != nil {
		env.Cancel(err)
		return
	}

	fmt.Fprintf(env.GetUIFile(), "re-sealed %s to %s\n", storeId, description)
}
