//go:build js && wasm

// mai-wasm is the browser adapter for the production workflow engine. It
// answers the same JSON requests as mai-runner, through a JavaScript function
// instead of stdin and stdout, so Mai Studio runs in a browser with no
// desktop app, no installed tools and no network. Files live in the file
// system the page provides as globalThis.fs (an in-memory one in Studio).
//
//	globalThis.maiRunner(requestJSON) -> Promise<responseJSON>
package main

import (
	"context"
	"encoding/json"
	"syscall/js"

	"miservice/internal/studio"
)

func main() {
	handle := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].Type() != js.TypeString {
			return js.ValueOf(`{"version":1,"ok":false,"error":{"code":"invalid_request","message":"Expected a JSON request"}}`)
		}
		text := args[0].String()
		var resolve, reject js.Value
		executor := js.FuncOf(func(_ js.Value, a []js.Value) any {
			resolve, reject = a[0], a[1]
			return nil
		})
		promise := js.Global().Get("Promise").New(executor)
		executor.Release()
		// Calls that wait on JavaScript (file system callbacks) must not block
		// the event loop, so each request runs in its own goroutine.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					reject.Invoke(js.Global().Get("Error").New("The workflow engine stopped unexpectedly"))
				}
			}()
			resolve.Invoke(respond(text))
		}()
		return promise
	})
	js.Global().Set("maiRunner", handle)
	if ready := js.Global().Get("maiRunnerReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}
	select {}
}

func respond(text string) string {
	var req studio.Request
	var resp studio.Response
	if err := json.Unmarshal([]byte(text), &req); err != nil {
		resp = studio.Response{Version: studio.Version, Error: &studio.Failure{Code: "invalid_request", Message: "Expected a JSON request"}}
	} else {
		resp = studio.Handle(context.Background(), req)
	}
	encoded, err := json.Marshal(resp)
	if err == nil && len(encoded) > studio.MaxFrameBytes {
		resp = studio.Response{Version: studio.Version, ID: req.ID, Error: &studio.Failure{Code: "response_too_large", Message: "Response exceeds 1 MiB; request fewer preview rows"}}
		encoded, err = json.Marshal(resp)
	}
	if err != nil {
		return `{"version":1,"ok":false,"error":{"code":"operation_failed","message":"Cannot encode the response"}}`
	}
	return string(encoded)
}
