// jellytool-pack copies a locally supplied, pinned Bun into app resources.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yetone/magpie/internal/plugin"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: jellytool-pack /absolute/bun /absolute/empty-output")
		os.Exit(2)
	}
	if err := plugin.PackJellytoolPlugins(context.Background(), os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
