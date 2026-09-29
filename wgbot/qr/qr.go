// Package qr renders client configs as QR code PNG images.
package qr

import (
	qrcode "github.com/skip2/go-qrcode"
)

// PNG renders a WireGuard client config into a QR code.
func PNG(content string) ([]byte, error) {
	return qrcode.Encode(content, qrcode.Medium, 512)
}
