package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"
)

const (
	l06aLEDPath     = "/sys/devices/i2c-0/0-003a/led_rgb"
	marqueeInterval = 25 * time.Millisecond
	marqueePeriod   = 3600 * time.Millisecond
)

type rgbColor [3]uint32
type rgbFrame [18]rgbColor

type rgbOutput interface {
	WriteFrame(rgbFrame, *rgbFrame) error
	Close() error
}

type LEDController struct {
	mu       sync.Mutex
	state    string
	cancel   context.CancelFunc
	stopped  chan struct{}
	logf     func(string, ...any)
	openRing func() (rgbOutput, error)
	effect   func(action, effect string) error
}

func NewLEDController(logf func(string, ...any)) *LEDController {
	return &LEDController{logf: logf, openRing: openRGBRing, effect: nativeLEDEffect}
}

func customLEDState(state string) bool { return state == "LISTENING" || state == "THINKING" }

func (c *LEDController) Set(state string) error {
	if state == "FOLLOWUP_WAIT" {
		state = "LISTENING"
	} else if state == "RESPONDING" {
		state = "SPEAKING"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if state == c.state {
		if c.stopped == nil {
			return nil
		}
		select {
		case <-c.stopped: /* retry a failed output */
		default:
			return nil
		}
	}
	previousState := c.state
	if c.cancel != nil {
		c.cancel()
		<-c.stopped
		c.cancel, c.stopped = nil, nil
	}
	var err error
	switch state {
	case "LISTENING", "THINKING":
		// Both states own the same ring. Do not blank it or invoke native effects
		// between them: shutdown/startup clearing caused a visible black flash.
		if !customLEDState(previousState) {
			err = c.shut("1", "2", "3", "11")
		}
		if err == nil {
			err = c.startCustom(state)
		}
	case "SPEAKING":
		err = c.shut("1", "2", "11")
		if customLEDState(previousState) {
			err = errors.Join(err, c.clearRing())
		}
		err = errors.Join(err, c.effect("show", "3"))
	case "IDLE", "MUSIC":
		err = c.shut("1", "2", "3", "11")
		if customLEDState(previousState) {
			err = errors.Join(err, c.clearRing())
		}
	}
	if err != nil {
		c.state = ""
		return err
	}
	c.state = state
	return nil
}

func nativeLEDEffect(action, effect string) error {
	_, err := runCommand("/bin/ubus", "-t", "1", "call", "led", action, fmt.Sprintf(`{"L":%s}`, effect))
	if err != nil {
		return fmt.Errorf("%s LED effect %s: %w", action, effect, err)
	}
	return nil
}

func (c *LEDController) shut(effects ...string) error {
	var result error
	for _, effect := range effects {
		result = errors.Join(result, c.effect("shut", effect))
	}
	return result
}

func (c *LEDController) clearRing() error {
	ring, err := c.openRing()
	if err != nil {
		return err
	}
	return errors.Join(ring.WriteFrame(rgbFrame{}, nil), ring.Close())
}

func cyanFrame() (frame rgbFrame) {
	for pixel := range frame {
		frame[pixel] = rgbColor{0, 190, 230}
	}
	return frame
}

func (c *LEDController) startCustom(state string) error {
	ring, err := c.openRing()
	if err != nil {
		return fmt.Errorf("%s LED unavailable: %w", state, err)
	}
	first, interval := cyanFrame(), time.Second
	if state == "THINKING" {
		first, interval = buildRGBMarqueeFrame(0), marqueeInterval
	}
	// Microphone capture remains gated on this synchronous first frame.
	if err := ring.WriteFrame(first, nil); err != nil {
		_ = ring.Close()
		return fmt.Errorf("show first %s LED frame: %w", state, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel, c.stopped = cancel, make(chan struct{})
	go c.runCustom(ctx, c.stopped, ring, state, first, interval)
	return nil
}

func (c *LEDController) runCustom(ctx context.Context, stopped chan<- struct{}, ring rgbOutput, state string, previous rgbFrame, interval time.Duration) {
	defer close(stopped)
	defer ring.Close()
	start := time.Now()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// The next state writes its own frame; retain the last lit frame.
			return
		case <-ticker.C:
			next, compare := cyanFrame(), (*rgbFrame)(nil)
			if state == "THINKING" {
				next, compare = buildRGBMarqueeFrame(time.Since(start)), &previous
			}
			if err := ring.WriteFrame(next, compare); err != nil {
				c.logf("%s LED frame failed: %v", state, err)
				return
			}
			previous = next
		}
	}
}

func buildRGBMarqueeFrame(elapsed time.Duration) rgbFrame {
	colors := [...]rgbColor{{255, 20, 0}, {255, 150, 0}, {30, 230, 20}, {0, 210, 255}, {30, 60, 255}, {210, 0, 255}}
	// Wall clock phase avoids accumulated lag if an occasional frame is late.
	rotation := float64(elapsed%marqueePeriod) / float64(marqueePeriod) * float64(len(colors))
	var frame rgbFrame
	for pixel := range frame {
		phase := math.Mod(float64(pixel)/3-rotation+float64(len(colors)), float64(len(colors)))
		index, fraction := int(phase), phase-math.Floor(phase)
		for channel := range frame[pixel] {
			a, b := float64(colors[index][channel]), float64(colors[(index+1)%len(colors)][channel])
			frame[pixel][channel] = uint32(math.Round(a + (b-a)*fraction))
		}
	}
	return frame
}

type sysfsRGBRing struct{ file *os.File }

func openRGBRing() (rgbOutput, error) {
	file, err := os.OpenFile(l06aLEDPath, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	return &sysfsRGBRing{file}, nil
}

func (r *sysfsRGBRing) WriteFrame(frame rgbFrame, previous *rgbFrame) error {
	// Reuse one fd. The driver's store callback accepts one pixel per write.
	for pixel, color := range frame {
		if previous != nil && (*previous)[pixel] == color {
			continue
		}
		value := color[0] | color[1]<<8 | color[2]<<16
		if _, err := fmt.Fprintf(r.file, "%d %d\n", pixel, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *sysfsRGBRing) Close() error { return r.file.Close() }
