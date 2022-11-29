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
 * Copyright 2022 Red Hat, Inc.
 *
 */

package create

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/api/instancetype"
	cdiv1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"sigs.k8s.io/yaml"

	"kubevirt.io/kubevirt/pkg/virtctl/templates"
)

const (
	VM = "vm"

	NameFlag                   = "name"
	RunStrategyFlag            = "run-strategy"
	TerminationGracePeriodFlag = "termination-grace-period"
	InstancetypeFlag           = "instancetype"
	PreferenceFlag             = "preference"
	DataSourceVolumeFlag       = "volume-datasource"
	ContainerdiskVolumeFlag    = "volume-containerdisk"
	ClonePvcVolumeFlag         = "volume-clone-pvc"
	PvcVolumeFlag              = "volume-pvc"
	BlankVolumeFlag            = "volume-blank"
	CloudInitUserDataFlag      = "cloud-init-user-data"
	CloudInitNetworkDataFlag   = "cloud-init-network-data"

	cloudInitDisk = "cloudinitdisk"
)

type createVM struct {
	name                   string
	terminationGracePeriod int64
	runStrategy            string
	instancetype           string
	preference             string
	dataSourceVolumes      []string
	containerdiskVolumes   []string
	clonePvcVolumes        []string
	blankVolumes           []string
	pvcVolumes             []string
	cloudInitUserData      string
	cloudInitNetworkData   string
}

type cloneVolume struct {
	Name   string             `param:"name"`
	Source string             `param:"src"`
	Size   *resource.Quantity `param:"size"`
}

type pvcVolume struct {
	Name   string `param:"name"`
	Source string `param:"src"`
}

type blankVolume struct {
	Name string             `param:"name"`
	Size *resource.Quantity `param:"size"`
}

type optionFn func(*createVM, *v1.VirtualMachine) error

var optFns = map[string]optionFn{
	RunStrategyFlag:          withRunStrategy,
	InstancetypeFlag:         withInstancetype,
	PreferenceFlag:           withPreference,
	DataSourceVolumeFlag:     withDataSourceVolume,
	ContainerdiskVolumeFlag:  withContainerdiskVolume,
	ClonePvcVolumeFlag:       withClonePvcVolume,
	PvcVolumeFlag:            withPvcVolume,
	BlankVolumeFlag:          withBlankVolume,
	CloudInitUserDataFlag:    withCloudInitData,
	CloudInitNetworkDataFlag: withCloudInitData,
}

var flags = []string{
	RunStrategyFlag,
	InstancetypeFlag,
	PreferenceFlag,
	DataSourceVolumeFlag,
	ContainerdiskVolumeFlag,
	ClonePvcVolumeFlag,
	PvcVolumeFlag,
	BlankVolumeFlag,
	CloudInitUserDataFlag,
	CloudInitNetworkDataFlag,
}

var runStrategies = []string{
	string(v1.RunStrategyAlways),
	string(v1.RunStrategyManual),
	string(v1.RunStrategyHalted),
	string(v1.RunStrategyOnce),
	string(v1.RunStrategyRerunOnFailure),
}

func NewVirtualMachineCommand() *cobra.Command {
	c := defaultCreateVM()
	cmd := &cobra.Command{
		Use:     VM,
		Short:   "Create a VirtualMachine manifest.",
		Example: c.usage(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.run(cmd)
		},
	}

	cmd.Flags().StringVar(&c.name, NameFlag, c.name, "Specify the name of the VM")
	cmd.Flags().StringVar(&c.runStrategy, RunStrategyFlag, c.runStrategy, "Specify the RunStrategy of the VM")
	cmd.Flags().Int64Var(&c.terminationGracePeriod, TerminationGracePeriodFlag, c.terminationGracePeriod, "Specify the termination grace period of the VM")

	cmd.Flags().StringVar(&c.instancetype, InstancetypeFlag, c.instancetype, "Specify the Instance Type of the VM")
	cmd.Flags().StringVar(&c.preference, PreferenceFlag, c.preference, "Specify the Preference of the VM")

	cmd.Flags().StringArrayVar(&c.blankVolumes, BlankVolumeFlag, c.dataSourceVolumes, "Specify one or more blank volumes to be used by the VM, this flag can be provided multiple times")
	cmd.Flags().StringArrayVar(&c.dataSourceVolumes, DataSourceVolumeFlag, c.dataSourceVolumes, "Specify one or more DSs to be cloned by the VM, this flag can be provided multiple times")
	cmd.Flags().StringArrayVar(&c.clonePvcVolumes, ClonePvcVolumeFlag, c.clonePvcVolumes, "Specify one or more PVCs to be cloned by the VM, this flag can be provided multiple times")
	cmd.Flags().StringArrayVar(&c.containerdiskVolumes, ContainerdiskVolumeFlag, c.containerdiskVolumes, "Specify one or more containerdisks to be cloned by the VM, this flag can be provided multiple times")
	cmd.Flags().StringArrayVar(&c.pvcVolumes, PvcVolumeFlag, c.pvcVolumes, "Specify one or more PVCs to be used by the VM, this flag can be provided multiple times")

	cmd.Flags().StringVar(&c.cloudInitUserData, CloudInitUserDataFlag, c.cloudInitUserData, "Specify the base64 encoded cloud-init user data of the VM")
	cmd.Flags().StringVar(&c.cloudInitNetworkData, CloudInitNetworkDataFlag, c.cloudInitNetworkData, "Specify the base64 encoded cloud-init network data of the VM")

	cmd.SetUsageTemplate(templates.UsageTemplate())

	return cmd
}

func defaultCreateVM() createVM {
	return createVM{
		name:                   "vm-" + rand.String(5),
		terminationGracePeriod: 180,
		runStrategy:            string(v1.RunStrategyAlways),
	}
}

func (c *createVM) run(cmd *cobra.Command) error {
	vm := newVM(c)
	for _, flag := range flags {
		if cmd.Flags().Changed(flag) {
			if err := optFns[flag](c, vm); err != nil {
				return err
			}
		}
	}

	out, err := yaml.Marshal(vm)
	if err != nil {
		return err
	}

	cmd.Print(string(out))
	return nil
}

func (c *createVM) usage() string {
	return `  # Create a manifest for a VirtualMachine with a random name:
  {{ProgramName}} create vm
    
  # Create a manifest for a running VirtualMachine with a specified name
  {{ProgramName}} create vm --name=my-vm --running=true

  # Create a manifest for a VirtualMachine with a specified name and RunStrategy Always
  {{ProgramName}} create vm --name=my-vm --run-strategy=Always
  
  # Create a manifest for a VirtualMachine with a specified VirtualMachineClusterInstancetype
  {{ProgramName}} create vm --instancetype=my-instancetype

  # Create a manifest for a VirtualMachine with a specified VirtualMachineInstancetype (namespaced)
  {{ProgramName}} create vm --instancetype=virtualmachineinstancetype/my-instancetype
  
  # Create a manifest for a VirtualMachine with a specified VirtualMachineClusterPreference
  {{ProgramName}} create vm --preference=my-preference

  # Create a manifest for a VirtualMachine with a specified VirtualMachinePreference (namespaced)
  {{ProgramName}} create vm --preference=virtualmachinepreference/my-preference

  # Create a manifest for a VirtualMachine with a cloned DataSource in namespace and specified size
  {{ProgramName}} create vm --volume-datasource=src:my-ns/my-ds,size:50Gi

  # Create a manifest for a VirtualMachine with a cloned containerdisk
  {{ProgramName}} create vm --volume-containerdisk=src:docker://my.registry/my-image:my-tag,size:50Gi

  # Create a manifest for a VirtualMachine with a specified VirtualMachineCluster{Instancetype,Preference} and cloned PVC
  {{ProgramName}} create vm --volume-clone-pvc=my-ns/my-pvc

  # Create a manifest for a VirtualMachine with a specified VirtualMachineCluster{Instancetype,Preference} and directly used PVC
  {{ProgramName}} create vm --volume-pvc=my-pvc

  # Create a manifest for a VirtualMachine with a clone DataSource and a blank disk
  {{ProgramName}} create vm --volume-datasource=src:my-ns/my-ds --volume-blank=size:50Gi

  # Create a manifest for a VirtualMachine with a specified VirtualMachineCluster{Instancetype,Preference} and cloned DataSource
  {{ProgramName}} create vm --instancetype=my-instancetype --preference=my-preference --volume-datasource=src:my-ds

  # Create a manifest for a VirtualMachine with a specified VirtualMachineCluster{Instancetype,Preference} and two cloned DataSources (flag can be provided multiple times)
  {{ProgramName}} create vm --instancetype=my-instancetype --preference=my-preference --volume-datasource=src:my-ds1 --volume-datasource=src:my-ds2

  # Create a manifest for a VirtualMachine with a specified VirtualMachineCluster{Instancetype,Preference} and directly used PVC
  {{ProgramName}} create vm --instancetype=my-instancetype --preference=my-preference --pvc=my-pvc`
}

func newVM(c *createVM) *v1.VirtualMachine {
	runStrategy := v1.VirtualMachineRunStrategy(c.runStrategy)
	return &v1.VirtualMachine{
		TypeMeta: metav1.TypeMeta{
			Kind:       v1.VirtualMachineGroupVersionKind.Kind,
			APIVersion: v1.VirtualMachineGroupVersionKind.GroupVersion().String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: c.name,
		},
		Spec: v1.VirtualMachineSpec{
			RunStrategy: &runStrategy,
			Template: &v1.VirtualMachineInstanceTemplateSpec{
				Spec: v1.VirtualMachineInstanceSpec{
					TerminationGracePeriodSeconds: &c.terminationGracePeriod,
				},
			},
		},
	}
}

func withRunStrategy(c *createVM, vm *v1.VirtualMachine) error {
	for _, runStrategy := range runStrategies {
		if runStrategy == c.runStrategy {
			vmRunStrategy := v1.VirtualMachineRunStrategy(c.runStrategy)
			vm.Spec.RunStrategy = &vmRunStrategy
			return nil
		}
	}

	return flagErr(RunStrategyFlag, "unknown RunStrategy \"%s\", supported RunStrategies are: %s", c.runStrategy, strings.Join(runStrategies, ", "))
}

func withInstancetype(c *createVM, vm *v1.VirtualMachine) error {
	kind, name, err := splitPrefixedName(c.instancetype)
	if err != nil {
		return flagErr(InstancetypeFlag, "%w", err)
	}

	if kind != "" && kind != instancetype.SingularResourceName && kind != instancetype.ClusterSingularResourceName {
		return flagErr(InstancetypeFlag, "invalid instancetype kind: %s", kind)
	}

	// If kind is empty we rely on the vm-mutator to fill in the default value VirtualMachineClusterInstancetype
	vm.Spec.Instancetype = &v1.InstancetypeMatcher{
		Name: name,
		Kind: kind,
	}

	return nil
}

func withPreference(c *createVM, vm *v1.VirtualMachine) error {
	kind, name, err := splitPrefixedName(c.preference)
	if err != nil {
		return flagErr(PreferenceFlag, "%w", err)
	}

	if kind != "" && kind != instancetype.SingularPreferenceResourceName && kind != instancetype.ClusterSingularPreferenceResourceName {
		return flagErr(PreferenceFlag, "invalid preference kind: %s", kind)
	}

	// If kind is empty we rely on the vm-mutator to fill in the default value VirtualMachineClusterPreference
	vm.Spec.Preference = &v1.PreferenceMatcher{
		Name: name,
		Kind: kind,
	}

	return nil
}

func withDataSourceVolume(c *createVM, vm *v1.VirtualMachine) error {
	for _, dataSourceVol := range c.dataSourceVolumes {
		vol := cloneVolume{}
		err := mapParams(DataSourceVolumeFlag, dataSourceVol, &vol)
		if err != nil {
			return err
		}

		if vol.Source == "" {
			return flagErr(DataSourceVolumeFlag, "src must be specified")
		}

		namespace, name, err := splitPrefixedName(vol.Source)
		if err != nil {
			return flagErr(DataSourceVolumeFlag, "src invalid: %w", err)
		}

		if vol.Name == "" {
			vol.Name = fmt.Sprintf("%s-ds-%s", vm.Name, name)
		}

		dvt := v1.DataVolumeTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name: vol.Name,
			},
			Spec: cdiv1.DataVolumeSpec{
				Storage: &cdiv1.StorageSpec{},
				SourceRef: &cdiv1.DataVolumeSourceRef{
					Kind: "DataSource",
					Name: name,
				},
			},
		}
		if namespace != "" {
			dvt.Spec.SourceRef.Namespace = &namespace
		}
		if vol.Size != nil {
			dvt.Spec.Storage.Resources.Requests = k8sv1.ResourceList{
				k8sv1.ResourceStorage: *vol.Size,
			}
		}
		vm.Spec.DataVolumeTemplates = append(vm.Spec.DataVolumeTemplates, dvt)

		vm.Spec.Template.Spec.Volumes = append(vm.Spec.Template.Spec.Volumes, v1.Volume{
			Name: vol.Name,
			VolumeSource: v1.VolumeSource{
				DataVolume: &v1.DataVolumeSource{
					Name: vol.Name,
				},
			},
		})
	}

	return nil
}

func withContainerdiskVolume(c *createVM, vm *v1.VirtualMachine) error {
	for i, containerdiskVol := range c.containerdiskVolumes {
		vol := cloneVolume{}
		err := mapParams(ContainerdiskVolumeFlag, containerdiskVol, &vol)
		if err != nil {
			return err
		}

		if vol.Source == "" {
			return flagErr(ContainerdiskVolumeFlag, "src must be specified")
		}
		if vol.Size == nil {
			return flagErr(ContainerdiskVolumeFlag, "size must be specified")
		}

		if !strings.HasPrefix(vol.Source, "docker://") {
			vol.Source = fmt.Sprintf("docker://%s", vol.Source)
		}
		if vol.Name == "" {
			vol.Name = fmt.Sprintf("%s-containerdisk-%d", vm.Name, i)
		}

		vm.Spec.DataVolumeTemplates = append(vm.Spec.DataVolumeTemplates, v1.DataVolumeTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name: vol.Name,
			},
			Spec: cdiv1.DataVolumeSpec{
				Storage: &cdiv1.StorageSpec{
					Resources: k8sv1.ResourceRequirements{
						Requests: k8sv1.ResourceList{
							k8sv1.ResourceStorage: *vol.Size,
						},
					},
				},
				Source: &cdiv1.DataVolumeSource{
					Registry: &cdiv1.DataVolumeSourceRegistry{
						URL: &vol.Source,
					},
				},
			},
		})

		vm.Spec.Template.Spec.Volumes = append(vm.Spec.Template.Spec.Volumes, v1.Volume{
			Name: vol.Name,
			VolumeSource: v1.VolumeSource{
				DataVolume: &v1.DataVolumeSource{
					Name: vol.Name,
				},
			},
		})
	}

	return nil
}

func withClonePvcVolume(c *createVM, vm *v1.VirtualMachine) error {
	for _, clonePvcVol := range c.clonePvcVolumes {
		vol := cloneVolume{}
		err := mapParams(ClonePvcVolumeFlag, clonePvcVol, &vol)
		if err != nil {
			return err
		}

		if vol.Source == "" {
			return flagErr(ClonePvcVolumeFlag, "src must be specified")
		}

		namespace, name, err := splitPrefixedName(vol.Source)
		if err != nil {
			return flagErr(ClonePvcVolumeFlag, "src invalid: %w", err)
		}
		if namespace == "" {
			return flagErr(ClonePvcVolumeFlag, "namespace of pvc '%s' must be specified", name)
		}

		if vol.Name == "" {
			vol.Name = fmt.Sprintf("%s-pvc-%s", vm.Name, name)
		}

		dvt := v1.DataVolumeTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name: vol.Name,
			},
			Spec: cdiv1.DataVolumeSpec{
				Storage: &cdiv1.StorageSpec{},
				Source: &cdiv1.DataVolumeSource{
					PVC: &cdiv1.DataVolumeSourcePVC{
						Namespace: namespace,
						Name:      name,
					},
				},
			},
		}
		if vol.Size != nil {
			dvt.Spec.Storage.Resources.Requests = k8sv1.ResourceList{
				k8sv1.ResourceStorage: *vol.Size,
			}
		}
		vm.Spec.DataVolumeTemplates = append(vm.Spec.DataVolumeTemplates, dvt)

		vm.Spec.Template.Spec.Volumes = append(vm.Spec.Template.Spec.Volumes, v1.Volume{
			Name: vol.Name,
			VolumeSource: v1.VolumeSource{
				DataVolume: &v1.DataVolumeSource{
					Name: vol.Name,
				},
			},
		})
	}

	return nil
}

func withPvcVolume(c *createVM, vm *v1.VirtualMachine) error {
	for _, pvcVol := range c.pvcVolumes {
		vol := pvcVolume{}
		err := mapParams(PvcVolumeFlag, pvcVol, &vol)
		if err != nil {
			return err
		}

		if vol.Source == "" {
			return flagErr(PvcVolumeFlag, "src must be specified")
		}

		namespace, name, err := splitPrefixedName(vol.Source)
		if err != nil {
			return flagErr(PvcVolumeFlag, "src invalid: %w", err)
		}
		if namespace != "" {
			return flagErr(PvcVolumeFlag, "not allowed to specify namespace of pvc '%s'", name)
		}

		if vol.Name == "" {
			vol.Name = name
		}

		vm.Spec.Template.Spec.Volumes = append(vm.Spec.Template.Spec.Volumes, v1.Volume{
			Name: vol.Name,
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
					PersistentVolumeClaimVolumeSource: k8sv1.PersistentVolumeClaimVolumeSource{
						ClaimName: name,
					},
				},
			},
		})
	}

	return nil
}

func withBlankVolume(c *createVM, vm *v1.VirtualMachine) error {
	for i, blankVol := range c.blankVolumes {
		vol := blankVolume{}
		err := mapParams(BlankVolumeFlag, blankVol, &vol)
		if err != nil {
			return err
		}

		if vol.Size == nil {
			return flagErr(BlankVolumeFlag, "size must be specified")
		}

		if vol.Name == "" {
			vol.Name = fmt.Sprintf("%s-blank-%d", vm.Name, i)
		}

		vm.Spec.DataVolumeTemplates = append(vm.Spec.DataVolumeTemplates, v1.DataVolumeTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name: vol.Name,
			},
			Spec: cdiv1.DataVolumeSpec{
				Storage: &cdiv1.StorageSpec{
					Resources: k8sv1.ResourceRequirements{
						Requests: k8sv1.ResourceList{
							k8sv1.ResourceStorage: *vol.Size,
						},
					},
				},
				Source: &cdiv1.DataVolumeSource{
					Blank: &cdiv1.DataVolumeBlankImage{},
				},
			},
		})

		vm.Spec.Template.Spec.Volumes = append(vm.Spec.Template.Spec.Volumes, v1.Volume{
			Name: vol.Name,
			VolumeSource: v1.VolumeSource{
				DataVolume: &v1.DataVolumeSource{
					Name: vol.Name,
				},
			},
		})
	}

	return nil
}

func withCloudInitData(c *createVM, vm *v1.VirtualMachine) error {
	// Skip if cloudInitDisk is already present
	for _, volume := range vm.Spec.Template.Spec.Volumes {
		if volume.Name == cloudInitDisk {
			return nil
		}
	}

	cloudInitNoCloud := &v1.CloudInitNoCloudSource{}
	if c.cloudInitNetworkData != "" {
		cloudInitNoCloud.NetworkDataBase64 = c.cloudInitNetworkData
	}
	if c.cloudInitUserData != "" {
		cloudInitNoCloud.UserDataBase64 = c.cloudInitUserData
	}
	vm.Spec.Template.Spec.Volumes = append(vm.Spec.Template.Spec.Volumes, v1.Volume{
		Name: cloudInitDisk,
		VolumeSource: v1.VolumeSource{
			CloudInitNoCloud: cloudInitNoCloud,
		},
	})

	return nil
}
