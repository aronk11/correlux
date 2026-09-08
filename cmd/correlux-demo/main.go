//go:build js && wasm && correlux_demo

// Correlux's browser entry point exposes only a synchronous, fixture-backed UI.
package main

import (
	"encoding/json"
	"github.com/aronk11/correlux/internal/ui/app"
	"syscall/js"
)

func main() {
	demo := app.NewBrowserDemo()
	step := js.FuncOf(func(this js.Value, args []js.Value) any {
		var in app.DemoInput
		if len(args) > 0 {
			if err := json.Unmarshal([]byte(args[0].String()), &in); err != nil {
				return `{"error":"Invalid demo input"}`
			}
		}
		if in.Screen == "reset" {
			demo = app.NewBrowserDemo()
		}
		out, _ := json.Marshal(demo.Step(in))
		return string(out)
	})
	js.Global().Set("correluxDemoStep", step)
	select {}
}
