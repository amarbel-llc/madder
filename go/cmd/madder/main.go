package main

import (
	"os"

	"github.com/amarbel-llc/madder/go/internal/india/commands_madder"
)

func main() {
	utility := commands_madder.GetUtility()
	utility.Run(os.Args)
}
