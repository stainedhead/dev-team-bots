//go:build ignore

// gen-icons pre-computes the two processed variants of boabot-icon.png and
// writes them into the imgs/ package so they can be embedded at compile time.
//
// Run via: go generate ./imgs/...
package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	raw, err := os.ReadFile("boabot-icon.png")
	must(err)

	must(os.WriteFile("boabot-icon-processed.png", makeDarkPixelsTransparent(raw), 0o644))
	must(os.WriteFile("boabot-icon-favicon.png", applyBlueWhiteFilter(raw), 0o644))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func makeDarkPixelsTransparent(pngBytes []byte) []byte {
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return pngBytes
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			r8, g8, b8, a8 := uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)
			luma := (uint32(r8)*299 + uint32(g8)*587 + uint32(b8)*114) / 1000
			if luma < 50 {
				dst.Set(x, y, color.RGBA{})
			} else {
				dst.Set(x, y, color.RGBA{R: r8, G: g8, B: b8, A: a8})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return pngBytes
	}
	return buf.Bytes()
}

func applyBlueWhiteFilter(pngBytes []byte) []byte {
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return pngBytes
	}
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			rf, gf, bf := float64(r>>8), float64(g>>8), float64(b>>8)

			// invert(1)
			rf, gf, bf = 255-rf, 255-gf, 255-bf

			// sepia(1)
			rf, gf, bf = rf*0.393+gf*0.769+bf*0.189,
				rf*0.349+gf*0.686+bf*0.168,
				rf*0.272+gf*0.534+bf*0.131

			// saturate(4) then hue-rotate(190deg) — work in HSL
			h, s, l := rgbToHSL(clamp255(rf), clamp255(gf), clamp255(bf))
			s = math.Min(1.0, s*4.0)
			h = math.Mod(h+190.0, 360.0)
			rf, gf, bf = hslToRGB(h, s, l)

			dst.Set(x, y, color.RGBA{R: uint8(rf), G: uint8(gf), B: uint8(bf), A: uint8(a >> 8)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return pngBytes
	}
	return buf.Bytes()
}

func clamp255(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	r, g, b = r/255, g/255, b/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h * 60, s, l
}

func hslToRGB(h, s, l float64) (r, g, b float64) {
	if s == 0 {
		v := l * 255
		return v, v, v
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hk := h / 360
	hue2rgb := func(t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		default:
			return p
		}
	}
	return hue2rgb(hk+1.0/3) * 255, hue2rgb(hk) * 255, hue2rgb(hk-1.0/3) * 255
}
