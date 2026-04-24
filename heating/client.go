package heating

import (
	"context"
	"fmt"
	"math"
	"time"
)

type Client struct {
	session *Session
}

func NewClient(session *Session) *Client {
	return &Client{session: session}
}

func (c *Client) State() HeaterState {
	return c.session.State()
}

func (c *Client) EnsureOn(ctx context.Context) error {
	c.session.WithTraceWindow(c.session.cfg.TraceWindow)
	state := c.session.State()
	if state.Ready() {
		return nil
	}
	if state.PowerState != PowerOn {
		if err := c.session.sendCommand(WireFrame{
			MessageType: 17,
			MessageCmd:  0,
			Size:        3,
			Data:        []int{SignalHeatingPower, 0, 3},
		}); err != nil {
			return err
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := c.session.waitFor(waitCtx, func(state HeaterState) bool {
		return state.Ready()
	})
	return err
}

func (c *Client) EnsureOff(ctx context.Context) error {
	c.session.WithTraceWindow(c.session.cfg.TraceWindow)
	state := c.session.State()
	if state.PowerState == PowerOff {
		return nil
	}
	if err := c.session.sendCommand(WireFrame{
		MessageType: 17,
		MessageCmd:  0,
		Size:        3,
		Data:        []int{SignalHeatingPower, 0, 5},
	}); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := c.session.waitFor(waitCtx, func(state HeaterState) bool {
		return state.PowerState == PowerOff
	})
	return err
}

func (c *Client) SendSimpleCommand(ctx context.Context, signal int, value int) error {
	_, err := c.SendSimpleCommandAt(ctx, signal, value)
	return err
}

func (c *Client) SendSimpleCommandAt(ctx context.Context, signal int, value int) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	c.session.WithTraceWindow(c.session.cfg.TraceWindow)
	return c.session.sendCommandAt(WireFrame{
		MessageType: 17,
		MessageCmd:  0,
		Size:        3,
		Data:        []int{signal, 0, value},
	})
}

func (c *Client) GetTargetTemp(ctx context.Context) (float64, error) {
	c.session.WithTraceWindow(c.session.cfg.TraceWindow)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, err := c.session.waitFor(waitCtx, func(state HeaterState) bool {
		return state.TargetTempKnown
	})
	if err != nil {
		return 0, err
	}
	return state.TargetTempC, nil
}

func (c *Client) SetTargetTemp(ctx context.Context, target float64) error {
	if math.Mod(target*10, 5) != 0 {
		return fmt.Errorf("target %.2f must be in 0.5C increments", target)
	}
	if err := c.EnsureOn(ctx); err != nil {
		return err
	}
	current, err := c.GetTargetTemp(ctx)
	if err != nil {
		return err
	}
	remaining := int(math.Round((target - current) / 0.5))
	if remaining == 0 {
		return nil
	}
	signal := SignalHeatingTempUp
	delta := 0.5
	if remaining < 0 {
		signal = SignalHeatingTempDown
		delta = -0.5
		remaining = -remaining
	}
	const epsilon = 0.01
	for i := 0; i < remaining; i++ {
		if math.Abs(current-target) < epsilon {
			return nil
		}
		previous := current
		if err := c.pressButton(signal); err != nil {
			return err
		}
		c.session.WithTraceWindow(c.session.cfg.TraceWindow)
		stepCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		state, err := c.session.waitFor(stepCtx, func(state HeaterState) bool {
			if !state.TargetTempKnown {
				return false
			}
			if math.Abs(state.TargetTempC-target) < epsilon {
				return true
			}
			if delta > 0 {
				return state.TargetTempC >= previous+0.49 && state.TargetTempC <= target+epsilon
			}
			return state.TargetTempC <= previous-0.49 && state.TargetTempC >= target-epsilon
		})
		cancel()
		if err != nil {
			stepTarget := previous + delta
			return fmt.Errorf("waiting for target near %.1fC from %.1fC: %w", stepTarget, previous, err)
		}
		current = state.TargetTempC
		if delta > 0 && current > target+epsilon {
			return fmt.Errorf("target overshot to %.1fC while aiming for %.1fC", current, target)
		}
		if delta < 0 && current < target-epsilon {
			return fmt.Errorf("target overshot to %.1fC while aiming for %.1fC", current, target)
		}
	}
	if math.Abs(current-target) < epsilon {
		return nil
	}
	return fmt.Errorf("target ended at %.1fC instead of %.1fC", current, target)
}

func (c *Client) pressButton(signal int) error {
	press := WireFrame{MessageType: 17, MessageCmd: 1, Size: 3, Data: []int{signal, 0, 1}}
	release := WireFrame{MessageType: 17, MessageCmd: 1, Size: 3, Data: []int{signal, 0, 0}}
	if err := c.session.sendCommand(press); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	return c.session.sendCommand(release)
}
