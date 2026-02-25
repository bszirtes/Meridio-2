/*
Copyright (c) 2026 OpenInfra Foundation Europe. All rights reserved.

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

package bird

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
)

var errBirdRunning = errors.New("bird is already running")

type Bird struct {
	SocketPath string
	ConfigFile string
	running    bool
	mu         sync.Mutex
}

func New() *Bird {
	return &Bird{
		SocketPath: "/var/run/bird/bird.ctl",
		ConfigFile: "/etc/bird/bird.conf",
	}
}

func (b *Bird) Run(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return errBirdRunning
	}

	if _, err := os.Stat(b.ConfigFile); errors.Is(err, os.ErrNotExist) {
		if err := b.writeConfig([]string{}, []*meridio2v1alpha1.GatewayRouter{}); err != nil {
			b.mu.Unlock()
			return err
		}
	}

	b.running = true
	b.mu.Unlock()

	cmd := exec.CommandContext(ctx, "bird", "-d", "-c", b.ConfigFile, "-s", b.SocketPath)
	out, err := cmd.CombinedOutput()
	if err != nil && !errors.Is(err, context.Cause(ctx)) {
		return fmt.Errorf("bird failed: %w: %s", err, out)
	}
	return nil
}

func (b *Bird) Configure(ctx context.Context, vips []string, routers []*meridio2v1alpha1.GatewayRouter) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.writeConfig(vips, routers); err != nil {
		return err
	}

	if b.running {
		cmd := exec.CommandContext(ctx, "birdc", "-s", b.SocketPath, "configure", `"`+b.ConfigFile+`"`)
		out, err := cmd.CombinedOutput()
		if err != nil && !errors.Is(err, context.Cause(ctx)) {
			return fmt.Errorf("birdc configure failed: %w: %s", err, out)
		}
	}

	return setPolicyRoutes(vips)
}

func (b *Bird) generateConfig(vips []string, routers []*meridio2v1alpha1.GatewayRouter) (string, error) {
	routersConf, err := routersConfig(routers)
	if err != nil {
		return "", err
	}
	return baseConfig() + "\n\n" + vipsConfig(vips) + "\n\n" + routersConf, nil
}

func (b *Bird) writeConfig(vips []string, routers []*meridio2v1alpha1.GatewayRouter) error {
	conf, err := b.generateConfig(vips, routers)
	if err != nil {
		return err
	}

	file, err := os.Create(b.ConfigFile)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(conf)
	return err
}
