package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println("0.0.0-ci-broken-service")
		return
	}
	os.Exit(23)
}
