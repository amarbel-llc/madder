package domain_interfaces

// The markl interface vocabulary is owned upstream by piggy's go
// module (piggy#183 ownership inversion). These aliases keep madder's
// domain_interfaces as the single import for domain interfaces — the
// blob-store and config interfaces in this package reference MarklId
// et al., so the aliases pin one coherent type-identity source without
// forcing dual imports on every consumer.

import (
	"code.linenisgreat.com/piggy/go/pkgs/domain_interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

type (
	MarklFormat       = domain_interfaces.MarklFormat
	FormatHash        = domain_interfaces.FormatHash
	MarklFormatGetter = domain_interfaces.MarklFormatGetter
	Hash              = domain_interfaces.Hash
	MarklId           = domain_interfaces.MarklId
	MarklIdMutable    = domain_interfaces.MarklIdMutable
	MarklIdGetter     = domain_interfaces.MarklIdGetter
	DigestWriteMap    = domain_interfaces.DigestWriteMap
)

type (
	Lock[
		KEY interfaces.Value,
		KEY_PTR interfaces.ValuePtr[KEY],
	] = domain_interfaces.Lock[KEY, KEY_PTR]

	LockMutable[
		KEY interfaces.Value,
		KEY_PTR interfaces.ValuePtr[KEY],
	] = domain_interfaces.LockMutable[KEY, KEY_PTR]
)
