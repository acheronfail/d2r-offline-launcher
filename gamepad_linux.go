//go:build linux

package main

import (
	"encoding/binary"
	"errors"
	"math"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/sys/unix"
)

type padAction uint8

const (
	padNone padAction = iota
	padUp
	padRight
	padDown
	padLeft
	padSelect
)

type gamepadDecoder struct {
	axis  [2]int8
	aDown bool
}

func (d *gamepadDecoder) decode(event [8]byte) padAction {
	value := int16(binary.LittleEndian.Uint16(event[4:6]))
	initial := event[6]&0x80 != 0
	switch event[6] & 0x7f {
	case 1:
		if event[7] != 0 {
			return padNone
		}
		pressed := value != 0
		newPress := pressed && !d.aDown && !initial
		d.aDown = pressed
		if newPress {
			return padSelect
		}
	case 2:
		if event[7] < 6 || event[7] > 7 {
			return padNone
		}
		axis := event[7] - 6
		direction := int8(0)
		if value < -16000 {
			direction = -1
		} else if value > 16000 {
			direction = 1
		}
		if direction == d.axis[axis] {
			return padNone
		}
		d.axis[axis] = direction
		if direction == 0 || initial {
			return padNone
		}
		if axis == 0 {
			if direction < 0 {
				return padLeft
			}
			return padRight
		}
		if direction < 0 {
			return padUp
		}
		return padDown
	}
	return padNone
}

type gamepadTarget struct {
	object   fyne.CanvasObject
	activate func()
}

type gamepadNavigation struct {
	window  fyne.Window
	targets []gamepadTarget
	launch  *widget.Button
	ring    *canvas.Rectangle
	layer   fyne.CanvasObject
}

func (n *gamepadNavigation) available(index int) bool {
	object := n.targets[index].object
	if !object.Visible() {
		return false
	}
	if disabled, ok := object.(fyne.Disableable); ok && disabled.Disabled() {
		return false
	}
	return true
}

func (n *gamepadNavigation) focus(index int) {
	if !n.available(index) {
		return
	}
	object := n.targets[index].object
	n.window.Canvas().Focus(object.(fyne.Focusable))
	position := fyne.CurrentApp().Driver().AbsolutePositionForObject(object)
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(n.layer)
	size := object.Size()
	n.ring.Move(fyne.NewPos(position.X-origin.X-2, position.Y-origin.Y-2))
	n.ring.Resize(fyne.NewSize(size.Width+4, size.Height+4))
	n.ring.Show()
	n.ring.Refresh()
}

func (n *gamepadNavigation) clearRing() {
	n.ring.Hide()
}

func (n *gamepadNavigation) focusDefault() {
	if n.window.Canvas().Focused() != nil {
		return
	}
	button := n.launch
	if button.Disabled() {
		return
	}
	for index, target := range n.targets {
		if target.object == button {
			n.focus(index)
			return
		}
	}
}

func (n *gamepadNavigation) current() int {
	focused := n.window.Canvas().Focused()
	for index, target := range n.targets {
		if target.object.(fyne.Focusable) == focused {
			return index
		}
	}
	return -1
}

func (n *gamepadNavigation) center(index int) fyne.Position {
	object := n.targets[index].object
	position := fyne.CurrentApp().Driver().AbsolutePositionForObject(object)
	size := object.Size()
	return fyne.NewPos(position.X+size.Width/2, position.Y+size.Height/2)
}

func (n *gamepadNavigation) handle(action padAction) {
	canvas := n.window.Canvas()
	if canvas.Overlays().Top() != nil {
		n.clearRing()
		if action == padSelect {
			if target, ok := canvas.Focused().(fyne.Tappable); ok {
				target.Tapped(nil)
			} else {
				canvas.FocusNext()
			}
		} else if action == padUp || action == padLeft {
			canvas.FocusPrevious()
		} else {
			canvas.FocusNext()
		}
		return
	}
	current := n.current()
	if current < 0 {
		n.focusDefault()
		return
	}
	if action == padSelect {
		target := n.targets[current]
		if n.available(current) {
			n.clearRing()
			if target.activate != nil {
				target.activate()
			} else {
				target.object.(fyne.Tappable).Tapped(nil)
			}
		}
		return
	}
	from := n.center(current)
	best, bestScore := -1, float32(math.MaxFloat32)
	for index := range n.targets {
		if index == current || !n.available(index) {
			continue
		}
		to := n.center(index)
		dx, dy := to.X-from.X, to.Y-from.Y
		var primary, cross float32
		switch action {
		case padUp:
			primary, cross = -dy, float32(math.Abs(float64(dx)))
		case padDown:
			primary, cross = dy, float32(math.Abs(float64(dx)))
		case padLeft:
			primary, cross = -dx, float32(math.Abs(float64(dy)))
		case padRight:
			primary, cross = dx, float32(math.Abs(float64(dy)))
		}
		if primary <= 1 {
			continue
		}
		if (action == padLeft || action == padRight) && cross > (n.targets[current].object.Size().Height+n.targets[index].object.Size().Height)/2 {
			continue
		}
		if score := primary + 2*cross; score < bestScore {
			best, bestScore = index, score
		}
	}
	if best >= 0 {
		n.focus(best)
	}
}

func waitForGamepad(done <-chan struct{}, duration time.Duration) bool {
	select {
	case <-done:
		return false
	case <-time.After(duration):
		return true
	}
}

func startGamepad(window fyne.Window, app fyne.App, targets []gamepadTarget, launch *widget.Button, ring *canvas.Rectangle, layer fyne.CanvasObject) {
	var active atomic.Bool
	navigation := gamepadNavigation{window: window, targets: targets, launch: launch, ring: ring, layer: layer}
	app.Lifecycle().SetOnEnteredForeground(func() { active.Store(true) })
	app.Lifecycle().SetOnExitedForeground(func() {
		active.Store(false)
		fyne.Do(navigation.clearRing)
	})
	done := make(chan struct{})
	var closeOnce sync.Once
	window.SetOnClosed(func() { closeOnce.Do(func() { close(done) }) })
	go func() {
		fd := -1
		defer func() {
			if fd >= 0 {
				unix.Close(fd)
			}
		}()
		var decoder gamepadDecoder
		var event [8]byte
		lastFocusCheck := time.Now()
		for {
			select {
			case <-done:
				return
			default:
			}
			if !active.Load() {
				if fd >= 0 {
					unix.Close(fd)
					fd = -1
				}
				if !waitForGamepad(done, 100*time.Millisecond) {
					return
				}
				continue
			}
			if fd < 0 {
				paths, _ := filepath.Glob("/dev/input/js[0-9]*")
				for _, path := range paths {
					opened, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK, 0)
					if err == nil {
						fd = opened
						decoder = gamepadDecoder{}
						fyne.Do(func() {
							if active.Load() {
								navigation.focusDefault()
							}
						})
						break
					}
				}
				if fd < 0 {
					if !waitForGamepad(done, time.Second) {
						return
					}
					continue
				}
			}
			n, err := unix.Read(fd, event[:])
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				if time.Since(lastFocusCheck) > 250*time.Millisecond {
					fyne.Do(func() {
						if !active.Load() || navigation.current() < 0 {
							navigation.clearRing()
						}
					})
					lastFocusCheck = time.Now()
				}
				if !waitForGamepad(done, 20*time.Millisecond) {
					return
				}
				continue
			}
			if err != nil || n != len(event) {
				unix.Close(fd)
				fd = -1
				continue
			}
			if action := decoder.decode(event); action != padNone {
				fyne.Do(func() {
					if active.Load() {
						navigation.handle(action)
					}
				})
			}
		}
	}()
}
