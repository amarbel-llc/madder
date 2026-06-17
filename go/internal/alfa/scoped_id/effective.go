package scoped_id

// DefaultName is the conventional name an unnamed scoped id resolves to —
// the user `default` store, dodder's `default` repo, etc. (FDR-0019).
const DefaultName = "default"

// EffectiveName returns id's name, or DefaultName when id is unnamed. It is
// the shared empty->default resolver for the FDR-0019 #3 ruling: PURE
// name-default — NOT walk-up-aware (nearest-ancestor resolution stays with
// the caller's discovery / MakeDefault; this only supplies the
// name fallback). dodder delegates here, dropping its local
// repo_id.EffectiveName (dodder#274), so the two consumers can't diverge
// on what "the default" is.
//
// Note `Set("")` still errors — empty is invalid INPUT; resolving an
// already-parsed unnamed id to a default is a separate, opt-in step.
func EffectiveName(id Id) string {
	if name := id.GetName(); name != "" {
		return name
	}

	return DefaultName
}

// EffectiveId returns id with its name forced to EffectiveName(id),
// preserving the location type — for feeding constructors that derive a
// repo/store name from the id (e.g. MakeDefaultAndInitialize). cwdDepth and
// any digest are dropped, which is safe because consumers gate
// multi-dot/system ids before this resolution.
func EffectiveId(id Id) Id {
	return MakeWithLocation(EffectiveName(id), id.GetLocationType())
}
