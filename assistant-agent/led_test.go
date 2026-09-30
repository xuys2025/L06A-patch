package main

import (
	"errors"
	"sync"
	"testing"
)

func TestRGBMarqueeContinuousAcrossWholeRotation(t *testing.T) {
	if buildRGBMarqueeFrame(0) != buildRGBMarqueeFrame(marqueePeriod) {
		t.Fatal("rotation has a seam")
	}
	previous := buildRGBMarqueeFrame(0)
	for elapsed := marqueeInterval; elapsed <= 2*marqueePeriod; elapsed += marqueeInterval {
		next := buildRGBMarqueeFrame(elapsed)
		for pixel, color := range next {
			if color[0]+color[1]+color[2] < 200 {
				t.Fatalf("dark flash at %s pixel %d", elapsed, pixel)
			}
			for channel, value := range color {
				delta := int(value) - int(previous[pixel][channel])
				if delta < 0 {
					delta = -delta
				}
				if value > 255 || delta > 11 {
					t.Fatalf("abrupt step at %s pixel %d channel %d: %d", elapsed, pixel, channel, delta)
				}
			}
		}
		previous = next
	}
}

type recordingRing struct {
	mu     sync.Mutex
	frames []rgbFrame
	fail   bool
}

func (r *recordingRing) WriteFrame(frame rgbFrame, _ *rgbFrame) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("LED write failed")
	}
	r.frames = append(r.frames, frame)
	return nil
}
func (r *recordingRing) Close() error { return nil }

func TestListeningThinkingTransitionsDoNotBlankRing(t *testing.T) {
	ring := &recordingRing{}
	effects := 0
	led := NewLEDController(t.Logf)
	led.openRing = func() (rgbOutput, error) { return ring, nil }
	led.effect = func(string, string) error { effects++; return nil }
	for _, state := range []string{"LISTENING", "THINKING", "FOLLOWUP_WAIT"} {
		if err := led.Set(state); err != nil {
			t.Fatal(err)
		}
	}
	if effects != 4 {
		t.Fatalf("custom transitions invoked native effects: %d calls", effects)
	}
	if err := led.Set("IDLE"); err != nil {
		t.Fatal(err)
	}
	ring.mu.Lock()
	defer ring.mu.Unlock()
	if len(ring.frames) < 4 {
		t.Fatal("missing state frames")
	}
	if ring.frames[0] != cyanFrame() {
		t.Fatal("first capture frame is not cyan")
	}
	for i, frame := range ring.frames[:len(ring.frames)-1] {
		if frame == (rgbFrame{}) {
			t.Fatalf("black flash at frame %d", i)
		}
	}
	if ring.frames[len(ring.frames)-1] != (rgbFrame{}) {
		t.Fatal("idle did not clear ring")
	}
}

func TestListeningRejectsFailedFirstFrame(t *testing.T) {
	ring := &recordingRing{fail: true}
	led := NewLEDController(t.Logf)
	led.openRing = func() (rgbOutput, error) { return ring, nil }
	led.effect = func(string, string) error { return nil }
	if err := led.Set("LISTENING"); err == nil {
		t.Fatal("capture must not pass a failed LED")
	}
	if led.state == "LISTENING" {
		t.Fatal("failed LED was cached as ready")
	}
	ring.fail = false
	if err := led.Set("LISTENING"); err != nil {
		t.Fatal(err)
	}
	if err := led.Set("IDLE"); err != nil {
		t.Fatal(err)
	}
}
