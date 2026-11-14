package moderation

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

func pixels(img image.Image) []float32 {
	scaled := resizeBilinear(img, InputSize)
	out := make([]float32, 3*InputSize*InputSize)
	plane := InputSize * InputSize
	for y := 0; y < InputSize; y++ {
		for x := 0; x < InputSize; x++ {
			c := scaled.RGBAAt(x, y)
			i := y*InputSize + x
			out[i] = float32(c.R)
			out[plane+i] = float32(c.G)
			out[2*plane+i] = float32(c.B)
		}
	}
	return out
}

func resizeBilinear(src image.Image, size int) *image.RGBA {
	b := src.Bounds()
	canvas := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), src, b.Min, draw.Over)

	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	if b.Dx() < 1 || b.Dy() < 1 {
		return dst
	}
	for y := 0; y < size; y++ {
		sy := (float64(y)+0.5)*float64(b.Dy())/float64(size) - 0.5
		y0 := int(math.Floor(sy))
		fy := sy - float64(y0)
		if y0 < 0 {
			y0, fy = 0, 0
		}
		y1 := y0 + 1
		if y1 >= b.Dy() {
			y1, fy = b.Dy()-1, 0
		}
		for x := 0; x < size; x++ {
			sx := (float64(x)+0.5)*float64(b.Dx())/float64(size) - 0.5
			x0 := int(math.Floor(sx))
			fx := sx - float64(x0)
			if x0 < 0 {
				x0, fx = 0, 0
			}
			x1 := x0 + 1
			if x1 >= b.Dx() {
				x1, fx = b.Dx()-1, 0
			}
			c00 := canvas.RGBAAt(x0, y0)
			c10 := canvas.RGBAAt(x1, y0)
			c01 := canvas.RGBAAt(x0, y1)
			c11 := canvas.RGBAAt(x1, y1)
			dst.SetRGBA(x, y, color.RGBA{
				R: byte(bilerp(float64(c00.R), float64(c10.R), float64(c01.R), float64(c11.R), fx, fy)),
				G: byte(bilerp(float64(c00.G), float64(c10.G), float64(c01.G), float64(c11.G), fx, fy)),
				B: byte(bilerp(float64(c00.B), float64(c10.B), float64(c01.B), float64(c11.B), fx, fy)),
				A: 255,
			})
		}
	}
	return dst
}

func bilerp(c00, c10, c01, c11, fx, fy float64) float64 {
	top := c00 + (c10-c00)*fx
	bottom := c01 + (c11-c01)*fx
	v := top + (bottom-top)*fy
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v + 0.5
}
