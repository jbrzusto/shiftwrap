package main

import (
	"flag"
	"fmt"
	"os"
)

func Usage() {
	fmt.Print(`
Usage:

  sw ...

Control shiftwrapd via http requests.

FLAGS:

`)
	flag.PrintDefaults()
	os.Exit(0)
}

func main() {
}
