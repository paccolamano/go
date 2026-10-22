package lifecycle

import (
	"os"
)

// signalBuffer is the capacity of the signal channel: the trigger and the signal
// that forces the stop.
const signalBuffer = 2

// listen registers the configured signals and returns the channel they arrive
// on, or nil when the App handles none. The caller must call unlisten with it.
func (r *runner) listen() chan os.Signal {
	if len(r.a.cfg.signals) == 0 {
		return nil
	}
	ch := make(chan os.Signal, signalBuffer)
	r.a.notify(ch, r.a.cfg.signals...)
	return ch
}

// unlisten undoes listen. signal.Stop only removes this channel, so the
// process's previous handling of the signals applies again.
func (r *runner) unlisten(ch chan os.Signal) {
	if ch != nil {
		r.a.stopNotify(ch)
	}
}
