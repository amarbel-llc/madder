package env_dir

import (
	"github.com/amarbel-llc/madder/go/internal/0/xdg_location_type"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

type RepoId struct {
	locationType xdg_location_type.Typee
	isSet        bool
}

func (id RepoId) IsEmpty() bool {
	return !id.isSet
}

func (id RepoId) GetLocationType() xdg_location_type.Type {
	return id.locationType
}

func (id *RepoId) Set(value string) (err error) {
	switch value {
	case "":
		id.isSet = false
		return nil

	case ".":
		id.locationType = xdg_location_type.Cwd
		id.isSet = true
		return nil

	case "/":
		id.locationType = xdg_location_type.XDGSystem
		id.isSet = true
		return nil

	default:
		if len(value) > 1 && value[0] == '/' {
			err = errors.Errorf(
				"remote repo selection (/%s) not yet implemented",
				value[1:],
			)
			return err
		}

		err = errors.Errorf("invalid repo_id: %q (expected . or /)", value)
		return err
	}
}

func (id RepoId) String() string {
	if !id.isSet {
		return ""
	}

	prefix := id.locationType.GetPrefix()
	if prefix == 0 {
		return ""
	}

	return string(prefix)
}

func (id RepoId) IsCwd() bool {
	return id.isSet && id.locationType == xdg_location_type.Cwd
}

func (id RepoId) IsSystem() bool {
	return id.isSet && id.locationType == xdg_location_type.XDGSystem
}
