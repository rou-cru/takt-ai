package styles

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi/kitty"
)

// The logo travels in chunks the protocol accepts, the first one naming it
// and every one but the last announcing more, and arrives as the embedded
// PNG.
func TestTransmitLogoChunksTheEmbeddedImage(t *testing.T) {
	commands := strings.Split(strings.TrimSuffix(TransmitLogo(), "\x1b\\"), "\x1b\\")
	if len(commands) < 2 {
		t.Fatalf("TransmitLogo() sent %d command(s), want the image in several chunks", len(commands))
	}
	var payload strings.Builder
	for index, command := range commands {
		keys, data, found := strings.Cut(strings.TrimPrefix(command, "\x1b_G"), ";")
		if !found || len(data) > kitty.MaxChunkSize {
			t.Fatalf("chunk %d is malformed or larger than %d bytes", index, kitty.MaxChunkSize)
		}
		last := index == len(commands)-1
		if strings.HasSuffix(keys, "m=0") != last {
			t.Errorf("chunk %d keys %q: only the last one ends the transfer", index, keys)
		}
		if index == 0 && !strings.HasPrefix(keys, "a=t,f=100,i=7316,q=2,") {
			t.Errorf("first chunk keys %q, want a quiet PNG transfer naming the logo", keys)
		}
		payload.WriteString(data)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil || !bytes.Equal(decoded, logoPNG) {
		t.Fatalf("transmitted payload is not the embedded PNG (err %v)", err)
	}
	mark, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		t.Fatal(err)
	}
	if bounds := mark.Bounds(); bounds.Dx() != bounds.Dy() {
		t.Errorf("embedded logo is %v, want square", bounds)
	}
	if _, _, _, alpha := mark.At(0, 0).RGBA(); alpha != 0 {
		t.Error("embedded logo corner is opaque, want the background cleared")
	}
}

// Placing draws over the given cells without moving the cursor the renderer
// tracks; hiding keeps the image stored and forgetting frees it.
func TestPlaceHideAndForgetLogo(t *testing.T) {
	want := "\x1b7\x1b[4;3H\x1b_Ga=p,i=7316,p=1,c=24,r=12,C=1,q=2\x1b\\\x1b8"
	if got := PlaceLogo(2, 3, 24, 12); got != want {
		t.Errorf("PlaceLogo() = %q, want %q", got, want)
	}
	if got := HideLogo(); got != "\x1b_Ga=d,d=i,i=7316,q=2\x1b\\" {
		t.Errorf("HideLogo() = %q", got)
	}
	if got := ForgetLogo(); got != "\x1b_Ga=d,d=I,i=7316,q=2\x1b\\" {
		t.Errorf("ForgetLogo() = %q", got)
	}
}
