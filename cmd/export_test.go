package cmd

import (
	"context"
	"time"
)

// SetDeviceSleep replaces the wait between device-login polls and returns a
// function that restores the previous one. Tests use it so a login flow runs
// at full speed instead of honouring the server's poll interval.
func SetDeviceSleep(f func(context.Context, time.Duration) error) func() {
	previous := deviceSleep
	deviceSleep = f
	return func() { deviceSleep = previous }
}
