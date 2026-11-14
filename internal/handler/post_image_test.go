package handler

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestPostImageReencode(t *testing.T) {
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	output, err := normalizePostImage(source.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	config, kind, err := image.DecodeConfig(bytes.NewReader(output))
	if err != nil || kind != "jpeg" || config.Width != 2 {
		t.Fatalf("%v %s %v", config, kind, err)
	}
	for _, data := range [][]byte{[]byte("<svg></svg>"), source.Bytes()[:12], make([]byte, (2<<20)+1)} {
		if _, err := normalizePostImage(data); err == nil {
			t.Fatal("unsafe image accepted")
		}
	}
}
