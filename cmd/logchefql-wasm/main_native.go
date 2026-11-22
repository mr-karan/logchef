//go:build !js

package main

import (
	"encoding/json"
	"log"
	"os"
)

// Generate the examples Astro renders before the visitor starts editing.
func main() {
	var examples []struct {
		Label  string      `json:"label"`
		Query  string      `json:"query"`
		Result compilation `json:"result"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&examples); err != nil {
		log.Fatal(err)
	}
	for i, example := range examples {
		examples[i].Result = compile(example.Query, "5m")
	}
	if err := json.NewEncoder(os.Stdout).Encode(examples); err != nil {
		log.Fatal(err)
	}
}
