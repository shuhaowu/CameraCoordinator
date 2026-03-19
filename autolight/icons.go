package main

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"

	"deedles.dev/tray"
)

var (
	//go:embed assets/autolight-camera-on-32.png
	cameraOnData32 []byte
	//go:embed assets/autolight-camera-on-64.png
	cameraOnData64 []byte
	//go:embed assets/autolight-camera-on-128.png
	cameraOnData128 []byte
	//go:embed assets/autolight-camera-on-256.png
	cameraOnData256 []byte

	//go:embed assets/autolight-camera-off-32.png
	cameraOffData32 []byte
	//go:embed assets/autolight-camera-off-64.png
	cameraOffData64 []byte
	//go:embed assets/autolight-camera-off-128.png
	cameraOffData128 []byte
	//go:embed assets/autolight-camera-off-256.png
	cameraOffData256 []byte

	//go:embed assets/autolight-camera-unknown-32.png
	cameraUnknownData32 []byte
	//go:embed assets/autolight-camera-unknown-64.png
	cameraUnknownData64 []byte
	//go:embed assets/autolight-camera-unknown-128.png
	cameraUnknownData128 []byte
	//go:embed assets/autolight-camera-unknown-256.png
	cameraUnknownData256 []byte

	cameraOnIcons      []image.Image
	cameraOffIcons     []image.Image
	cameraUnknownIcons []image.Image
)

func mustDecodePixmap(data []byte) tray.Pixmap {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	return tray.ToPixmap(img)
}

func init() {
	cameraOnIcons = []image.Image{
		mustDecodePixmap(cameraOnData32),
		mustDecodePixmap(cameraOnData64),
		mustDecodePixmap(cameraOnData128),
		mustDecodePixmap(cameraOnData256),
	}
	cameraOffIcons = []image.Image{
		mustDecodePixmap(cameraOffData32),
		mustDecodePixmap(cameraOffData64),
		mustDecodePixmap(cameraOffData128),
		mustDecodePixmap(cameraOffData256),
	}
	cameraUnknownIcons = []image.Image{
		mustDecodePixmap(cameraUnknownData32),
		mustDecodePixmap(cameraUnknownData64),
		mustDecodePixmap(cameraUnknownData128),
		mustDecodePixmap(cameraUnknownData256),
	}
}
