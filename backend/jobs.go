package main

// Jobs run docker compose commands and stream their output to the browser.

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Job struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	State   string   `json:"state"` // running | ok | failed
	Lines   []string `json:"lines"`
	Started int64    `json:"started"`
	Ended   int64    `json:"ended,omitempty"`
}

type Jobs struct {
	hub *Hub
	mu  sync.Mutex
	all []*Job // newest last, capped
	// one compose operation per project at a time
	locks map[string]*sync.Mutex
}

func NewJobs(hub *Hub) *Jobs { return &Jobs{hub: hub, locks: map[string]*sync.Mutex{}} }

func (j *Jobs) List() []Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Job, len(j.all))
	for i, x := range j.all {
		out[i] = *x
		out[i].Lines = append([]string(nil), x.Lines...)
	}
	return out
}

func (j *Jobs) lock(key string) *sync.Mutex {
	j.mu.Lock()
	defer j.mu.Unlock()
	m, ok := j.locks[key]
	if !ok {
		m = &sync.Mutex{}
		j.locks[key] = m
	}
	return m
}

type step struct {
	dir  string
	env  []string
	args []string // full argv, e.g. docker compose ...
}

// Start runs steps sequentially in the background, stopping at the first failure.
func (j *Jobs) Start(lockKey, title string, steps []step) *Job {
	b := make([]byte, 6)
	rand.Read(b)
	job := &Job{ID: hex.EncodeToString(b), Title: title, State: "running", Started: time.Now().UnixMilli()}
	j.mu.Lock()
	j.all = append(j.all, job)
	if len(j.all) > 30 {
		j.all = j.all[len(j.all)-30:]
	}
	j.mu.Unlock()
	j.publish(job, "")

	go func() {
		l := j.lock(lockKey)
		l.Lock()
		defer l.Unlock()
		ok := true
		for _, s := range steps {
			j.publish(job, "$ "+strings.Join(s.args, " "))
			if err := j.run(job, s); err != nil {
				j.publish(job, "error: "+err.Error())
				ok = false
				break
			}
		}
		j.mu.Lock()
		job.Ended = time.Now().UnixMilli()
		if ok {
			job.State = "ok"
		} else {
			job.State = "failed"
		}
		j.mu.Unlock()
		j.publish(job, "")
	}()
	return job
}

func (j *Jobs) run(job *Job, s step) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.args[0], s.args[1:]...)
	cmd.Dir = s.dir
	cmd.Env = s.env
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			// compose draws progress with carriage returns; keep the last frame
			line := sc.Text()
			if i := strings.LastIndex(line, "\r"); i >= 0 {
				line = line[i+1:]
			}
			j.publish(job, stripANSI(line))
		}
		close(done)
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	return err
}

func (j *Jobs) publish(job *Job, line string) {
	j.mu.Lock()
	if line != "" {
		job.Lines = append(job.Lines, line)
		if len(job.Lines) > 2000 {
			job.Lines = job.Lines[len(job.Lines)-2000:]
		}
	}
	snap := Job{ID: job.ID, Title: job.Title, State: job.State, Started: job.Started, Ended: job.Ended}
	j.mu.Unlock()
	// send only the new line; the browser appends
	j.hub.Broadcast("job", map[string]any{"job": snap, "line": line})
}

// RunQuick runs a short command synchronously and returns combined output.
func RunQuick(ctx context.Context, s step) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.args[0], s.args[1:]...)
	cmd.Dir = s.dir
	cmd.Env = s.env
	out, err := cmd.CombinedOutput()
	return stripANSI(string(out)), err
}

func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !(s[i] >= '@' && s[i] <= '~') {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
