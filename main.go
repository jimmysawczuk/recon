package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	url := os.Args[1]

	parser := NewParser()
	res, _ := parser.Parse(url)

	jres, _ := json.MarshalIndent(res, "", "   ")
	fmt.Println(string(jres))
}
