/*
Copyright 2021 The KubeVela Authors.

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

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/oam-dev/kubevela/apis/types"
	"github.com/oam-dev/kubevela/pkg/utils/common"
	"github.com/oam-dev/kubevela/pkg/utils/system"
	cmdutil "github.com/oam-dev/kubevela/pkg/utils/util"
	"github.com/oam-dev/kubevela/references/plugins"
)

const (
	// SideBar file name for docsify
	SideBar = "_sidebar.md"
	// NavBar file name for docsify
	NavBar = "_navbar.md"
	// IndexHTML file name for docsify
	IndexHTML = "index.html"
	// CSS file name for custom CSS
	CSS = "custom.css"
	// README file name for docsify
	README = "README.md"
)

const (
	// Port is the port for reference docs website
	Port = ":18081"
)

var webSite bool

// NewCapabilityShowCommand shows the reference doc for a workload type or trait
func NewCapabilityShowCommand(c common.Args, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "show",
		Short:   "Show the reference doc for a workload type or trait",
		Long:    "Show the reference doc for a workload type or trait",
		Example: `show webservice`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return c.SetConfig()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("please specify a workload type or trait")
			}
			ctx := context.Background()
			capabilityName := args[0]
			velaEnv, err := GetFlagEnvOrCurrent(cmd, c)
			if err != nil {
				return err
			}
			if webSite {
				return startReferenceDocsSite(ctx, velaEnv.Namespace, c, ioStreams, capabilityName)
			}
			return ShowReferenceConsole(ctx, c, ioStreams, capabilityName, velaEnv.Namespace)
		},
		Annotations: map[string]string{
			types.TagCommandType: types.TypeStart,
		},
	}

	cmd.Flags().BoolVarP(&webSite, "web", "", false, " start web doc site")
	cmd.SetOut(ioStreams.Out)
	return cmd
}

func startReferenceDocsSite(ctx context.Context, ns string, c common.Args, ioStreams cmdutil.IOStreams, capabilityName string) error {
	home, err := system.GetVelaHomeDir()
	if err != nil {
		return err
	}
	referenceHome := filepath.Join(home, "reference")

	definitionPath := filepath.Join(referenceHome, "capabilities")
	if _, err := os.Stat(definitionPath); err != nil && os.IsNotExist(err) {
		if err := os.MkdirAll(definitionPath, 0750); err != nil {
			return err
		}
	}
	docsPath := filepath.Join(referenceHome, "docs")
	if _, err := os.Stat(docsPath); err != nil && os.IsNotExist(err) {
		if err := os.MkdirAll(docsPath, 0750); err != nil {
			return err
		}
	}
	capabilities, err := plugins.GetNamespacedCapabilitiesFromCluster(ctx, ns, c, nil)
	if err != nil {
		return err
	}
	// check whether input capability is valid
	var capabilityIsValid bool
	var capabilityType types.CapType
	for _, c := range capabilities {
		if c.Name == capabilityName {
			capabilityIsValid = true
			capabilityType = c.Type
			break
		}
	}
	if !capabilityIsValid {
		return fmt.Errorf("%s is not a valid component type or trait", capabilityName)
	}

	cli, err := c.GetClient()
	if err != nil {
		return err
	}
	ref := &plugins.MarkdownReference{
		ParseReference: plugins.ParseReference{
			Client: cli,
		},
	}

	if err := ref.CreateMarkdown(ctx, capabilities, docsPath, plugins.ReferenceSourcePath); err != nil {
		return err
	}

	if err := generateSideBar(capabilities, docsPath); err != nil {
		return err
	}

	if err := generateNavBar(docsPath); err != nil {
		return err
	}

	if err := generateIndexHTML(docsPath); err != nil {
		return err
	}
	if err := generateCustomCSS(docsPath); err != nil {
		return err
	}

	if err := generateREADME(capabilities, docsPath); err != nil {
		return err
	}

	var capabilityPath string
	switch capabilityType {
	case types.TypeWorkload:
		capabilityPath = plugins.WorkloadTypePath
	case types.TypeTrait:
		capabilityPath = plugins.TraitPath
	case types.TypeScope:
	case types.TypeComponentDefinition:
		capabilityPath = plugins.ComponentDefinitionTypePath
	default:
		return fmt.Errorf("unsupported type: %v", capabilityType)
	}

	url := fmt.Sprintf("http://127.0.0.1%s/#/%s/%s", Port, capabilityPath, capabilityName)
	server := &http.Server{
		Addr:         Port,
		Handler:      http.FileServer(http.Dir(docsPath)),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	server.SetKeepAlivesEnabled(true)
	errCh := make(chan error, 1)

	launch(server, errCh)

	select {
	case err = <-errCh:
		return err
	case <-time.After(time.Second):
		if err := OpenBrowser(url); err != nil {
			ioStreams.Infof("automatically invoking browser failed: %v\nPlease visit %s for reference docs", err, url)
		}
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGTERM)

	<-sc
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

func launch(server *http.Server, errChan chan<- error) {
	go func() {
		err := server.ListenAndServe()
		if err != nil && errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()
}

func generateSideBar(capabilities []types.Capability, docsPath string) error {
	sideBar := filepath.Join(docsPath, SideBar)
	components, traits := getComponentsAndTraits(capabilities)
	f, err := os.Create(sideBar)
	if err != nil {
		return err
	}
	if _, err := f.WriteString("- Components Types\n"); err != nil {
		return err
	}
	for _, c := range components {
		if _, err := f.WriteString(fmt.Sprintf("  - [%s](%s/%s.md)\n", c, plugins.ComponentDefinitionTypePath, c)); err != nil {
			return err
		}
	}
	if _, err := f.WriteString("- Traits\n"); err != nil {
		return err
	}
	for _, t := range traits {
		if _, err := f.WriteString(fmt.Sprintf("  - [%s](%s/%s.md)\n", t, plugins.TraitPath, t)); err != nil {
			return err
		}
	}
	return nil
}

func generateNavBar(docsPath string) error {
	sideBar := filepath.Join(docsPath, NavBar)
	_, err := os.Create(sideBar)
	if err != nil {
		return err
	}
	return nil
}

func generateIndexHTML(docsPath string) error {
	indexHTML := `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>KubeVela Reference Docs</title>
  <meta http-equiv="X-UA-Compatible" content="IE=edge,chrome=1" />
  <meta name="description" content="Description">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, minimum-scale=1.0">
  <link rel="stylesheet" href="//cdn.jsdelivr.net/npm/docsify@4/lib/themes/vue.css">
  <link rel="stylesheet" href="//cdn.jsdelivr.net/npm/docsify-sidebar-collapse/dist/sidebar.min.css" />
  <link rel="stylesheet" href="./custom.css">
</head>
<body>
  <div id="app"></div>
  <script>
    window.$docsify = {
      name: 'KubeVela Reference Docs',
      loadSidebar: true,
      loadNavbar: true,
      subMaxLevel: 1,
      alias: {
        '/_sidebar.md': '/_sidebar.md',
        '/_navbar.md': '/_navbar.md'
      },
      formatUpdated: '{MM}/{DD}/{YYYY} {HH}:{mm}:{ss}',
    }
  </script>
  <!-- Docsify v4 -->
  <script src="//cdn.jsdelivr.net/npm/docsify@4"></script>
  <script src="//cdn.jsdelivr.net/npm/docsify/lib/docsify.min.js"></script>
  <!-- plugins -->
  <script src="//cdn.jsdelivr.net/npm/docsify-sidebar-collapse/dist/docsify-sidebar-collapse.min.js"></script>
</body>
</html>
`
	return os.WriteFile(filepath.Join(docsPath, IndexHTML), []byte(indexHTML), 0600)
}

func generateCustomCSS(docsPath string) error {
	css := `
body {
    overflow: auto !important;
}`
	return os.WriteFile(filepath.Join(docsPath, CSS), []byte(css), 0600)
}

func generateREADME(capabilities []types.Capability, docsPath string) error {
	readmeMD := filepath.Join(docsPath, README)
	f, err := os.Create(readmeMD)
	if err != nil {
		return err
	}
	if _, err := f.WriteString("# KubeVela Reference Docs for Component Types and Traits\n" +
		"Click the navigation bar on the left or the links below to look into the detailed referennce of a Workload type or a Trait.\n"); err != nil {
		return err
	}

	workloads, traits := getComponentsAndTraits(capabilities)

	if _, err := f.WriteString("## Component Types\n"); err != nil {
		return err
	}

	for _, w := range workloads {
		if _, err := f.WriteString(fmt.Sprintf("  - [%s](%s/%s.md)\n", w, plugins.ComponentDefinitionTypePath, w)); err != nil {
			return err
		}
	}
	if _, err := f.WriteString("## Traits\n"); err != nil {
		return err
	}

	for _, t := range traits {
		if _, err := f.WriteString(fmt.Sprintf("  - [%s](%s/%s.md)\n", t, plugins.TraitPath, t)); err != nil {
			return err
		}
	}
	return nil
}

func getComponentsAndTraits(capabilities []types.Capability) ([]string, []string) {
	var components, traits []string
	for _, c := range capabilities {
		switch c.Type {
		case types.TypeComponentDefinition:
			components = append(components, c.Name)
		case types.TypeTrait:
			traits = append(traits, c.Name)
		case types.TypeScope:
		case types.TypeWorkload:
		default:
		}
	}
	return components, traits
}

// ShowReferenceConsole will show capability reference in console
func ShowReferenceConsole(ctx context.Context, c common.Args, ioStreams cmdutil.IOStreams, capabilityName string, ns string) error {
	capability, err := plugins.GetCapabilityByName(ctx, c, capabilityName, ns)
	if err != nil {
		return err
	}

	cli, err := c.GetClient()
	if err != nil {
		return err
	}
	ref := &plugins.ConsoleReference{
		ParseReference: plugins.ParseReference{
			Client: cli,
		},
	}

	var propertyConsole []plugins.ConsoleReference
	switch capability.Category {
	case types.HelmCategory:
		_, propertyConsole, err = ref.GenerateHelmAndKubeProperties(ctx, capability)
		if err != nil {
			return err
		}
	case types.KubeCategory:
		_, propertyConsole, err = ref.GenerateHelmAndKubeProperties(ctx, capability)
		if err != nil {
			return err
		}
	case types.CUECategory:
		propertyConsole, err = ref.GenerateCUETemplateProperties(capability)
		if err != nil {
			return err
		}
	case types.TerraformCategory:
		propertyConsole, err = ref.GenerateTerraformCapabilityProperties(*capability)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupport capability category %s", capability.Category)
	}
	for _, p := range propertyConsole {
		ioStreams.Info(p.TableName)
		p.TableObject.Render()
		ioStreams.Info("\n")
	}
	return nil
}

// OpenBrowser will open browser by url in different OS system
// nolint:gosec
func OpenBrowser(url string) error {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("cmd", "/C", "start", url).Run()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	return err
}
