/*
Copyright 2021 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package process

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/pkg/errors"
)

const (
	errKill           = "failed to kill process"
	errNotInitialized = "process not initialized"
	errNotBuffered    = "stream not buffered"
)

func New(pathExec string, args []string, cwd string, autoStart, bufferStreams, logStreams bool,
	logger logging.Logger) (*Info, error) {
	result := &Info{
		cmd:        exec.Command(pathExec, args...),
		wgStreams:  &sync.WaitGroup{},
		chStopped:  make(chan bool),
		logger:     logger,
		logStreams: logStreams,
	}
	if cwd != "" {
		result.cmd.Dir = cwd
	}
	if bufferStreams {
		result.buffStdout = &bytes.Buffer{}
		result.buffStderr = &bytes.Buffer{}
	}

	stdout, err := result.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	result.stdout = stdout

	stderr, err := result.cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	result.stderr = stderr

	if autoStart {
		return result, result.cmd.Start()
	}
	return result, nil
}

type lineProcessor func(line string, logger logging.Logger)

type Info struct {
	cmd        *exec.Cmd
	wgStreams  *sync.WaitGroup
	chStopped  chan bool
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	logger     logging.Logger
	buffStdout *bytes.Buffer
	buffStderr *bytes.Buffer
	logStreams bool
}

func (pi *Info) StartStdout(processor lineProcessor) {
	pi.processLines(pi.stdout, processor)
}

func (pi *Info) LogStdout() {
	pi.StartStdout(nil)
}

func (pi *Info) StartStderr(processor lineProcessor) {
	pi.processLines(pi.stderr, processor)
}

func (pi *Info) LogStderr() {
	pi.StartStderr(nil)
}

func (pi *Info) Log() {
	pi.LogStdout()
	pi.LogStderr()
}

func (pi *Info) StdoutAsString() (string, error) {
	if pi.buffStdout == nil {
		return "", errors.New(errNotBuffered)
	}
	return pi.buffStdout.String(), nil
}

func (pi *Info) StderrAsString() (string, error) {
	if pi.buffStderr == nil {
		return "", errors.New(errNotBuffered)
	}
	return pi.buffStderr.String(), nil
}

func (pi *Info) Run(ctx context.Context) ([]byte, error) {
	if pi.cmd == nil {
		return nil, errors.New(errNotInitialized)
	}
	// when this routine is scheduled, if ctx is already done, do not start the process
	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	default:
	}

	chErr := make(chan error)
	go func() {
		chErr <- pi.cmd.Run()
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case err := <-chErr:
		var stderr []byte
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = exitErr.Stderr
		}
		return stderr, err
	}
}

func (pi *Info) RunWithDeadline(ctx context.Context, to time.Duration) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(to))
	defer cancel()
	return pi.Run(ctx)
}

func (pi *Info) Start() error {
	if pi.cmd == nil {
		return errors.New(errNotInitialized)
	}
	return pi.cmd.Start()
}

func (pi *Info) Stopped() chan bool {
	return pi.chStopped
}

func (pi *Info) Wait() {
	pi.wgStreams.Wait()
	close(pi.chStopped)
}

func (pi *Info) WaitError() error {
	pi.Wait()
	err := pi.cmd.Wait()
	if _, ok := err.(*exec.ExitError); (!ok && err != nil) || err == nil {
		return err
	}
	var stderr []byte
	if pi.buffStderr != nil {
		stderr = pi.buffStderr.Bytes()
	}
	return &exec.ExitError{
		ProcessState: pi.cmd.ProcessState,
		Stderr:       stderr,
	}
}

func (pi *Info) GetProcessState() *os.ProcessState {
	return pi.cmd.ProcessState
}

func (pi *Info) GetCmd() *exec.Cmd {
	return pi.cmd
}

func (pi *Info) Kill() error {
	if pi.cmd == nil || pi.cmd.Process == nil {
		return nil
	}
	return errors.Wrap(pi.cmd.Process.Kill(), errKill)
}

func (pi *Info) processLines(stream io.ReadCloser, processor lineProcessor) {
	streamName := "<unknown>"
	var buff *bytes.Buffer
	switch stream {
	case pi.stdout:
		streamName = "stdout"
		buff = pi.buffStdout

	case pi.stderr:
		streamName = "stderr"
		buff = pi.buffStderr
	}

	pi.wgStreams.Add(1)
	go func() {
		defer pi.wgStreams.Done()
		logStreamLines(stream, processor,
			pi.logger.WithValues("stream", streamName, "exec", pi.cmd.Path, "cwd", pi.cmd.Dir),
			pi.logStreams, buff)
	}()
}

func logStreamLines(reader io.Reader, process lineProcessor, logger logging.Logger,
	logStream bool, buff *bytes.Buffer) {
	s := bufio.NewScanner(reader)
	for s.Scan() {
		line := s.Text()
		if logStream {
			logger.Info(line)
		}
		if buff != nil {
			buff.WriteString(line)
		}
		if process != nil {
			process(line, logger)
		}
	}
	if err := s.Err(); err != nil {
		logger.Info("Error during scanning", "error", err)
	}
}
