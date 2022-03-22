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
	"os"
	"regexp"
	"time"

	"github.com/pkg/errors"
)

const (
	envReattachConfig = "TF_REATTACH_PROVIDERS"
	regexReattachLine = envReattachConfig + `='(.*)'`
	reattachTimeout   = 1 * time.Minute
)

func (ws *WorkspaceStore) startSharedServer() error { //nolint:gocyclo
	if len(ws.nativeProviderPath) == 0 {
		return nil
	}
	log := ws.logger.WithValues("nativeProviderPath", ws.nativeProviderPath, "nativeProviderArgs", ws.nativeProviderArgs)
	if ws.reattachConfig != "" {
		log.Debug("Shared gRPC server is running...", "reattachConfig", ws.reattachConfig)
		return nil
	}
	errCh := make(chan error, 1)
	reattachCh := make(chan string, 1)
	re, err := regexp.Compile(regexReattachLine)
	if err != nil {
		return errors.Wrap(err, "failed to compile regexp")
	}

	go func() {
		defer close(errCh)
		defer close(reattachCh)
		defer func() {
			ws.mu.Lock()
			ws.reattachConfig = ""
			ws.mu.Unlock()
		}()
		//#nosec G204 no user input
		cmd := ws.executor.Command(ws.nativeProviderPath, ws.nativeProviderArgs...)
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
		ws.reattachConfig = reattachConfig
		return errors.Wrapf(os.Setenv(envReattachConfig, ws.reattachConfig),
			"could not set reattach config env variable %q to value: %s", envReattachConfig, ws.reattachConfig)
	case err := <-errCh:
		return err
	case <-time.After(reattachTimeout):
		return errors.Errorf("timed out after %v while waiting for the reattach configuration string", reattachTimeout)
	}
}
