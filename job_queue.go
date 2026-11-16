package ochazuke

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// JobsBackgroundMaxSetting bounds the background jobs running at once:
// the jobs launched with a RequestFromBackground Ctx (a page's refresh, a
// list's store read, the Nouns page's counts, a detail's related
// sections), which nobody is waiting on. Past it a new one waits in launch
// order, running (job.list says queued) but with its work not started,
// until one ends. A job someone asked for (a key, the palette, the CLI, an
// agent) never waits and takes no slot. 0 (or less) bounds nothing.
const JobsBackgroundMaxSetting = "jobs.background-max"

// startJob queues a launched job's work for the adapter, or, for a
// background job past the bound, keeps it until a slot frees.
func (r *Runtime) startJob(j *job, start tea.Cmd) {
	if j.background && !r.backgroundSlotFree() {
		j.Queued, j.start = true, start
		r.jobQueue = append(r.jobQueue, j.Job)
		return
	}
	r.commands = append(r.commands, start)
}

// backgroundSlotFree reports whether a background job may start now.
func (r *Runtime) backgroundSlotFree() bool {
	limit := r.Settings().Int(JobsBackgroundMaxSetting)
	if limit <= 0 {
		return true
	}
	held := 0
	for _, j := range r.jobs {
		if j.background && j.Status == JobRunning && !j.Queued {
			held++
		}
	}
	return held < limit
}

// releaseSlot runs when a job ends (finish): a queued one leaves the queue
// and never starts; one that held a slot hands it to the next waiting.
func (r *Runtime) releaseSlot(j *job) {
	if !j.background {
		return
	}
	if j.Queued {
		j.Queued, j.start = false, nil
		r.jobQueue = slices.DeleteFunc(r.jobQueue, func(id uint64) bool { return id == j.Job })
		return
	}
	r.startQueued()
}

// startQueued starts the waiting background jobs, oldest first, while
// slots are free.
func (r *Runtime) startQueued() {
	for len(r.jobQueue) > 0 && r.backgroundSlotFree() {
		j := r.jobs[r.jobQueue[0]]
		r.jobQueue = r.jobQueue[1:]
		if j == nil || j.Status != JobRunning || !j.Queued {
			continue
		}
		start := j.start
		j.Queued, j.start = false, nil
		r.commands = append(r.commands, start)
	}
}
