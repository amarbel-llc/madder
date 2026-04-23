package buildinfo

var Version = "dev"
var Commit  = "unknown"

func Set(v, c string) {
	Version = v
	Commit  = c
}

func String() string {
	return Version + "+" + Commit
}
