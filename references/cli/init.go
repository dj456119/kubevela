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
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"cuelang.org/go/cue"
	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/oam-dev/kubevela/apis/types"
	common2 "github.com/oam-dev/kubevela/pkg/utils/common"
	"github.com/oam-dev/kubevela/pkg/utils/env"
	cmdutil "github.com/oam-dev/kubevela/pkg/utils/util"
	"github.com/oam-dev/kubevela/references/appfile"
	"github.com/oam-dev/kubevela/references/appfile/api"
	"github.com/oam-dev/kubevela/references/common"
	"github.com/oam-dev/kubevela/references/plugins"
)

type appInitOptions struct {
	client client.Client
	cmdutil.IOStreams
	Env *types.EnvMeta
	c   common2.Args

	app          *api.Application
	appName      string
	workloadName string
	workloadType string
	renderOnly   bool
}

// NewInitCommand creates `init` command
func NewInitCommand(c common2.Args, ioStreams cmdutil.IOStreams) *cobra.Command {
	o := &appInitOptions{IOStreams: ioStreams, c: c}
	cmd := &cobra.Command{
		Use:                   "init",
		DisableFlagsInUseLine: true,
		Short:                 "Create scaffold for an application",
		Long:                  "Create scaffold for an application",
		Example:               "vela init",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return c.SetConfig()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			newClient, err := c.GetClient()
			if err != nil {
				return err
			}
			o.client = newClient
			o.Env, err = GetFlagEnvOrCurrent(cmd, c)
			if err != nil {
				return err
			}
			o.IOStreams.Info("Welcome to use KubeVela CLI! Please describe your application.")
			o.IOStreams.Info()
			if err = o.CheckEnv(); err != nil {
				return err
			}
			if err = o.Naming(); err != nil {
				return err
			}
			if err = o.Workload(); err != nil {
				return err
			}

			if err := appfile.Validate(o.app); err != nil {
				return err
			}

			b, err := yaml.Marshal(o.app.AppFile)
			if err != nil {
				return err
			}
			err = os.WriteFile("./vela.yaml", b, 0600)
			if err != nil {
				return err
			}
			o.IOStreams.Info("\nDeployment config is rendered and written to " + color.New(color.FgCyan).Sprint("vela.yaml"))

			if o.renderOnly {
				return nil
			}

			ctx := context.Background()
			err = common.BuildRun(ctx, o.app, o.client, o.Env, o.IOStreams)
			if err != nil {
				return err
			}
			deployStatus, err := printTrackingDeployStatus(c, o.IOStreams, o.appName, o.Env)
			if err != nil {
				return err
			}
			if deployStatus != compStatusDeployed {
				return nil
			}
			return printAppStatus(context.Background(), newClient, ioStreams, o.appName, o.Env, cmd, c)
		},
		Annotations: map[string]string{
			types.TagCommandType: types.TypeStart,
		},
	}
	cmd.Flags().BoolVar(&o.renderOnly, "render-only", false, "Rendering vela.yaml in current dir and do not deploy")
	cmd.SetOut(ioStreams.Out)
	return cmd
}

// Naming asks user to input app name
func (o *appInitOptions) Naming() error {
	prompt := &survey.Input{
		Message: "What would you like to name your application (required): ",
	}
	err := survey.AskOne(prompt, &o.appName, survey.WithValidator(survey.Required))
	if err != nil {
		return fmt.Errorf("read app name err %w", err)
	}
	return nil
}

// CheckEnv checks environment, e.g., domain and email.
func (o *appInitOptions) CheckEnv() error {
	if o.Env.Namespace == "" {
		o.Env.Namespace = "default"
	}
	if err := env.CreateEnv(o.Env.Name, o.Env); err != nil {
		return errors.Wrap(err, "app init create namespace err")
	}
	return nil
}

func formatAndGetUsage(p *types.Parameter) string {
	usage := p.Usage
	if usage == "" {
		usage = "what would you configure for parameter '" + color.New(color.FgCyan).Sprintf("%s", p.Name) + "'"
	}
	if p.Required {
		usage += " (required): "
	} else {
		defaultValue := fmt.Sprintf("%v", p.Default)
		if defaultValue != "" {
			usage += fmt.Sprintf(" (optional, default is %s): ", defaultValue)
		} else {
			usage += " (optional): "
		}
		if val, ok := p.Default.(json.Number); ok {
			if p.Type == cue.NumberKind || p.Type == cue.FloatKind {
				p.Default, _ = val.Float64()
			}
			if p.Type == cue.IntKind {
				p.Default, _ = val.Int64()
			}
		}
	}
	return usage
}

// Workload asks user to choose workload type from installed workloads
func (o *appInitOptions) Workload() error {
	workloads, err := plugins.LoadInstalledCapabilityWithType(o.Env.Namespace, o.c, types.TypeComponentDefinition)
	if err != nil {
		return err
	}
	var workloadList []string
	for _, w := range workloads {
		workloadList = append(workloadList, w.Name)
	}
	prompt := &survey.Select{
		Message: "Choose the workload type for your application (required, e.g., webservice): ",
		Options: workloadList,
	}
	err = survey.AskOne(prompt, &o.workloadType, survey.WithValidator(survey.Required))
	if err != nil {
		return fmt.Errorf("read workload type err %w", err)
	}
	workload, err := GetCapabilityByName(o.workloadType, workloads)
	if err != nil {
		return err
	}
	namePrompt := &survey.Input{
		Message: fmt.Sprintf("What would you like to name this %s (required): ", o.workloadType),
	}
	err = survey.AskOne(namePrompt, &o.workloadName, survey.WithValidator(survey.Required))
	if err != nil {
		return fmt.Errorf("read workload name err %w", err)
	}
	fs := pflag.NewFlagSet("workload", pflag.ContinueOnError)
	for _, pp := range workload.Parameters {
		p := pp
		if p.Name == "name" {
			continue
		}
		usage := formatAndGetUsage(&p)
		// nolint:exhaustive
		switch p.Type {
		case cue.StringKind:
			var data string
			prompt := &survey.Input{
				Message: usage,
			}
			var opts []survey.AskOpt
			if p.Required {
				opts = append(opts, survey.WithValidator(survey.Required))
			}
			err = survey.AskOne(prompt, &data, opts...)
			if err != nil {
				return fmt.Errorf("read param %s err %w", p.Name, err)
			}
			fs.String(p.Name, data, p.Usage)
		case cue.NumberKind, cue.FloatKind:
			var data string
			prompt := &survey.Input{
				Message: usage,
			}
			var opts []survey.AskOpt
			if p.Required {
				opts = append(opts, survey.WithValidator(survey.Required))
			}
			opts = append(opts, survey.WithValidator(func(ans interface{}) error {
				data := ans.(string)
				if data == "" && !p.Required {
					return nil
				}
				_, err := strconv.ParseFloat(data, 64)
				return err
			}))
			err = survey.AskOne(prompt, &data, opts...)
			if err != nil {
				return fmt.Errorf("read param %s err %w", p.Name, err)
			}
			if data == "" {
				fs.Float64(p.Name, p.Default.(float64), p.Usage)
			} else {
				val, _ := strconv.ParseFloat(data, 64)
				fs.Float64(p.Name, val, p.Usage)
			}
		case cue.IntKind:
			var data string
			prompt := &survey.Input{
				Message: usage,
			}
			var opts []survey.AskOpt
			if p.Required {
				opts = append(opts, survey.WithValidator(survey.Required))
			}
			opts = append(opts, survey.WithValidator(func(ans interface{}) error {
				data := ans.(string)
				if data == "" && !p.Required {
					return nil
				}
				_, err := strconv.ParseInt(data, 10, 64)
				return err
			}))
			err = survey.AskOne(prompt, &data, opts...)
			if err != nil {
				return fmt.Errorf("read param %s err %w", p.Name, err)
			}
			if data == "" {
				fs.Int64(p.Name, p.Default.(int64), p.Usage)
			} else {
				val, _ := strconv.ParseInt(data, 10, 64)
				fs.Int64(p.Name, val, p.Usage)
			}
		case cue.BoolKind:
			var data bool
			prompt := &survey.Confirm{
				Message: usage,
			}
			if p.Required {
				err = survey.AskOne(prompt, &data, survey.WithValidator(survey.Required))
			} else {
				err = survey.AskOne(prompt, &data)
			}
			if err != nil {
				return fmt.Errorf("read param %s err %w", p.Name, err)
			}
			fs.Bool(p.Name, data, p.Usage)
		default:
			// other type not supported
		}
	}
	o.app, err = common.BaseComplete(o.Env, o.c, o.workloadName, o.appName, fs, o.workloadType)
	return err
}

// GetCapabilityByName get eponymous types.Capability from workloads by name
func GetCapabilityByName(name string, workloads []types.Capability) (types.Capability, error) {
	for _, v := range workloads {
		if v.Name == name {
			return v, nil
		}
	}
	return types.Capability{}, fmt.Errorf("%s not found", name)
}
