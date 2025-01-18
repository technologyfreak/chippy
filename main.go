package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
)

const (
	DISP_WIDTH  = 64
	DISP_HEIGHT = 32
	DISP_SCALE  = 10
)

type Game struct{}

func (g *Game) Update() error {
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
}

func (g *Game) Layout(outWidth, outHeight int) (int, int) {
	return DISP_WIDTH, DISP_HEIGHT
}

func main() {
	ebiten.SetWindowSize(DISP_WIDTH*DISP_SCALE, DISP_HEIGHT*DISP_SCALE)
	ebiten.SetWindowTitle("Chippy")

	game := &Game{}

	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
