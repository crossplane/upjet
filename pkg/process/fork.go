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
	"regexp"
	"sync"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/pkg/errors"
)

const (
	errKill           = "failed to kill process"
	errNotInitialized = "process not initialized"
	errNotStarted     = "process not started"
	errNotBuffered    = "stream not buffered"
)

var (
	ansiEscaper = regexp.MustCompile("[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))")
)

// New initializes a new process.Info for running processes.
// New call is itself is never blocking and can optionally start the process
// if autoStart is set. The process run the command specified by pathExec
// passing the command-line arguments specified by args. The process's
// working directory is specified with cwd. If bufferStreams is set, stdout &
// stderr are buffered and will be available to the caller when the process
// terminates. If logStreams is set, then stdout & stderr streams are logged
// using the specified logger. Even if logStreams is not set, a logger needs to
// be specified for logging of the library itself.
func New(pathExec string, args []string, cwd string, autoStart, bufferStreams, logStreams bool,
	logger logging.Logger) (*Info, error) {
	result := &Info{
		// pathExec or the args are never a user input in tfcli
		cmd:        exec.Command(pathExec, args...), //nolint:gosec
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

// Info represents a command with associated stdout & stderr buffers
// if buffering is requested. The command can either be run synchronously or
// asynchronously.
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

// StartStdout starts line processing of stdout stream using the specified
// lineProcessor
func (pi *Info) StartStdout(processor lineProcessor) {
	pi.processStream(pi.stdout, processor)
}

// LogStdout logs contents of stdout using the configured logger
func (pi *Info) LogStdout() {
	pi.StartStdout(nil)
}

// StartStderr starts line processing of stderr stream using the specified
// lineProcessor
func (pi *Info) StartStderr(processor lineProcessor) {
	pi.processStream(pi.stderr, processor)
}

// LogStderr logs contents of stderr using the configured logger
func (pi *Info) LogStderr() {
	pi.StartStderr(nil)
}

// Log logs contents of stdout & stderr using the configured logger
func (pi *Info) Log() {
	pi.LogStdout()
	pi.LogStderr()
}

// StdoutAsString returns the contents of stdout stream as a string
func (pi *Info) StdoutAsString() (string, error) {
	if pi.buffStdout == nil {
		return "", errors.New(errNotBuffered)
	}
	return ansiEscaper.ReplaceAllString(pi.buffStdout.String(), ""), nil
}

// StderrAsString returns the contents of stderr stream as a string
func (pi *Info) StderrAsString() (string, error) {
	if pi.buffStderr == nil {
		return "", errors.New(errNotBuffered)
	}
	return ansiEscaper.ReplaceAllString(pi.buffStderr.String(), ""), nil
}

// Run runs the configured command and blocks the caller until the process
// terminates
func (pi *Info) Run(ctx context.Context) error {
	if pi.cmd == nil {
		return errors.New(errNotInitialized)
	}
	// when this routine is scheduled, if ctx is already done, do not start the process
	select {
	case <-ctx.Done():
		return ctx.Err()

	default:
	}

	chErr := make(chan error, 1)

	go func() {
		defer close(chErr)
		if err := pi.cmd.Start(); err != nil {
			chErr <- err
			return
		}
		pi.LogStdout()
		pi.LogStderr()
		chErr <- pi.WaitError()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()

	case err := <-chErr:
		return err
	}
}

// Start starts the configured command without blocking the caller.
// Clients can also request to autostart the process if autoStart is
// set in the call to process.New.
func (pi *Info) Start() error {
	if pi.cmd == nil {
		return errors.New(errNotInitialized)
	}
	return pi.cmd.Start()
}

func (pi *Info) wait() {
	pi.wgStreams.Wait()
	close(pi.chStopped)
}

// WaitError waits for the started process to finish returning an error if
// the process did not exit with code 0. This is a blocking call.
func (pi *Info) WaitError() error {
	pi.wait()
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

// GetProcessState returns the exec.ProcessState associated with the process,
// if one exists (i.e., if the process has terminated).
func (pi *Info) GetProcessState() *os.ProcessState {
	return pi.cmd.ProcessState
}

// GetPid returns the pid of a started pipeline process
func (pi *Info) GetPid() (int, error) {
	if pi.cmd == nil || pi.cmd.Process == nil {
		return 0, errors.New(errNotStarted)
	}
	return pi.cmd.Process.Pid, nil
}

// GetCmd returns the associated exec.Cmd with the configured command.
func (pi *Info) GetCmd() *exec.Cmd {
	return pi.cmd
}

// Kill attempts to kill the associated process forcefully
func (pi *Info) Kill() error {
	if pi.cmd == nil || pi.cmd.Process == nil || (pi.cmd.ProcessState != nil && pi.cmd.ProcessState.Exited()) {
		return nil
	}
	return errors.Wrap(pi.cmd.Process.Kill(), errKill)
}

func (pi *Info) processStream(stream io.ReadCloser, processor lineProcessor) {
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
		if pi.logStreams || processor != nil {
			processLines(stream, processor,
				pi.logger.WithValues("stream", streamName, "exec", pi.cmd.Path, "cwd", pi.cmd.Dir),
				pi.logStreams, buff)

			return
		}

		if buff == nil {
			return
		}
		if _, err := io.Copy(buff, stream); err != nil {
			pi.logger.Info("Error during copying", "error", err)
		}
	}()
}

func processLines(reader io.Reader, process lineProcessor, logger logging.Logger,
	logStream bool, sw io.StringWriter) {
	s := bufio.NewScanner(reader)
	for s.Scan() {
		line := s.Text()
		if logStream {
			logger.Info(line)
		}
		if sw != nil {
			if _, err := sw.WriteString(line); err != nil {
				logger.Info("Error while writing to the buffer", "error", err)
			}
		}
		if process != nil {
			process(line, logger)
		}
	}
	if err := s.Err(); err != nil {
		logger.Info("Error during scanning", "error", err)
	}
}
