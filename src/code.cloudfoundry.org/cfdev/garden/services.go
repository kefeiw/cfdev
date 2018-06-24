package garrden

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"code.cloudfoundry.org/cfdev/errors"
	"code.cloudfoundry.org/garden"
	yaml "gopkg.in/yaml.v2"
)

func DeployService(client garden.Client, handle, script string) error {
	container, err := client.Create(containerSpec(handle))
	if err != nil {
		return err
	}

	process, err := container.Run(garden.ProcessSpec{
		ID:   handle,
		Path: "/bin/bash",
		Args: []string{filepath.Join("/var/vcap/cache", script)},
		User: "root",
	}, garden.ProcessIO{})

	if err != nil {
		return err
	}

	exitCode, err := process.Wait()
	if err != nil {
		return err
	}

	if exitCode != 0 {
		return errors.SafeWrap(nil, fmt.Sprintf("process exited with status %d", exitCode))
	}

	client.Destroy(handle)

	return nil
}

type Service struct {
	Name       string `yaml:"name"`
	Handle     string `yaml:"handle"`
	Script     string `yaml:"script"`
	Deployment string `yaml:"deployment"`
}

func GetServices(client garden.Client, handle, script string) ([]Service, error) {
	container, err := client.Create(containerSpec(handle))
	if err != nil {
		return err
	}
	r, err := container.StreamOut(container.StreamOutSpec{})
	if err != nil {
		return err
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	services := make([]Service, 0)
	err = yaml.Unmarshal(b, services)
	return services, err
}

func containerSpec(handle string) garden.ContainerSpec {
	return garden.ContainerSpec{
		Handle:     handle,
		Privileged: true,
		Network:    "10.246.0.0/16",
		Image: garden.ImageRef{
			URI: "/var/vcap/cache/workspace.tar",
		},
		BindMounts: []garden.BindMount{
			{
				SrcPath: "/var/vcap",
				DstPath: "/var/vcap",
				Mode:    garden.BindMountModeRW,
			},
			{
				SrcPath: "/var/vcap/cache",
				DstPath: "/var/vcap/cache",
				Mode:    garden.BindMountModeRO,
			},
		},
	}
}
