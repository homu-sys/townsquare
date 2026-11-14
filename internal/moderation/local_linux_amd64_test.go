//go:build linux && amd64

package moderation

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestLocalClassifierScoresSafeImage(t *testing.T) {
	checker, err := New()
	if err != nil {
		t.Fatalf("load classifier: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, InputSize, InputSize))
	for y := 0; y < InputSize; y++ {
		for x := 0; x < InputSize; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	verdict, err := checker.Check(img)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	sum := verdict.NSFL + verdict.NSFW + verdict.SFW
	if math.Abs(sum-1) > 0.01 {
		t.Fatalf("probabilities sum to %f: %+v", sum, verdict)
	}
	if verdict.SFW < 0.5 {
		t.Fatalf("safe test image scored as unsafe: %+v", verdict)
	}
	if msg := verdict.Rejection(0.60, 0.60); msg != "" {
		t.Fatalf("safe test image rejected: %s (%+v)", msg, verdict)
	}
}
