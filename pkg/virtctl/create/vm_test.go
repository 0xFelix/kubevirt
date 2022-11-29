package create_test

import (
	"encoding/base64"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "kubevirt.io/api/core/v1"
	instancetypeapi "kubevirt.io/api/instancetype"
	"sigs.k8s.io/yaml"

	"kubevirt.io/kubevirt/tests/clientcmd"

	. "kubevirt.io/kubevirt/pkg/virtctl/create"
)

const (
	cloudInitUserData = `#cloud-config
user: user
password: password
chpasswd: { expire: False }`

	cloudInitNetworkData = `network:
  version: 1
  config:
  - type: physical
  name: eth0
  subnets:
    - type: dhcp`
)

var _ = Describe("create vm", func() {
	Context("Manifest is created successfully", func() {
		It("VM with random name", func() {
			out, err := runCmd()
			Expect(err).ToNot(HaveOccurred())
			_ = unmarshalVM(out)
		})

		It("VM with specified name", func() {
			const name = "my-vm"
			out, err := runCmd(setFlag(NameFlag, name))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Name).To(Equal(name))
		})

		It("RunStrategy is set to Always by default", func() {
			out, err := runCmd()
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Spec.Running).To(BeNil())
			Expect(vm.Spec.RunStrategy).ToNot(BeNil())
			Expect(*vm.Spec.RunStrategy).To(Equal(v1.RunStrategyAlways))
		})

		It("VM with specified run strategy", func() {
			const runStrategy = v1.RunStrategyManual
			out, err := runCmd(setFlag(RunStrategyFlag, string(runStrategy)))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Spec.Running).To(BeNil())
			Expect(vm.Spec.RunStrategy).ToNot(BeNil())
			Expect(*vm.Spec.RunStrategy).To(Equal(runStrategy))
		})

		It("Termination grace period defaults to 180", func() {
			out, err := runCmd()
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)
			Expect(vm.Spec.Template.Spec.TerminationGracePeriodSeconds).ToNot(BeNil())
			Expect(*vm.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(180)))
		})

		It("VM with specified termination grace period", func() {
			const terminationGracePeriod int64 = 123
			out, err := runCmd(setFlag(TerminationGracePeriodFlag, fmt.Sprint(terminationGracePeriod)))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Spec.Template.Spec.TerminationGracePeriodSeconds).ToNot(BeNil())
			Expect(*vm.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(terminationGracePeriod))
		})

		DescribeTable("VM with specified instancetype", func(flag, name, kind string) {
			out, err := runCmd(setFlag(InstancetypeFlag, flag))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Spec.Instancetype.Name).To(Equal(name))
			Expect(vm.Spec.Instancetype.Kind).To(Equal(kind))
		},
			Entry("Implicit cluster-wide", "my-instancetype", "my-instancetype", ""),
			Entry("Explicit cluster-wide", "virtualmachineclusterinstancetype/my-clusterinstancetype", "my-clusterinstancetype", instancetypeapi.ClusterSingularResourceName),
			Entry("Explicit namespaced", "virtualmachineinstancetype/my-instancetype", "my-instancetype", instancetypeapi.SingularResourceName),
		)

		DescribeTable("VM with specified preference", func(flag, name, kind string) {
			out, err := runCmd(setFlag(PreferenceFlag, flag))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Spec.Preference.Name).To(Equal(name))
			Expect(vm.Spec.Preference.Kind).To(Equal(kind))
		},
			Entry("Implicit cluster-wide", "my-preference", "my-preference", ""),
			Entry("Explicit cluster-wide", "virtualmachineclusterpreference/my-clusterpreference", "my-clusterpreference", instancetypeapi.ClusterSingularPreferenceResourceName),
			Entry("Explicit namespaced", "virtualmachinepreference/my-preference", "my-preference", instancetypeapi.SingularPreferenceResourceName),
		)

		DescribeTable("VM with specified datasource", func(dsNamespace, dsName, dvtName, dvtSize, params string) {
			out, err := runCmd(setFlag(DataSourceVolumeFlag, params))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			if dvtName == "" {
				dvtName = fmt.Sprintf("%s-ds-%s", vm.Name, dsName)
			}
			Expect(vm.Spec.DataVolumeTemplates).To(HaveLen(1))
			Expect(vm.Spec.DataVolumeTemplates[0].Name).To(Equal(dvtName))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Kind).To(Equal("DataSource"))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Name).To(Equal(dsName))
			if dsNamespace != "" {
				Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Namespace).ToNot(BeNil())
				Expect(*vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Namespace).To(Equal(dsNamespace))
			} else {
				Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Namespace).To(BeNil())
			}
			if dvtSize != "" {
				Expect(vm.Spec.DataVolumeTemplates[0].Spec.Storage.Resources.Requests[k8sv1.ResourceStorage]).To(Equal(resource.MustParse(dvtSize)))
			}
			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal(dvtName))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume.Name).To(Equal(dvtName))
		},
			Entry("without namespace", "", "my-dv", "", "", "src:my-dv"),
			Entry("with namespace", "my-ns", "my-dv", "", "", "src:my-ns/my-dv"),
			Entry("without namespace and with name", "", "my-dv", "my-dvt", "", "src:my-dv,name:my-dvt"),
			Entry("with namespace and name", "my-ns", "my-dv", "my-dvt", "", "src:my-ns/my-dv,name:my-dvt"),
			Entry("without namespace and with size", "", "my-dv", "", "10Gi", "src:my-dv,size:10Gi"),
			Entry("with namespace and size", "my-ns", "my-dv", "", "10Gi", "src:my-ns/my-dv,size:10Gi"),
			Entry("without namespace and with name and size", "", "my-dv", "my-dvt", "10Gi", "src:my-dv,name:my-dvt,size:10Gi"),
			Entry("with namespace, name and size", "my-ns", "my-dv", "my-dvt", "10Gi", "src:my-ns/my-dv,name:my-dvt,size:10Gi"),
		)

		DescribeTable("VM with specified containerdisk", func(containerdisk, dvtName, dvtSize, params string) {
			out, err := runCmd(setFlag(ContainerdiskVolumeFlag, params))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			if dvtName == "" {
				dvtName = fmt.Sprintf("%s-containerdisk-0", vm.Name)
			}
			Expect(vm.Spec.DataVolumeTemplates).To(HaveLen(1))
			Expect(vm.Spec.DataVolumeTemplates[0].Name).To(Equal(dvtName))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source.Registry).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source.Registry.URL).ToNot(BeNil())
			Expect(*vm.Spec.DataVolumeTemplates[0].Spec.Source.Registry.URL).To(Equal(containerdisk))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Storage.Resources.Requests[k8sv1.ResourceStorage]).To(Equal(resource.MustParse(dvtSize)))
			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal(dvtName))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume.Name).To(Equal(dvtName))
		},
			Entry("src without prefix", "docker://my.registry/my-image:my-tag", "", "10Gi", "src:my.registry/my-image:my-tag,size:10Gi"),
			Entry("src with prefix", "docker://my.registry/my-image:my-tag", "", "10Gi", "src:docker://my.registry/my-image:my-tag,size:10Gi"),
			Entry("src without prefix and with name", "docker://my.registry/my-image:my-tag", "my-dvt", "10Gi", "src:my.registry/my-image:my-tag,name:my-dvt,size:10Gi"),
			Entry("src with prefix and with name", "docker://my.registry/my-image:my-tag", "my-dvt", "10Gi", "src:docker://my.registry/my-image:my-tag,name:my-dvt,size:10Gi"),
		)

		DescribeTable("VM with specified clone pvc", func(pvcNamespace, pvcName, dvtName, dvtSize, params string) {
			out, err := runCmd(setFlag(ClonePvcVolumeFlag, params))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			if dvtName == "" {
				dvtName = fmt.Sprintf("%s-pvc-%s", vm.Name, pvcName)
			}
			Expect(vm.Spec.DataVolumeTemplates).To(HaveLen(1))
			Expect(vm.Spec.DataVolumeTemplates[0].Name).To(Equal(dvtName))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source.PVC).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source.PVC.Namespace).To(Equal(pvcNamespace))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source.PVC.Name).To(Equal(pvcName))
			if dvtSize != "" {
				Expect(vm.Spec.DataVolumeTemplates[0].Spec.Storage.Resources.Requests[k8sv1.ResourceStorage]).To(Equal(resource.MustParse(dvtSize)))
			}
			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal(dvtName))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume.Name).To(Equal(dvtName))
		},
			Entry("with src", "my-ns", "my-pvc", "", "", "src:my-ns/my-pvc"),
			Entry("with src and name", "my-ns", "my-pvc", "my-dvt", "", "src:my-ns/my-pvc,name:my-dvt"),
			Entry("with src and size", "my-ns", "my-pvc", "", "10Gi", "src:my-ns/my-pvc,size:10Gi"),
			Entry("with src, name and size", "my-ns", "my-pvc", "my-dvt", "10Gi", "src:my-ns/my-pvc,name:my-dvt,size:10Gi"),
		)

		DescribeTable("VM with specified pvc", func(pvcName, volName, params string) {
			out, err := runCmd(setFlag(PvcVolumeFlag, params))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			if volName == "" {
				volName = pvcName
			}
			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal(volName))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(pvcName))
		},
			Entry("with src", "my-pvc", "", "src:my-pvc"),
			Entry("with src and name", "my-pvc", "my-direct-pvc", "src:my-pvc,name:my-direct-pvc"),
		)

		DescribeTable("VM with blank disk", func(blankName, blankSize, params string) {
			out, err := runCmd(setFlag(BlankVolumeFlag, params))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			if blankName == "" {
				blankName = fmt.Sprintf("%s-blank-0", vm.Name)
			}
			Expect(vm.Spec.DataVolumeTemplates).To(HaveLen(1))
			Expect(vm.Spec.DataVolumeTemplates[0].Name).To(Equal(blankName))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Source.Blank).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Storage.Resources.Requests[k8sv1.ResourceStorage]).To(Equal(resource.MustParse(blankSize)))
			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal(blankName))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume.Name).To(Equal(blankName))
		},
			Entry("with size", "", "10Gi", "size:10Gi"),
			Entry("with size and name", "my-blank", "10Gi", "size:10Gi,name:my-blank"),
		)

		It("VM with specified cloud-init user data", func() {
			userDataB64 := base64.StdEncoding.EncodeToString([]byte(cloudInitUserData))
			out, err := runCmd(setFlag(CloudInitUserDataFlag, userDataB64))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal("cloudinitdisk"))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud.UserDataBase64).To(Equal(userDataB64))

			decoded, err := base64.StdEncoding.DecodeString(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud.UserDataBase64)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(decoded)).To(Equal(cloudInitUserData))
		})

		It("VM with specified cloud-init network data", func() {
			networkDataB64 := base64.StdEncoding.EncodeToString([]byte(cloudInitNetworkData))
			out, err := runCmd(setFlag(CloudInitNetworkDataFlag, networkDataB64))
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal("cloudinitdisk"))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud.NetworkDataBase64).To(Equal(networkDataB64))

			decoded, err := base64.StdEncoding.DecodeString(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud.NetworkDataBase64)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(decoded)).To(Equal(cloudInitNetworkData))
		})

		It("VM with specified cloud-init user and network data", func() {
			userDataB64 := base64.StdEncoding.EncodeToString([]byte(cloudInitUserData))
			networkDataB64 := base64.StdEncoding.EncodeToString([]byte(cloudInitNetworkData))
			out, err := runCmd(
				setFlag(CloudInitUserDataFlag, userDataB64),
				setFlag(CloudInitNetworkDataFlag, networkDataB64),
			)
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal("cloudinitdisk"))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud.UserDataBase64).To(Equal(userDataB64))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud.NetworkDataBase64).To(Equal(networkDataB64))

			decoded, err := base64.StdEncoding.DecodeString(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud.UserDataBase64)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(decoded)).To(Equal(cloudInitUserData))
			decoded, err = base64.StdEncoding.DecodeString(vm.Spec.Template.Spec.Volumes[0].VolumeSource.CloudInitNoCloud.NetworkDataBase64)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(decoded)).To(Equal(cloudInitNetworkData))
		})

		It("Complex example", func() {
			const vmName = "my-vm"
			const runStrategy = v1.RunStrategyManual
			const terminationGracePeriod int64 = 123
			const instancetypeKind = "virtualmachineinstancetype"
			const instancetypeName = "my-instancetype"
			const preferenceName = "my-preference"
			const dsNamespace = "my-ns"
			const dsName = "my-ds"
			const dvtSize = "10Gi"
			const pvcName = "my-pvc"
			userDataB64 := base64.StdEncoding.EncodeToString([]byte(cloudInitUserData))

			out, err := runCmd(
				setFlag(NameFlag, vmName),
				setFlag(RunStrategyFlag, string(runStrategy)),
				setFlag(TerminationGracePeriodFlag, fmt.Sprint(terminationGracePeriod)),
				setFlag(InstancetypeFlag, fmt.Sprintf("%s/%s", instancetypeKind, instancetypeName)),
				setFlag(PreferenceFlag, preferenceName),
				setFlag(DataSourceVolumeFlag, fmt.Sprintf("src:%s/%s,size:%s", dsNamespace, dsName, dvtSize)),
				setFlag(PvcVolumeFlag, fmt.Sprintf("src:%s", pvcName)),
				setFlag(CloudInitUserDataFlag, userDataB64),
			)
			Expect(err).ToNot(HaveOccurred())
			vm := unmarshalVM(out)

			Expect(vm.Name).To(Equal(vmName))

			Expect(vm.Spec.Running).To(BeNil())
			Expect(vm.Spec.RunStrategy).ToNot(BeNil())
			Expect(*vm.Spec.RunStrategy).To(Equal(runStrategy))

			Expect(vm.Spec.Template.Spec.TerminationGracePeriodSeconds).ToNot(BeNil())
			Expect(*vm.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(terminationGracePeriod))

			Expect(vm.Spec.Instancetype).ToNot(BeNil())
			Expect(vm.Spec.Instancetype.Kind).To(Equal(instancetypeKind))
			Expect(vm.Spec.Instancetype.Name).To(Equal(instancetypeName))

			Expect(vm.Spec.Preference).ToNot(BeNil())
			Expect(vm.Spec.Preference.Kind).To(BeEmpty())
			Expect(vm.Spec.Preference.Name).To(Equal(preferenceName))

			dvtDsName := fmt.Sprintf("%s-ds-%s", vmName, dsName)
			Expect(vm.Spec.DataVolumeTemplates).To(HaveLen(1))
			Expect(vm.Spec.DataVolumeTemplates[0].Name).To(Equal(dvtDsName))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Kind).To(Equal("DataSource"))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Namespace).ToNot(BeNil())
			Expect(*vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Namespace).To(Equal(dsNamespace))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Name).To(Equal(dsName))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.Storage.Resources.Requests[k8sv1.ResourceStorage]).To(Equal(resource.MustParse(dvtSize)))

			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(3))
			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal(dvtDsName))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume.Name).To(Equal(dvtDsName))
			Expect(vm.Spec.Template.Spec.Volumes[1].Name).To(Equal(pvcName))
			Expect(vm.Spec.Template.Spec.Volumes[1].VolumeSource.PersistentVolumeClaim).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[1].VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(pvcName))
			Expect(vm.Spec.Template.Spec.Volumes[2].Name).To(Equal("cloudinitdisk"))
			Expect(vm.Spec.Template.Spec.Volumes[2].VolumeSource.CloudInitNoCloud).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[2].VolumeSource.CloudInitNoCloud.UserDataBase64).To(Equal(userDataB64))

			decoded, err := base64.StdEncoding.DecodeString(vm.Spec.Template.Spec.Volumes[2].VolumeSource.CloudInitNoCloud.UserDataBase64)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(decoded)).To(Equal(cloudInitUserData))
		})
	})

	Describe("Manifest is not created successfully", func() {
		DescribeTable("Invalid values for RunStrategy", func(runStrategy string) {
			out, err := runCmd(setFlag(RunStrategyFlag, runStrategy))

			Expect(err).To(MatchError(fmt.Sprintf("invalid argument for \"--run-strategy\" flag: unknown RunStrategy \"%s\", supported RunStrategies are: Always, Manual, Halted, Once, RerunOnFailure", runStrategy)))
			Expect(out).To(BeEmpty())
		},
			Entry("some string", "not-a-bool"),
			Entry("float", "1.23"),
			Entry("bool", "true"),
		)

		DescribeTable("Invalid values for TerminationGracePeriodFlag", func(terminationGracePeriod string) {
			out, err := runCmd(setFlag(TerminationGracePeriodFlag, terminationGracePeriod))

			Expect(err).To(MatchError(fmt.Sprintf("invalid argument \"%s\" for \"--termination-grace-period\" flag: strconv.ParseInt: parsing \"%s\": invalid syntax", terminationGracePeriod, terminationGracePeriod)))
			Expect(out).To(BeEmpty())
		},
			Entry("string", "not-a-number"),
			Entry("float", "1.23"),
		)

		DescribeTable("Invalid arguments to InstancetypeFlag", func(flag, errMsg string) {
			out, err := runCmd(setFlag(InstancetypeFlag, flag))

			Expect(err).To(MatchError(errMsg))
			Expect(out).To(BeEmpty())
		},
			Entry("Invalid kind", "madethisup/my-instancetype", "invalid argument for \"--instancetype\" flag: invalid instancetype kind: madethisup"),
			Entry("Invalid argument count", "virtualmachineinstancetype/my-instancetype/madethisup", "invalid argument for \"--instancetype\" flag: invalid count 3 of slashes in prefix/name"),
			Entry("Empty name", "virtualmachineinstancetype/", "invalid argument for \"--instancetype\" flag: name cannot be empty"),
		)

		DescribeTable("Invalid arguments to PreferenceFlag", func(flag, errMsg string) {
			out, err := runCmd(setFlag(PreferenceFlag, flag))

			Expect(err).To(MatchError(errMsg))
			Expect(out).To(BeEmpty())
		},
			Entry("Invalid kind", "madethisup/my-preference", "invalid argument for \"--preference\" flag: invalid preference kind: madethisup"),
			Entry("Invalid argument count", "virtualmachinepreference/my-preference/madethisup", "invalid argument for \"--preference\" flag: invalid count 3 of slashes in prefix/name"),
			Entry("Empty name", "virtualmachinepreference/", "invalid argument for \"--preference\" flag: name cannot be empty"),
		)

		DescribeTable("Invalid arguments to DataSourceVolumeFlag", func(flag, errMsg string) {
			out, err := runCmd(setFlag(DataSourceVolumeFlag, flag))

			Expect(err).To(MatchError(errMsg))
			Expect(out).To(BeEmpty())
		},
			Entry("Empty params", "", "invalid argument for \"--volume-datasource\" flag: params may not be empty"),
			Entry("Invalid param", "test=test", "invalid argument for \"--volume-datasource\" flag: params need to have at least one colon: test=test"),
			Entry("Unknown param", "test:test", "invalid argument for \"--volume-datasource\" flag: unknown param(s): test:test"),
			Entry("Missing src", "name:test", "invalid argument for \"--volume-datasource\" flag: src must be specified"),
			Entry("Empty name in src", "src:my-ns/", "invalid argument for \"--volume-datasource\" flag: src invalid: name cannot be empty"),
			Entry("Invalid slashes count in src", "src:my-ns/my-ds/madethisup", "invalid argument for \"--volume-datasource\" flag: src invalid: invalid count 3 of slashes in prefix/name"),
			Entry("Invalid quantity in size", "size:10Gu", "invalid argument for \"--volume-datasource\" flag: failed to parse param \"size\": unable to parse quantity's suffix"),
		)

		DescribeTable("Invalid arguments to ContainerdiskVolumeFlag", func(flag, errMsg string) {
			out, err := runCmd(setFlag(ContainerdiskVolumeFlag, flag))

			Expect(err).To(MatchError(errMsg))
			Expect(out).To(BeEmpty())
		},
			Entry("Empty params", "", "invalid argument for \"--volume-containerdisk\" flag: params may not be empty"),
			Entry("Invalid param", "test=test", "invalid argument for \"--volume-containerdisk\" flag: params need to have at least one colon: test=test"),
			Entry("Unknown param", "test:test", "invalid argument for \"--volume-containerdisk\" flag: unknown param(s): test:test"),
			Entry("Missing src", "name:test", "invalid argument for \"--volume-containerdisk\" flag: src must be specified"),
			Entry("Missing size", "src:my.registry/my-image:my-tag", "invalid argument for \"--volume-containerdisk\" flag: size must be specified"),
			Entry("Invalid quantity in size", "size:10Gu", "invalid argument for \"--volume-containerdisk\" flag: failed to parse param \"size\": unable to parse quantity's suffix"),
		)

		DescribeTable("Invalid arguments to ClonePvcVolumeFlag", func(flag, errMsg string) {
			out, err := runCmd(setFlag(ClonePvcVolumeFlag, flag))

			Expect(err).To(MatchError(errMsg))
			Expect(out).To(BeEmpty())
		},
			Entry("Empty params", "", "invalid argument for \"--volume-clone-pvc\" flag: params may not be empty"),
			Entry("Invalid param", "test=test", "invalid argument for \"--volume-clone-pvc\" flag: params need to have at least one colon: test=test"),
			Entry("Unknown param", "test:test", "invalid argument for \"--volume-clone-pvc\" flag: unknown param(s): test:test"),
			Entry("Missing src", "name:test", "invalid argument for \"--volume-clone-pvc\" flag: src must be specified"),
			Entry("Empty name in src", "src:my-ns/", "invalid argument for \"--volume-clone-pvc\" flag: src invalid: name cannot be empty"),
			Entry("Invalid slashes count in src", "src:my-ns/my-pvc/madethisup", "invalid argument for \"--volume-clone-pvc\" flag: src invalid: invalid count 3 of slashes in prefix/name"),
			Entry("Missing namespace in src", "src:my-pvc", "invalid argument for \"--volume-clone-pvc\" flag: namespace of pvc 'my-pvc' must be specified"),
			Entry("Invalid quantity in size", "size:10Gu", "invalid argument for \"--volume-clone-pvc\" flag: failed to parse param \"size\": unable to parse quantity's suffix"),
		)

		DescribeTable("Invalid arguments to PvcVolumeFlag", func(flag, errMsg string) {
			out, err := runCmd(setFlag(PvcVolumeFlag, flag))

			Expect(err).To(MatchError(errMsg))
			Expect(out).To(BeEmpty())
		},
			Entry("Empty params", "", "invalid argument for \"--volume-pvc\" flag: params may not be empty"),
			Entry("Invalid param", "test=test", "invalid argument for \"--volume-pvc\" flag: params need to have at least one colon: test=test"),
			Entry("Unknown param", "test:test", "invalid argument for \"--volume-pvc\" flag: unknown param(s): test:test"),
			Entry("Missing src", "name:test", "invalid argument for \"--volume-pvc\" flag: src must be specified"),
			Entry("Empty name in src", "src:my-ns/", "invalid argument for \"--volume-pvc\" flag: src invalid: name cannot be empty"),
			Entry("Invalid slashes count in src", "src:my-ns/my-pvc/madethisup", "invalid argument for \"--volume-pvc\" flag: src invalid: invalid count 3 of slashes in prefix/name"),
			Entry("Namespace in src", "src:my-ns/my-pvc", "invalid argument for \"--volume-pvc\" flag: not allowed to specify namespace of pvc 'my-pvc'"),
		)

		DescribeTable("Invalid arguments to BlankVolumeFlag", func(flag, errMsg string) {
			out, err := runCmd(setFlag(BlankVolumeFlag, flag))

			Expect(err).To(MatchError(errMsg))
			Expect(out).To(BeEmpty())
		},
			Entry("Empty params", "", "invalid argument for \"--volume-blank\" flag: params may not be empty"),
			Entry("Invalid param", "test=test", "invalid argument for \"--volume-blank\" flag: params need to have at least one colon: test=test"),
			Entry("Unknown param", "test:test", "invalid argument for \"--volume-blank\" flag: unknown param(s): test:test"),
			Entry("Missing size", "name:my-blank", "invalid argument for \"--volume-blank\" flag: size must be specified"),
		)
	})
})

func setFlag(flag, parameter string) string {
	return fmt.Sprintf("--%s=%s", flag, parameter)
}

func runCmd(args ...string) ([]byte, error) {
	_args := append([]string{CREATE, VM}, args...)
	return clientcmd.NewRepeatableVirtctlCommandWithOut(_args...)()
}

func unmarshalVM(bytes []byte) *v1.VirtualMachine {
	vm := &v1.VirtualMachine{}
	Expect(yaml.Unmarshal(bytes, vm)).To(Succeed())
	Expect(vm.Kind).To(Equal("VirtualMachine"))
	Expect(vm.APIVersion).To(Equal("kubevirt.io/v1"))
	return vm
}
