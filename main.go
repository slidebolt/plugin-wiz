package main

import (
	"log"

	runner "github.com/slidebolt/sdk-runner"
)

func main() {
	r, err := runner.NewRunner(NewWizPlugin(nil))
	if err != nil {
		log.Fatal(err)
	}
	if err := r.Run(); err != nil {
		log.Fatal(err)
	}
}
