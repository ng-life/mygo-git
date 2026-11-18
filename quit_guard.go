package main

// A multi-repository operation can be between Git subprocesses. Guard the whole
// job, not just child PIDs, so closing/restarting cannot strand an index lock.
func (a *client) canQuitSafely() bool {
	if !a.busy {
		return true
	}
	a.quitPending = true
	a.output = "Waiting for the current Git operation to finish before quitting…"
	if a.win != nil {
		a.win.Update(func() {})
	}
	return false
}
