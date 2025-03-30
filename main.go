package main

import (
	"errors"
	"fmt"
	"math/bits"
	"math/rand/v2"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

const (
	DispWidth    = 64
	DispHeight   = 32
	DispScale    = 10
	MaxMem       = 4096
	FontSetSize  = 80
	MaxRegs      = 16
	MaxStack     = 48
	DefaultStart = 512
)

var (
	DispOps ebiten.DrawImageOptions
)

// error
var (
	ErrStackIsFull         = errors.New("stack is full")
	ErrStackIsEmpty        = errors.New("stack is empty")
	ErrNotValidInstruction = errors.New("not a valid instruction")
)

type Game struct {
	mem    []byte
	keymap map[ebiten.Key]byte

	v  []byte // registers
	i  uint16 // address register
	pc uint16 // pointer counter
	sp uint16 // stack pointer

	stack []uint16

	dt byte // delay timer
	st byte // sound timer

	pixels []byte
	disp   *ebiten.Image

	isKeyPressed bool
	pressed      byte
}

func (g *Game) load(program string, start uint16) error {
	data, err := os.ReadFile(program)
	if err != nil {
		return err
	}
	g.pc = start
	for _, b := range data {
		g.mem[start] = b
		start++
	}
	return nil
}

func (g *Game) fetch() uint16 {
	opcode := (uint16(g.mem[g.pc]) << 8) | uint16(g.mem[g.pc+1])
	g.pc += 2
	return opcode
}

// read from memory starting at address in i, store in v[0] through v[x]
func (g *Game) op_fx65(x byte) error {
	for i := 0; i <= int(x); i++ {
		g.v[i] = g.mem[g.i+uint16(i)]
	}
	return nil
}

// read v[0] through v[x], store in memory starting at address in i
func (g *Game) op_fx55(x byte) error {
	i := g.i
	for j := 0; j <= int(x); j++ {
		g.mem[i] = g.v[j]
		i++
	}
	return nil
}

// store BCD rep of v[x] in i, i+1, and i+2
func (g *Game) op_fx33(x byte) error {
	g.mem[g.i] = bcd(g.v[x], 100)
	g.mem[g.i+1] = bcd(g.v[x], 10)
	g.mem[g.i+2] = bcd(g.v[x], 1)
	return nil
}

// set i to v[x]
func (g *Game) op_fx29(x byte) error {
	g.i = uint16(g.v[x])
	return nil
}

// add v[x] to i, store in i
func (g *Game) op_fx1e(x byte) error {
	g.i += uint16(g.v[x])
	return nil
}

// set sound timer to v[x]
func (g *Game) op_fx18(x byte) error {
	g.st = g.v[x]
	return nil
}

// set delay timer to v[x]
func (g *Game) op_fx15(x byte) error {
	g.dt = g.v[x]
	return nil
}

// wait for key press, store in v[x]
func (g *Game) op_fx0a(x byte) error {
	g.getKey()
	if !g.isKeyPressed {
		g.pc -= 2
	} else {
		g.v[x] = g.pressed
	}
	return nil
}

// set v[x] to delay timer
func (g *Game) op_fx07(x byte) error {
	g.v[x] = g.dt
	return nil
}

// skip next instruction if key pressed != v[x]
func (g *Game) op_exa1(x byte) error {
	g.getKey()
	if !g.isKeyPressed || (g.isKeyPressed && g.pressed != g.v[x]) {
		g.pc += 2
	}
	return nil
}

// skip next instruction if key pressed == v[x]
func (g *Game) op_ex9e(x byte) error {
	g.getKey()
	if g.isKeyPressed && g.pressed == g.v[x] {
		g.pc += 2
	}
	return nil
}

// write to the display
func (g *Game) op_dxyn(x, y, n byte) error {
	g.v[0xf] = 0
	for yOffset := 0; yOffset < int(n); yOffset++ {
		newY := (int(g.v[y]) + yOffset) % DispHeight
		data := g.mem[g.i+uint16(yOffset)]
		newY *= DispWidth * 4
		for xOffset := 0; xOffset < 8; xOffset++ {
			if data&(0x80>>xOffset) != 0 {
				newX := (int(g.v[x]) + xOffset) % DispWidth
				coord := newY + newX*4
				if g.pixels[coord] == 0xFF {
					g.v[0xf] = 1
				}
				g.pixels[coord] ^= 0xFF
				g.pixels[coord+1] ^= 0xFF
				g.pixels[coord+2] ^= 0xFF
				g.pixels[coord+3] ^= 0xFF
			}
		}
	}
	g.disp.WritePixels(g.pixels[:])
	return nil
}

// set v[x] to random byte bitwise anded with kk
func (g *Game) op_cxkk(x, kk byte) error {
	g.v[x] = byte(rand.UintN(255)) & kk
	return nil
}

// jump to address nnn + v[0]
func (g *Game) op_bnnn(nnn uint16) error {
	g.pc = nnn + uint16(g.v[0])
	return nil
}

// set i = nnn
func (g *Game) op_annn(nnn uint16) error {
	g.i = nnn
	return nil
}

// skip next instruction if v[x] != v[y]
func (g *Game) op_9xy0(x, y byte) error {
	if g.v[x] != g.v[y] {
		g.pc += 2
	}
	return nil
}

// shift left v[x] by 1, store in v[x], store least-significant bit in v[f]
func (g *Game) op_8xye(x byte) error {
	var most byte
	if bits.OnesCount8(g.v[x]) > bits.OnesCount8(g.v[x]<<1) {
		most = 1
	}
	g.v[x] <<= 1
	g.v[0xf] = most
	return nil
}

// subtract v[x] from v[y], store in v[x], store inverse of borrow in v[f]
func (g *Game) op_8xy7(x, y byte) error {
	diff, borrow := bits.Sub(uint(g.v[y]), uint(g.v[x]), 0)
	g.v[x] = byte(diff)
	g.v[0xf] = 1 - byte(borrow)
	return nil
}

// shift right v[x] by 1, store in v[x], store least-significant bit in v[f]
func (g *Game) op_8xy6(x byte) error {
	var least byte
	if bits.OnesCount8(g.v[x]) > bits.OnesCount8(g.v[x]>>1) {
		least = 1
	}
	g.v[x] >>= 1
	g.v[0xf] = least
	return nil
}

// subtract v[y] from v[x], store in v[x], store inverse of borrow in v[f]
func (g *Game) op_8xy5(x, y byte) error {
	diff, borrow := bits.Sub(uint(g.v[x]), uint(g.v[y]), 0)
	g.v[x] = byte(diff)
	g.v[0xf] = 1 - byte(borrow)
	return nil
}

// add v[y] to v[x], store in v[x], store carry in v[f]
func (g *Game) op_8xy4(x, y byte) error {
	sum := uint16(g.v[x]) + uint16(g.v[y])
	g.v[x] += g.v[y]
	g.v[0xf] = byte((sum >> 8) & 1)
	return nil
}

// xor v[x] and v[y], store in v[x]
func (g *Game) op_8xy3(x, y byte) error {
	g.v[x] ^= g.v[y]
	return nil
}

// bitwise and v[x] and v[y], store in v[x]
func (g *Game) op_8xy2(x, y byte) error {
	g.v[x] &= g.v[y]
	return nil
}

// bitwise or v[x] and v[y], store in v[x]
func (g *Game) op_8xy1(x, y byte) error {
	g.v[x] |= g.v[y]
	return nil
}

// set v[x] to v[y]
func (g *Game) op_8xy0(x, y byte) error {
	g.v[x] = g.v[y]
	return nil
}

// add kk to v[x], store in v[x]
func (g *Game) op_7xkk(x, kk byte) error {
	g.v[x] += kk
	return nil
}

// set v[x] to kk
func (g *Game) op_6xkk(x, kk byte) error {
	g.v[x] = kk
	return nil
}

// skip next instruction if v[x] == v[y]
func (g *Game) op_5xy0(x, y byte) error {
	if g.v[x] == g.v[y] {
		g.pc += 2
	}
	return nil
}

// skip next instruction if v[x] != kk
func (g *Game) op_4xkk(x, kk byte) error {
	if g.v[x] != kk {
		g.pc += 2
	}
	return nil
}

// skip next instruction if v[x] == kk
func (g *Game) op_3xkk(x, kk byte) error {
	if g.v[x] == kk {
		g.pc += 2
	}
	return nil
}

// push address nnn onto stack
func (g *Game) op_2nnn(nnn uint16) error {
	if g.sp >= MaxStack {
		return ErrStackIsFull
	}

	g.stack[g.sp] = g.pc
	g.sp++
	g.pc = nnn
	return nil
}

// jump to address nnn
func (g *Game) op_1nnn(nnn uint16) error {
	g.pc = nnn
	return nil
}

// pop address from stack
func (g *Game) op_00ee() error {
	if g.sp == 0 {
		return ErrStackIsEmpty
	}

	g.pc = g.stack[g.sp-1]
	g.sp--
	return nil
}

// clear display
func (g *Game) op_00e0() error {
	g.pixels = make([]byte, DispWidth*DispHeight*4)
	g.disp.WritePixels(g.pixels[:])
	return nil
}

func (g *Game) getKey() error {
	g.isKeyPressed = false
	keys := make([]ebiten.Key, 0)
	keys = inpututil.AppendPressedKeys(keys)
	for _, key := range keys {
		if pressed, valid := g.keymap[key]; valid {
			g.pressed = pressed
			g.isKeyPressed = true
			return nil
		}
	}
	return nil
}

func (g *Game) cycles(perFrame int) error {
	for range perFrame {
		opcode := g.fetch()
		nnn := opcode & 0xfff
		x := byte(opcode >> 8 & 0xf)
		y := byte(opcode >> 4 & 0xf)
		n := byte(opcode & 0xf)
		kk := byte(opcode & 0xff)
		var err error

		ofx65 := opcode | 0xf065
		ofx55 := opcode | 0xf055
		ofx33 := opcode | 0xf033
		ofx29 := opcode | 0xf029
		ofx1e := opcode | 0xf01e
		ofx18 := opcode | 0xf018
		ofx15 := opcode | 0xf015
		ofx0a := opcode | 0xf00a
		ofx07 := opcode | 0xf007
		oexa1 := opcode | 0xe0a1
		oex9e := opcode | 0xe09e
		odxyn := opcode | 0xd000
		ocxkk := opcode | 0xc000
		obnnn := opcode | 0xb000
		oannn := opcode | 0xa000
		o9xy0 := opcode | 0x9000
		o8xye := opcode | 0x800e
		o8xy7 := opcode | 0x8007
		o8xy6 := opcode | 0x8006
		o8xy5 := opcode | 0x8005
		o8xy4 := opcode | 0x8004
		o8xy3 := opcode | 0x8003
		o8xy2 := opcode | 0x8002
		o8xy1 := opcode | 0x8001
		o8xy0 := opcode | 0x8000
		o7xkk := opcode | 0x7000
		o6xkk := opcode | 0x6000
		o5xy0 := opcode | 0x5000
		o4xkk := opcode | 0x4000
		o3xkk := opcode | 0x3000
		o2nnn := opcode | 0x2000
		o1nnn := opcode | 0x1000
		o00ee := opcode | 0x00ee
		o00e0 := opcode | 0x00e0

		switch opcode {
		case ofx65:
			err = g.op_fx65(x)
		case ofx55:
			err = g.op_fx55(x)
		case ofx33:
			err = g.op_fx33(x)
		case ofx29:
			err = g.op_fx29(x)
		case ofx1e:
			err = g.op_fx1e(x)
		case ofx18:
			err = g.op_fx18(x)
		case ofx15:
			err = g.op_fx15(x)
		case ofx0a:
			err = g.op_fx0a(x)
		case ofx07:
			err = g.op_fx07(x)
		case oexa1:
			err = g.op_exa1(x)
		case oex9e:
			err = g.op_ex9e(x)
		case odxyn:
			err = g.op_dxyn(x, y, n)
		case ocxkk:
			err = g.op_cxkk(x, kk)
		case obnnn:
			err = g.op_bnnn(nnn)
		case oannn:
			err = g.op_annn(nnn)
		case o9xy0:
			err = g.op_9xy0(x, y)
		case o8xye:
			err = g.op_8xye(x)
		case o8xy7:
			err = g.op_8xy7(x, y)
		case o8xy6:
			err = g.op_8xy6(x)
		case o8xy5:
			err = g.op_8xy5(x, y)
		case o8xy4:
			err = g.op_8xy4(x, y)
		case o8xy3:
			err = g.op_8xy3(x, y)
		case o8xy2:
			err = g.op_8xy2(x, y)
		case o8xy1:
			err = g.op_8xy1(x, y)
		case o8xy0:
			err = g.op_8xy0(x, y)
		case o7xkk:
			err = g.op_7xkk(x, kk)
		case o6xkk:
			err = g.op_6xkk(x, kk)
		case o5xy0:
			err = g.op_5xy0(x, y)
		case o4xkk:
			err = g.op_4xkk(x, kk)
		case o3xkk:
			err = g.op_3xkk(x, kk)
		case o2nnn:
			err = g.op_2nnn(nnn)
		case o1nnn:
			err = g.op_1nnn(nnn)
		case o00ee:
			err = g.op_00ee()
		case o00e0:
			err = g.op_00e0()
		default:
			err = ErrNotValidInstruction
		}

		if err != nil {
			return err
		}
	}
	return nil
}

func (g *Game) Update() error {
	err := g.cycles(20)
	if g.dt > 0 {
		g.dt--
	} else {
		g.dt = 60
	}

	if g.st > 0 {
		g.st--
	} else {
		g.st = 60
	}

	return err
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Clear()
	screen.DrawImage(g.disp, &DispOps)
}

func (g *Game) Layout(outWidth, outHeight int) (int, int) {
	return DispWidth, DispHeight
}

func NewGame(program string, start ...uint16) *Game {
	g := &Game{
		disp:   ebiten.NewImage(DispWidth, DispHeight),
		dt:     60,
		st:     60,
		mem:    make([]byte, MaxMem),
		v:      make([]byte, MaxRegs),
		stack:  make([]uint16, MaxStack),
		pixels: make([]byte, DispWidth*DispHeight*4),
	}

	mapFontset(&g.mem)
	mapKeys(&g.keymap)

	if len(start) == 0 {
		g.load(program, DefaultStart)
	} else if len(start) == 1 {
		g.load(program, start[0])
	} else {
		fmt.Fprintf(os.Stderr, "usage: <program> [start]\n")
		os.Exit(1)
	}

	return g
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: <program> [start]\n")
		os.Exit(1)
	}

	game := NewGame(os.Args[1])

	ebiten.SetWindowSize(DispWidth*DispScale, DispHeight*DispScale)
	ebiten.SetWindowTitle("Chippy")
	ebiten.SetScreenClearedEveryFrame(false)

	if err := ebiten.RunGame(game); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err.Error())
		os.Exit(1)
	}
}

func mapFontset(mem *[]byte) {
	fontset := []byte{
		0xf0, 0x90, 0x90, 0x90, 0xf0, // 0
		0x20, 0x60, 0x20, 0x20, 0x70, // 1
		0xf0, 0x10, 0xf0, 0x80, 0xf0, // 2
		0xf0, 0x10, 0xf0, 0x10, 0xf0, // 3
		0x90, 0x90, 0xf0, 0x10, 0x10, // 4
		0xf0, 0x80, 0xf0, 0x10, 0xf0, // 5
		0xf0, 0x80, 0xf0, 0x90, 0xf0, // 6
		0xf0, 0x10, 0x20, 0x40, 0x40, // 7
		0xf0, 0x90, 0xf0, 0x90, 0xf0, // 8
		0xf0, 0x90, 0xf0, 0x10, 0xf0, // 9
		0xf0, 0x90, 0xf0, 0x90, 0x90, // A
		0xE0, 0x90, 0xE0, 0x90, 0xE0, // B
		0xf0, 0x80, 0x80, 0x80, 0xf0, // C
		0xE0, 0x90, 0x90, 0x90, 0xE0, // D
		0xf0, 0x80, 0xf0, 0x80, 0xf0, // E
		0xf0, 0x80, 0xf0, 0x80, 0x80, // F
	}
	copy(*mem, fontset)
}

func mapKeys(keymap *map[ebiten.Key]byte) {
	(*keymap) = map[ebiten.Key]byte{
		ebiten.Key1: 0x1,
		ebiten.Key2: 0x2,
		ebiten.Key3: 0x3,
		ebiten.Key4: 0xC,
		ebiten.KeyQ: 0x4,
		ebiten.KeyW: 0x5,
		ebiten.KeyE: 0x6,
		ebiten.KeyR: 0xD,
		ebiten.KeyA: 0x7,
		ebiten.KeyS: 0x8,
		ebiten.KeyD: 0x9,
		ebiten.KeyF: 0xE,
		ebiten.KeyZ: 0xA,
		ebiten.KeyX: 0x0,
		ebiten.KeyC: 0xB,
		ebiten.KeyV: 0xF,
	}
}

func bcd(num, place byte) byte {
	return ((num % (place * 10)) - (num % place)) / place
}
