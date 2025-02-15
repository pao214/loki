//go:build ignore
// +build ignore

package main

import (
	"log"
	"time"

	"github.com/prometheus/prometheus/util/modtimevfs"
	"github.com/shurcooL/vfsgen"

	"github.com/pao214/loki/clients/pkg/promtail/server/ui"
)

func main() {
	fs := modtimevfs.New(ui.Assets, time.Unix(1, 0))
	err := vfsgen.Generate(fs, vfsgen.Options{
		PackageName:  "ui",
		BuildTags:    "!dev",
		VariableName: "Assets",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
