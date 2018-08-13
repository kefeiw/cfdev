package process

import (
	"net"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/cfdev/errors"

	"encoding/json"
	"io/ioutil"
	"os"
	"path"

	"code.cloudfoundry.org/cfdev/daemon"
	"code.cloudfoundry.org/cfdev/env"
)

const retries = 5

func (v *VpnKit) Start() error {
	if err := v.setup(); err != nil {
		return errors.SafeWrap(err, "Failed to setup VPNKit")
	}
	if err := v.DaemonRunner.AddDaemon(v.daemonSpec()); err != nil {
		return errors.SafeWrap(err, "install vpnkit")
	}
	if err := v.DaemonRunner.Start(VpnKitLabel); err != nil {
		return errors.SafeWrap(err, "start vpnkit")
	}
	attempt := 0
	for {
		conn, err := net.Dial("unix", filepath.Join(v.Config.VpnKitStateDir, "vpnkit_eth.sock"))
		if err == nil {
			conn.Close()
			return nil
		} else if attempt >= retries {
			return errors.SafeWrap(err, "conenct to vpnkit")
		} else {
			time.Sleep(time.Second)
			attempt++
		}
	}
}

func (v *VpnKit) Destroy() error {
	return v.DaemonRunner.RemoveDaemon(VpnKitLabel)
}

func (v *VpnKit) Watch(exit chan string) {
	go func() {
		for {
			running, err := v.DaemonRunner.IsRunning(VpnKitLabel)
			if !running && err == nil {
				exit <- "vpnkit"
				return
			}
			time.Sleep(5 * time.Second)
		}
	}()
}

func (v *VpnKit) daemonSpec() daemon.DaemonSpec {
	return daemon.DaemonSpec{
		Label:       VpnKitLabel,
		Program:     path.Join(v.Config.CacheDir, "vpnkit"),
		SessionType: "Background",
		ProgramArguments: []string{
			path.Join(v.Config.CacheDir, "vpnkit"),
			"--ethernet", path.Join(v.Config.VpnKitStateDir, "vpnkit_eth.sock"),
			"--port", path.Join(v.Config.VpnKitStateDir, "vpnkit_port.sock"),
			"--vsock-path", path.Join(v.Config.StateDir, "connect"),
			"--http", path.Join(v.Config.VpnKitStateDir, "http_proxy.json"),
			"--host-names", "host.cfdev.sh",
		},
		RunAtLoad:  false,
		StdoutPath: path.Join(v.Config.CFDevHome, "vpnkit.stdout.log"),
		StderrPath: path.Join(v.Config.CFDevHome, "vpnkit.stderr.log"),
	}
}

func (v *VpnKit) setup() error {
	httpProxyPath := filepath.Join(v.Config.VpnKitStateDir, "http_proxy.json")

	proxyConfig := env.BuildProxyConfig(v.Config.BoshDirectorIP, v.Config.CFRouterIP)
	proxyContents, err := json.Marshal(proxyConfig)
	if err != nil {
		return errors.SafeWrap(err, "Unable to create proxy config")
	}

	if _, err := os.Stat(httpProxyPath); !os.IsNotExist(err) {
		err = os.Remove(httpProxyPath)
		if err != nil {
			return errors.SafeWrap(err, "Unable to remove 'http_proxy.json'")
		}
	}

	httpProxyConfig := []byte(proxyContents)
	err = ioutil.WriteFile(httpProxyPath, httpProxyConfig, 0777)
	if err != nil {
		return err
	}
	return nil
}
