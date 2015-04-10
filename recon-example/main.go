package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jimmysawczuk/recon"
)

func main() {
	url := os.Args[1]

	parser := recon.NewParser()
	res, _ := parser.Parse(url)

	jres, _ := json.MarshalIndent(res, "", "   ")
	fmt.Println(string(jres))
}
