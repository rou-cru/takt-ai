package opencode_test

import (
	"os"
	"testing"
)

func writeHistoryFixture(t *testing.T, write func(string, []byte)) {
	t.Helper()
	data, err := os.ReadFile("testdata/history-fixture.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("history-fixture.mjs", data)
}

// writeOpenCodeClient installs an OpenCode client double whose permission
// service answers through globalThis.createPermission, so each script decides
// what the user replies; without one, no user can be asked.
func writeOpenCodeClient(write func(string, []byte)) {
	write("node_modules/@opencode/client/package.json", []byte(`{"type":"module","exports":{".":"./index.js","./service":"./service.js"}}`))
	write("node_modules/@opencode/client/index.js", []byte(`export const OpenCode={make:()=>({permission:{create:r=>globalThis.createPermission?globalThis.createPermission(r):Promise.reject(new Error("no user to ask"))}})}`))
	write("node_modules/@opencode/client/service.js", []byte(`export const Service={discover:async()=>({url:"http://fixture"}),headers:()=>({})}`))
}
