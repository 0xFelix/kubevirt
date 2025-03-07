/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright The KubeVirt Authors
 *
 */

package serialconnect

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	v1 "kubevirt.io/client-go/kubevirt/typed/core/v1"

	"kubevirt.io/kubevirt/pkg/virtctl/clientconfig"
	"kubevirt.io/kubevirt/pkg/virtctl/templates"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "serialconnect VMI1 VMI2",
		Short:   "Connect serial consoles of two VMs together.",
		Example: usage(),
		Args:    cobra.ExactArgs(2),
		RunE:    run,
	}
	cmd.SetUsageTemplate(templates.UsageTemplate())
	return cmd
}

func usage() string {
	return `  # Connect serial consoles of two VMIs together:
  {{ProgramName}} serialconnect myvmi1 myvmi2`
}

func run(cmd *cobra.Command, args []string) error {
	vmi1 := args[0]
	vmi2 := args[1]

	client, namespace, _, err := clientconfig.ClientAndNamespaceFromContext(cmd.Context())
	if err != nil {
		return fmt.Errorf("cannot obtain KubeVirt client: %v", err)
	}

	s1, err := client.VirtualMachineInstance(namespace).SerialConsole(vmi1, &v1.SerialConsoleOptions{})
	if err != nil {
		return err
	}

	s2, err := client.VirtualMachineInstance(namespace).SerialConsole(vmi2, &v1.SerialConsoleOptions{})
	if err != nil {
		return err
	}

	done := make(chan struct{})
	errChan := make(chan error)

	r1, w1 := io.Pipe()
	r2, w2 := io.Pipe()
	defer w1.Close()
	defer w2.Close()

	go func() {
		errChan <- s1.Stream(v1.StreamOptions{
			In:  r1,
			Out: w2,
		})
	}()
	go func() {
		errChan <- s2.Stream(v1.StreamOptions{
			In:  r2,
			Out: w1,
		})
	}()

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		close(done)
	}()

	cmd.Printf("Connecting serial consoles of '%s' and '%s', press Ctrl+C to exit...", vmi1, vmi2)

	select {
	case <-done:
	case err = <-errChan:
	}

	return err
}
