//go:build linux

package main

import (
	"encoding/binary"
	"testing"
)

func joystickEvent(kind, number byte, value int16) [8]byte {
	var event [8]byte
	binary.LittleEndian.PutUint16(event[4:6], uint16(value))
	event[6], event[7] = kind, number
	return event
}

func TestGamepadDpadAndA(t *testing.T) {
	var decoder gamepadDecoder
	for _, step := range []struct {
		event [8]byte
		want  padAction
	}{
		{joystickEvent(0x81, 0, 1), padNone},
		{joystickEvent(1, 0, 0), padNone},
		{joystickEvent(1, 0, 1), padSelect},
		{joystickEvent(1, 0, 1), padNone},
		{joystickEvent(2, 6, -32767), padLeft},
		{joystickEvent(2, 6, -32767), padNone},
		{joystickEvent(2, 6, 0), padNone},
		{joystickEvent(2, 6, 32767), padRight},
		{joystickEvent(2, 7, -32767), padUp},
		{joystickEvent(2, 7, 32767), padDown},
	} {
		if got := decoder.decode(step.event); got != step.want {
			t.Fatalf("event %v: got %v, want %v", step.event, got, step.want)
		}
	}
}
