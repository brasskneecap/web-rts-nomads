// Command cliffatlas generates structured cliff auto-tile atlases from the
// decorative *-elevation-25 terrain scenes.
//
// The -25 sheets are continuous scenes, not auto-tile atlases. This tool cuts
// the reusable cliff pieces (flat top, straight rock face, vertical wall, outer
// corner) out of each scene and mirrors/composites them into a fixed canonical
// 4x4 slot layout that tiles seamlessly in any combination. All -25 sheets
// share the same cliff cell layout, so one extraction recipe serves every
// terrain.
//
// Canonical 4x4 slot layout (col,row), 160px tiles:
//
//	NW=(0,0)   N=(1,0)    NE=(2,0)  flat=(3,0)
//	W =(0,1)   FLAT=(1,1) E =(2,1)  NEi=(3,1)
//	SW=(0,2)   S=(1,2)    SE=(2,2)  NWi=(3,2)
//	flat=(0,3) flat=(1,3) SWi=(2,3) SEi=(3,3)
//
// The auto-tiler (client + server) indexes these slots; see the design spec
// docs/superpowers/specs/2026-07-23-smart-cliff-tool-design.md.
//
// Usage (from the server/ dir): go run ./cmd/cliffatlas [imagesDir]
// imagesDir defaults to internal/game/catalog/tilesets/images.
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const T = 160 // source/atlas tile size (px)

// terrains maps the output cliff tileset name to its -25 source sheet stem.
var terrains = map[string]string{
	"grass":      "grass-grass",
	"dirt":       "dirt-dirt",
	"dirt-grass": "dirt-grass",
	"snow":       "snow-snow",
	"corrupt":    "corrupt-corrupt",
}

func main() {
	imagesDir := filepath.Join("internal", "game", "catalog", "tilesets", "images")
	if len(os.Args) > 1 {
		imagesDir = os.Args[1]
	}
	for name, src := range terrains {
		atlas := genAtlas(filepath.Join(imagesDir, src+"-elevation-25.png"))
		out := filepath.Join(imagesDir, name+"-cliff.png")
		f, err := os.Create(out)
		if err != nil {
			panic(err)
		}
		if err := png.Encode(f, atlas); err != nil {
			panic(err)
		}
		f.Close()
		println("wrote", out)
	}
}

func genAtlas(srcPath string) *image.RGBA {
	src := load(srcPath)
	flat := sub(src, 2, 1)  // pure interior grass
	sWall := sub(src, 3, 0) // horizontal rock face (plateau above, drop below)
	vWall := sub(src, 1, 0) // vertical rock face
	oc := sub(src, 0, 0)    // outer corner (rock wraps top+right)

	N, S, W, E := flipV(sWall), sWall, vWall, flipH(vWall)
	NW, NE, SE, SW := flipH(oc), oc, flipV(oc), flipH(flipV(oc))
	NEi := innerCorner(flat, NE, T-1, 0)
	NWi := innerCorner(flat, NW, 0, 0)
	SEi := innerCorner(flat, SE, T-1, T-1)
	SWi := innerCorner(flat, SW, 0, T-1)

	a := image.NewRGBA(image.Rect(0, 0, 4*T, 4*T))
	blit(a, NW, 0, 0)
	blit(a, N, 1, 0)
	blit(a, NE, 2, 0)
	blit(a, flat, 3, 0)
	blit(a, W, 0, 1)
	blit(a, flat, 1, 1)
	blit(a, E, 2, 1)
	blit(a, NEi, 3, 1)
	blit(a, SW, 0, 2)
	blit(a, S, 1, 2)
	blit(a, SE, 2, 2)
	blit(a, NWi, 3, 2)
	blit(a, flat, 0, 3)
	blit(a, flat, 1, 3)
	blit(a, SWi, 2, 3)
	blit(a, SEi, 3, 3)
	return a
}

// innerCorner composites a rock nub near corner (ncx,ncy) of cornerTile onto
// flat, feathered by distance so concave (inner) corners show a small notch.
func innerCorner(flat, cornerTile *image.RGBA, ncx, ncy int) *image.RGBA {
	d := image.NewRGBA(image.Rect(0, 0, T, T))
	draw.Draw(d, d.Bounds(), flat, image.Pt(0, 0), draw.Src)
	const R = 82.0
	for y := 0; y < T; y++ {
		for x := 0; x < T; x++ {
			dist := math.Hypot(float64(x-ncx), float64(y-ncy))
			if dist >= R {
				continue
			}
			w := 1 - dist/R
			w = w * w
			fr, fg, fb := at8(flat, x, y)
			cr, cg, cb := at8(cornerTile, x, y)
			d.SetRGBA(x, y, color.RGBA{
				uint8(fr*(1-w) + cr*w),
				uint8(fg*(1-w) + cg*w),
				uint8(fb*(1-w) + cb*w),
				255,
			})
		}
	}
	return d
}

func load(p string) *image.RGBA {
	f, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	b := im.Bounds()
	r := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(r, r.Bounds(), im, b.Min, draw.Src)
	return r
}

func sub(src *image.RGBA, cx, cy int) *image.RGBA {
	d := image.NewRGBA(image.Rect(0, 0, T, T))
	draw.Draw(d, d.Bounds(), src, image.Pt(cx*T, cy*T), draw.Src)
	return d
}

func flipH(s *image.RGBA) *image.RGBA {
	d := image.NewRGBA(s.Bounds())
	for y := 0; y < T; y++ {
		for x := 0; x < T; x++ {
			d.Set(x, y, s.At(T-1-x, y))
		}
	}
	return d
}

func flipV(s *image.RGBA) *image.RGBA {
	d := image.NewRGBA(s.Bounds())
	for y := 0; y < T; y++ {
		for x := 0; x < T; x++ {
			d.Set(x, y, s.At(x, T-1-y))
		}
	}
	return d
}

func blit(dst, s *image.RGBA, cx, cy int) {
	draw.Draw(dst, image.Rect(cx*T, cy*T, cx*T+T, cy*T+T), s, image.Pt(0, 0), draw.Src)
}

func at8(im *image.RGBA, x, y int) (float64, float64, float64) {
	r, g, b, _ := im.At(x, y).RGBA()
	return float64(r >> 8), float64(g >> 8), float64(b >> 8)
}
