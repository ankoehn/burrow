// Command gencerts writes a local dev CA + localhost server cert. DEV ONLY.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ankoehn/burrow/internal/devcert"
)

func main() {
	dir := flag.String("dir", "certs", "output directory")
	force := flag.Bool("force", false, "overwrite existing certs")
	flag.Parse()
	if err := devcert.Generate(*dir, *force); err != nil {
		fmt.Fprintln(os.Stderr, "gencerts:", err)
		os.Exit(1)
	}
	fmt.Printf("dev certs written to %s/ (DEV ONLY - do not use in production)\n", *dir)
}
