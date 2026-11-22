//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"
)

func main() {
	js.Global().Set("compile", js.FuncOf(func(_ js.Value, args []js.Value) any {
		interval := "5m"
		if len(args) > 1 && args[1].Type() == js.TypeObject {
			if v := args[1].Get("interval"); v.Type() == js.TypeString {
				interval = v.String()
			}
		}
		b, err := json.Marshal(compile(args[0].String(), interval))
		if err != nil {
			panic(err)
		}
		return string(b)
	}))
	select {}
}
