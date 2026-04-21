package futility

import (
	"testing"
)

func TestParamTypeString(t *testing.T) {
	tests := []struct {
		pt   ParamType
		want string
	}{
		{String, "string"},
		{Int, "integer"},
		{Bool, "boolean"},
		{Float, "number"},
		{Array, "array"},
	}
	for _, tt := range tests {
		if got := tt.pt.JSONSchemaType(); got != tt.want {
			t.Errorf("ParamType(%d).JSONSchemaType() = %q, want %q", tt.pt, got, tt.want)
		}
	}
}

func TestCommandHasRun(t *testing.T) {
	cmd := Command{
		Name: "status",
		Run: func(req Request) (*Result, error) {
			return TextResult("ok"), nil
		},
	}
	if cmd.Run == nil {
		t.Error("Run should be set")
	}
}

func TestCommandParamsRequired(t *testing.T) {
	cmd := Command{
		Name:        "status",
		Description: Description{Short: "Show status"},
		Params: []Param{
			StringFlag{Name: "repo_path", Description: "Path to repo", Required: true},
			BoolFlag{Name: "verbose", Description: "Verbose output"},
		},
	}

	required := cmd.RequiredParams()
	if len(required) != 1 {
		t.Fatalf("RequiredParams() len = %d, want 1", len(required))
	}
	if required[0].paramName() != "repo_path" {
		t.Errorf("RequiredParams()[0].paramName() = %q, want %q", required[0].paramName(), "repo_path")
	}
}

func TestParamShortFieldZeroValueMeansNoShortFlag(t *testing.T) {
	p := BoolFlag{Name: "verbose", Description: "Verbose output"}
	if p.Short != 0 {
		t.Errorf("Short zero value = %q, want 0", p.Short)
	}
}

func TestParamShortFieldSet(t *testing.T) {
	p := BoolFlag{Name: "verbose", Description: "Verbose output", Short: 'v'}
	if p.Short != 'v' {
		t.Errorf("Short = %q, want 'v'", p.Short)
	}
}
