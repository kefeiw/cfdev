package start

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code.cloudfoundry.org/cfdev/cfanalytics"
	"code.cloudfoundry.org/cfdev/env"
	"code.cloudfoundry.org/cfdev/errors"
	"github.com/spf13/cobra"
	"code.cloudfoundry.org/cfdev/garden"
	"github.com/hooklift/iso9660"
	"io"
	"io/ioutil"
	"gopkg.in/yaml.v2"
	"code.cloudfoundry.org/cfdev/resource"
)

var (
	services     []garden.Service
	message      string
	isCompatible bool
)

const compatibilityVersion = "v1"

func (s *Start) Cmd() *cobra.Command {
	args := Args{}
	cmd := &cobra.Command{
		Use: "start",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := s.Execute(args); err != nil {
				return errors.SafeWrap(err, "cf dev start")
			}
			return nil
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVarP(&args.DepsIsoPath, "file", "f", "", "path to .dev file containing bosh & cf bits")
	pf.StringVarP(&args.Registries, "registries", "r", "", "docker registries that skip ssl validation - ie. host:port,host2:port2")
	pf.IntVarP(&args.Cpus, "cpus", "c", 4, "cpus to allocate to vm")
	pf.IntVarP(&args.Mem, "memory", "m", 4096, "memory to allocate to vm in MB")
	pf.BoolVarP(&args.NoProvision, "no-provision", "n", false, "start vm but do not provision")

	pf.MarkHidden("no-provision")
	return cmd
}

func (s *Start) Execute(args Args) error {
	go func() {
		select {
		case <-s.Exit:
			// no-op
		case name := <-s.LocalExit:
			s.UI.Say("ERROR: %s has stopped", name)
		}
		s.VpnKit.Stop()
		os.Exit(128)
	}()

	depsIsoName := "cf"
	depsIsoPath := filepath.Join(s.Config.CacheDir, "cf-deps.iso")
	depsToDownload := s.Config.Dependencies
	if args.DepsIsoPath != "" {
		depsIsoName = filepath.Base(args.DepsIsoPath)
		var err error
		depsIsoPath, err = filepath.Abs(args.DepsIsoPath)
		if err != nil {
			return errors.SafeWrap(err, "determining absolute path to deps iso")
		}
		if _, err := os.Stat(depsIsoPath); os.IsNotExist(err) {
			return fmt.Errorf("no file found at: %s", depsIsoPath)
		}

		services, message, isCompatible, err = readIsoAndVerifyVersion(depsIsoPath)
		if err != nil {
			return errors.SafeWrap(err, fmt.Sprintf("%s is not compatible with CF Dev. Please use a compatible file.", depsIsoName))
		}

		if !isCompatible {
			return fmt.Errorf("%s is not compatible with CF Dev. Please use a compatible file", depsIsoName)
		}

		depsToDownload = resource.Catalog{}
		for _, item := range s.Config.Dependencies.Items {
			if item.Name != "cf-deps.iso" {
				depsToDownload.Items = append(depsToDownload.Items, item)
			}
		}
	}

	s.AnalyticsToggle.SetProp("type", depsIsoName)
	s.Analytics.Event(cfanalytics.START_BEGIN)

	if err := env.Setup(s.Config); err != nil {
		return errors.SafeWrap(err, "environment setup")
	}

	if err := CleanupStateDir(s.Config); err != nil {
		return errors.SafeWrap(err, "cleaning state directory")
	}

	if err := s.HostNet.AddLoopbackAliases(s.Config.BoshDirectorIP, s.Config.CFRouterIP); err != nil {
		return errors.SafeWrap(err, "adding aliases")
	}

	registries, err := s.parseDockerRegistriesFlag(args.Registries)
	if err != nil {
		return errors.SafeWrap(err, "Unable to parse docker registries")
	}

	s.UI.Say("Downloading Resources...")
	if err := s.Cache.Sync(depsToDownload); err != nil {
		return errors.SafeWrap(err, "Unable to sync assets")
	}

	services, message, isCompatible, err := readIsoAndVerifyVersion(depsIsoPath)
	if err != nil {
		return errors.SafeWrap(err, "Incompatible iso specified")
	}

	if !isCompatible {
		return fmt.Errorf("%s is not a compatible iso file", depsIsoName)
	}

	s.UI.Say("Creating the VM...")
	if err := s.HyperV.CreateVM(depsIsoPath); err != nil {
		return errors.SafeWrap(err, "Unable to create VM")
	}

	s.UI.Say("Starting VPNKit...")
	if err := s.VpnKit.Start(); err != nil {
		return errors.SafeWrap(err, "starting vpnkit")
	}

	s.UI.Say("Starting the VM...")
	if err := s.HyperV.Start("cfdev"); err != nil {
		return errors.SafeWrap(err, "starting vpnkit")
	}

	s.UI.Say("Waiting for Garden...")
	s.waitForGarden()

	if args.NoProvision {
		s.UI.Say("VM will not be provisioned because '-n' (no-provision) flag was specified.")
		return nil
	}

	s.UI.Say("Deploying the BOSH Director...")
	if err := s.GardenClient.DeployBosh(); err != nil {
		return errors.SafeWrap(err, "Failed to deploy the BOSH Director")
	}

	s.UI.Say("Deploying CF...")
	s.GardenClient.ReportProgress(s.UI, "cf")
	if err := s.GardenClient.DeployCloudFoundry(registries); err != nil {
		return errors.SafeWrap(err, "Failed to deploy the Cloud Foundry")
	}

	err = s.GardenClient.DeployServices(s.UI, services)
	if err != nil {
		return err
	}

	s.UI.Say(`

	  ██████╗███████╗██████╗ ███████╗██╗   ██╗
	 ██╔════╝██╔════╝██╔══██╗██╔════╝██║   ██║
	 ██║     █████╗  ██║  ██║█████╗  ██║   ██║
	 ██║     ██╔══╝  ██║  ██║██╔══╝  ╚██╗ ██╔╝
	 ╚██████╗██║     ██████╔╝███████╗ ╚████╔╝
	  ╚═════╝╚═╝     ╚═════╝ ╚══════╝  ╚═══╝
	             is now running!

	To begin using CF Dev, please run:
	    cf login -a https://api.dev.cfdev.sh --skip-ssl-validation

	Admin user => Email: admin / Password: admin
	Regular user => Email: user / Password: pass
	`)

	if message != "" {
		s.UI.Say(message)
	}

	s.Analytics.Event(cfanalytics.START_END)

	return nil
}

func (s *Start) waitForGarden() {
	for {
		if err := s.GardenClient.Ping(); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
}

func (s *Start) parseDockerRegistriesFlag(flag string) ([]string, error) {
	if flag == "" {
		return nil, nil
	}

	values := strings.Split(flag, ",")

	registries := make([]string, 0, len(values))

	for _, value := range values {
		// Including the // will cause url.Parse to validate 'value' as a host:port
		u, err := url.Parse("//" + value)

		if err != nil {
			// Grab the more succinct error message
			if urlErr, ok := err.(*url.Error); ok {
				err = urlErr.Err
			}
			return nil, fmt.Errorf("'%v' - %v", value, err)
		}
		registries = append(registries, u.Host)
	}
	return registries, nil
}

func readIsoAndVerifyVersion(isoFile string) ([]garden.Service, string, bool, error) {

	file, err := os.Open(isoFile)
	if err != nil {
		return nil, "", false, err
	}

	r, err := iso9660.NewReader(file)
	if err != nil {
		return nil, "", false, err
	}

	for {
		f, err := r.Next()
		if err == io.EOF {
			fmt.Println("File not found")
			return nil, "", false, err
		}

		if err != nil {
			return nil, "", false, err
		}

		if strings.Contains(f.Name(), "metadata.yml") {
			buf, err := ioutil.ReadAll(f.Sys().(io.Reader))
			if err != nil {
				return nil, "", false, err
			}

			metadata := struct {
				Version  string           `yaml:"compatibility_version"`
				Message  string           `yaml:"splash_message"`
				Services []garden.Service `yaml:"services"`
			}{}

			err = yaml.Unmarshal(buf, &metadata)
			if err != nil {
				return nil, "", false, err
			}

			if metadata.Version != compatibilityVersion {
				return nil, "", false, nil
			}

			return metadata.Services, metadata.Message, true, nil
		}
	}
	return nil, "", false, nil
}