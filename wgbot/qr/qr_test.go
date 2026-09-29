package qr

import "testing"

func TestPNG(t *testing.T) {
	png, err := PNG("[Interface]\nPrivateKey = test\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 300 {
		t.Fatalf("suspiciously small PNG: %d bytes", len(png))
	}
}
