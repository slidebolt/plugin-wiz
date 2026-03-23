package main

import (
	runtime "github.com/slidebolt/sb-runtime"

	"github.com/slidebolt/plugin-wiz/app"
)

func main() { runtime.Run(app.New()) }
