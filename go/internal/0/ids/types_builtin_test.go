//go:build test

package ids

import "testing"

func TestMultiV0Registered(t *testing.T) {
	bt := GetOrPanic(TypeTomlBlobStoreConfigMultiV0)
	if bt.TypeStruct.String() != TypeTomlBlobStoreConfigMultiV0 {
		t.Fatalf("round-trip: got %q, want %q",
			bt.TypeStruct.String(), TypeTomlBlobStoreConfigMultiV0)
	}
}
