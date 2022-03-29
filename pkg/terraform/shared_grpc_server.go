/*
Copyright 2022 The Crossplane Authors.

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

package terraform

import (
	"bufio"
	"regexp"
	"time"

	"github.com/pkg/errors"
)

const (
	// error messages
	errNativeProviderPath = "native provider path is not configured"
	errFmtTimeout         = "timed out after %v while waiting for the reattach configuration string"

	envReattachConfig = "TF_REATTACH_PROVIDERS"
	regexReattachLine = envReattachConfig + `='(.*)'`
	reattachTimeout   = 1 * time.Minute
)

// StartSharedServer starts a shared gRPC server if not already running
// A logger, native provider's path and command-line arguments to be
// passed to it must have been properly configured.
// Returns any errors encountered and the reattachment configuration for
// the native provider.
func (sr *SharedGRPCRunner) StartSharedServer() (string, error) { //nolint:gocyclo
	sr.mu.Lock()
	defer sr.mu.Unlock()
	if len(sr.nativeProviderPath) == 0 {
		return "", errors.New(errNativeProviderPath)
	}
	log := sr.logger.WithValues("nativeProviderPath", sr.nativeProviderPath, "nativeProviderArgs", sr.nativeProviderArgs)
	if sr.reattachConfig != "" {
		log.Debug("Shared gRPC server is running...", "reattachConfig", sr.reattachConfig)
		return sr.reattachConfig, nil
	}
	errCh := make(chan error, 1)
	reattachCh := make(chan string, 1)
	re, err := regexp.Compile(regexReattachLine)
	if err != nil {
		return "", errors.Wrap(err, "failed to compile regexp")
	}

	go func() {
		defer close(errCh)
		defer close(reattachCh)
		defer func() {
			sr.mu.Lock()
			sr.reattachConfig = ""
			sr.mu.Unlock()
		}()
		//#nosec G204 no user input
		cmd := sr.executor.Command(sr.nativeProviderPath, sr.nativeProviderArgs...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errCh <- err
			return
		}
		if err := cmd.Start(); err != nil {
			errCh <- err
			return
		}
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			t := scanner.Text()
			matches := re.FindStringSubmatch(t)
			if matches == nil {
				continue
			}
			reattachCh <- matches[1]
			break
		}
		if err := cmd.Wait(); err != nil {
			log.Info("Native Terraform provider process error", "error", err)
			errCh <- err
		}
	}()

	select {
	case reattachConfig := <-reattachCh:
		sr.reattachConfig = reattachConfig
		return sr.reattachConfig, nil
	case err := <-errCh:
		return "", err
	case <-sr.clock.After(reattachTimeout):
		return "", errors.Errorf(errFmtTimeout, reattachTimeout)
	}
}
