package sku

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/0/external_state"
	"github.com/amarbel-llc/madder/go/internal/bravo/ids"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

type (
	Config interface {
		domain_interfaces.Config
		ids.InlineTypeChecker // TODO move out of konfig entirely
	}

	TransactedGetter interface {
		GetSku() *Transacted
	}

	ObjectWithList struct {
		Object, List *Transacted
	}

	ExternalLike interface {
		ids.ObjectIdGetter
		interfaces.Stringer
		TransactedGetter
		ExternalLikeGetter
		GetExternalState() external_state.State
		ExternalObjectIdGetter
		GetRepoId() ids.RepoId
	}

	ExternalLikeGetter interface {
		GetSkuExternal() *Transacted
	}
)
