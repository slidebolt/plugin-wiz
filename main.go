package main

import (
	"log"

	runner "github.com/slidebolt/sdk-runner"
)

func main() {
	if err := runner.NewRunner(NewWizPlugin(nil)).Run(); err != nil {
		log.Fatal(err)
	}
}
