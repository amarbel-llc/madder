package sftp_probe

import "testing"

func TestStageString(t *testing.T) {
	cases := []struct {
		stage Stage
		want  string
	}{
		{StageOK, "ok"},
		{StageDecrypt, "decrypt"},
		{StageDecompress, "decompress"},
		{StageHashMismatch, "hash_mismatch"},
	}

	for _, tc := range cases {
		if got := tc.stage.String(); got != tc.want {
			t.Errorf("Stage(%d).String() = %q, want %q", tc.stage, got, tc.want)
		}
	}
}
