package env_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"os"

	"fmt"
	"io/ioutil"
	"path/filepath"

	"code.cloudfoundry.org/cfdev/config"
	"code.cloudfoundry.org/cfdev/env"
)

var _ = Describe("env", func() {
	Context("when proxy env vars are set", func() {
		BeforeEach(func() {
			os.Setenv("HTTP_PROXY", "some-http-proxy")
			os.Setenv("HTTPS_PROXY", "some-https-proxy")
			os.Setenv("NO_PROXY", "some-no-proxy")
		})
		AfterEach(func() {
			os.Unsetenv("HTTP_PROXY")
			os.Unsetenv("HTTPS_PROXY")
			os.Unsetenv("NO_PROXY")
		})
		It("returns the http config", func() {
			proxyConfig := env.BuildProxyConfig("bosh-ip", "router-ip", "host-ip")
			Expect(proxyConfig.Http).To(Equal("some-http-proxy"))
			Expect(proxyConfig.Https).To(Equal("some-https-proxy"))
			Expect(proxyConfig.NoProxy).To(Equal("some-no-proxy,bosh-ip,router-ip,host-ip"))
		})
	})

	Context("when multiple mixed case proxy envs prioritize uppercase", func() {
		BeforeEach(func() {
			os.Setenv("http_proxy", "lower-case-http-proxy")
			os.Setenv("HTTP_PROXY", "upper-some-http-proxy")
			os.Setenv("https_proxy", "lower-case-https-proxy")
			os.Setenv("HTTPS_PROXY", "upper-some-https-proxy")
			os.Setenv("no_proxy", "lower-some-no-proxy")
			os.Setenv("NO_PROXY", "upper-some-no-proxy,bosh-ip,router-ip")
		})
		AfterEach(func() {
			os.Unsetenv("http_proxy")
			os.Unsetenv("HTTP_PROXY")
			os.Unsetenv("https_proxy")
			os.Unsetenv("HTTPS_PROXY")
			os.Unsetenv("no_proxy")
			os.Unsetenv("NO_PROXY")
		})
		It("returns the http config", func() {
			proxyConfig := env.BuildProxyConfig("bosh-ip", "router-ip", "host-ip")
			Expect(proxyConfig.Http).To(Equal("upper-some-http-proxy"))
			Expect(proxyConfig.Https).To(Equal("upper-some-https-proxy"))
			Expect(proxyConfig.NoProxy).To(Equal("upper-some-no-proxy,bosh-ip,router-ip,host-ip"))
		})
	})

	Describe("SetupHomeDir", func() {
		var dir, homeDir, cacheDir, stateDir, vpnkitStateDir string
		var err error
		var conf config.Config

		BeforeEach(func() {
			dir, err = ioutil.TempDir(os.TempDir(), "test-space")
			Expect(err).NotTo(HaveOccurred())

			homeDir = filepath.Join(dir, "some-cfdev-home")
			cacheDir = filepath.Join(dir, "some-cache-dir")
			stateDir = filepath.Join(dir, "some-state-dir")
			vpnkitStateDir = filepath.Join(dir, "some-vpnkit-state-dir")

			conf = config.Config{
				CFDevHome: homeDir,
				CacheDir:  cacheDir,
				StateDir:  stateDir,
				VpnKitStateDir:  vpnkitStateDir,
			}
		})

		AfterEach(func() {
			os.RemoveAll(dir)
		})

		It("creates home, state, and cache dirs", func() {
			Expect(env.SetupHomeDir(conf)).To(Succeed())
			_, err := os.Stat(homeDir)
			Expect(err).NotTo(HaveOccurred())
			_, err = os.Stat(cacheDir)
			Expect(err).NotTo(HaveOccurred())
			_, err = os.Stat(stateDir)
			Expect(err).NotTo(HaveOccurred())
			_, err = os.Stat(vpnkitStateDir)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("when there is already state in the home dir", func() {
			var oldFile, oldDir string

			BeforeEach(func(){
				oldFile = filepath.Join(stateDir, "some-file")
				oldDir = filepath.Join(stateDir, "some-dir")

				Expect(os.Mkdir(homeDir, 0755)).To(Succeed())
				Expect(os.Mkdir(cacheDir, 0755)).To(Succeed())
				Expect(os.Mkdir(stateDir, 0755)).To(Succeed())

				Expect(ioutil.WriteFile(oldFile, []byte{}, 0644)).To(Succeed())
				Expect(os.Mkdir(oldDir, 0755)).To(Succeed())
				Expect(ioutil.WriteFile(filepath.Join(oldDir, "some-other-file"), []byte{}, 0400)).To(Succeed())
			})

			It("cleans out the state dir", func() {
				Expect(env.SetupHomeDir(conf)).To(Succeed())
				_, err := os.Stat(oldFile)
				Expect(os.IsNotExist(err)).To(BeTrue())
				_, err = os.Stat(oldDir)
				Expect(os.IsNotExist(err)).To(BeTrue())
			})
		})

		Context("when home dir cannot be created", func() {
			BeforeEach(func() {
				ioutil.WriteFile(homeDir, []byte{}, 0400)
			})

			It("returns an error", func() {
				err := env.SetupHomeDir(conf)
				Expect(err.Error()).
					To(ContainSubstring(fmt.Sprintf("failed to create cfdev home dir: path %s", homeDir)))
			})
		})

		Context("when cache dir cannot be created", func() {
			BeforeEach(func() {
				ioutil.WriteFile(cacheDir, []byte{}, 0400)
			})

			It("returns an error", func() {
				err := env.SetupHomeDir(conf)
				Expect(err.Error()).
					To(ContainSubstring(fmt.Sprintf("failed to create cache dir: path %s", cacheDir)))
			})
		})
	})
})

