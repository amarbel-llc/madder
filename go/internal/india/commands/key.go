package commands

import (
	"fmt"
	"path/filepath"

	"code.linenisgreat.com/madder/go/internal/charlie/store_key"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
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
	sidecar blob_store_configs.TomlStoreKeyV1
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
			"no key agent: nothing is opened. Exits non-zero if the pigpen " +
			"cannot be read. Use key-reseal to seal the store key to the " +
			"pigpen's current recipients.",
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

	source := store.sidecar.Recipients.Source

	fmt.Fprintf(out, "pigpen:      %s\n", source)
	fmt.Fprintf(out, "sealed to:   %s\n", sealedDescription)

	current, err := store_key.LoadRecipients(source)
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

	if len(added) == 0 && len(removed) == 0 {
		fmt.Fprintf(out, "status:      in sync\n")
	} else {
		fmt.Fprintf(
			out,
			"status:      changed; run `madder key-reseal %s`\n",
			storeId,
		)
	}
}

type KeyReseal struct {
	pigpen string

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
			"a pigpen that has moved)",
	)
}

func (cmd KeyReseal) GetDescription() futility.Description {
	return futility.Description{
		Short: "seal a store's key to its pigpen's current recipients",
		Long: "For a blob store created with -pigpen, open the sealed store " +
			"key through the key agent, re-read the pigpen, seal the SAME " +
			"store key to the pigpen's current recipients, and replace the " +
			"blob_store-key sidecar. No blob is read or rewritten. With " +
			"-pigpen <path> it seals to that pigpen instead and records " +
			"it as the store's pigpen, for one that has moved. Adding " +
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

	source := store.sidecar.Recipients.Source

	if cmd.pigpen != "" {
		var err error

		if source, err = filepath.Abs(cmd.pigpen); err != nil {
			env.Cancel(err)
			return
		}
	}

	recipients, err := store_key.LoadRecipients(source)
	if err != nil {
		errors.ContextCancelWithBadRequestError(env, err)
		return
	}

	digest, err := store_key.RecipientSetDigest(recipients)
	if err != nil {
		env.Cancel(err)
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

	sidecar, err := blob_store_configs.EncodeStoreKey(
		blob_store_configs.TomlStoreKeyV1{
			Recipients: blob_store_configs.StoreKeyRecipients{
				Source: source,
				Digest: digest,
			},
			Sealed: blob_store_configs.StoreKeySealed{Document: string(resealed)},
		},
	)
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
