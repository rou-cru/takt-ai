package opencode

// The canonical adapter is tested in runtime/sandbox. Generate the embedded
// installation artifact rather than maintaining a second implementation.
//go:generate cp ../../runtime/sandbox/adapter.mjs assets/takt-sandbox.mjs
