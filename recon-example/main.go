package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jimmysawczuk/recon"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Must specify a URL\n")
	}

	url := os.Args[1]

	res, err := recon.Parse(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %s\n", url, err)
	}

	jres, _ := json.MarshalIndent(res, "", "   ")
	fmt.Println("Recon parse results for:\n" + url)
	fmt.Println("------------------------------------------------")
	fmt.Println(string(jres))
}
