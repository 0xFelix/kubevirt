package virtctl

import (
	"context"
	"encoding/base64"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	v1 "kubevirt.io/api/core/v1"
	apiinstancetype "kubevirt.io/api/instancetype"
	instancetypev1alpha2 "kubevirt.io/api/instancetype/v1alpha2"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"sigs.k8s.io/yaml"

	"kubevirt.io/kubevirt/tests/clientcmd"
	"kubevirt.io/kubevirt/tests/libstorage"
	"kubevirt.io/kubevirt/tests/util"

	. "kubevirt.io/kubevirt/pkg/virtctl/create"
)

const cloudInitUserData = `#cloud-config
user: user
password: password
chpasswd: { expire: False }`

var _ = Describe("[sig-compute][virtctl]create vm", func() {
	var virtClient kubecli.KubevirtClient

	BeforeEach(func() {
		var err error
		virtClient, err = kubecli.GetKubevirtClient()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("should create a valid VM manifest", func() {
		It("VM with random name and default settings", func() {
			out, err := runCmd()
			Expect(err).ToNot(HaveOccurred())
			vm, err := virtClient.VirtualMachine(util.NamespaceTestDefault).Create(unmarshalVM(out))
			Expect(err).ToNot(HaveOccurred())

			Expect(vm.Name).ToNot(BeEmpty())
			Expect(vm.Spec.Template.Spec.TerminationGracePeriodSeconds).ToNot(BeNil())
			Expect(*vm.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(int64(180)))
			Expect(vm.Spec.Running).To(BeNil())
			Expect(vm.Spec.RunStrategy).ToNot(BeNil())
			Expect(*vm.Spec.RunStrategy).To(Equal(v1.RunStrategyAlways))
		})

		It("Complex example", func() {
			const runStrategy = v1.RunStrategyManual
			const terminationGracePeriod int64 = 123
			const cdSource = "docker://my.registry/my-image:my-tag"
			const cdSize = "5Gi"
			const blankSize = "10Gi"
			vmName := "vm-" + rand.String(5)
			instancetype := createInstancetype(virtClient)
			preference := createPreference(virtClient)
			dataSource := createDataSource(virtClient)
			pvc := libstorage.CreateFSPVC("vm-pvc-"+rand.String(5), util.NamespaceTestDefault, "128M")
			userDataB64 := base64.StdEncoding.EncodeToString([]byte(cloudInitUserData))

			out, err := runCmd(
				setFlag(NameFlag, vmName),
				setFlag(RunStrategyFlag, string(runStrategy)),
				setFlag(TerminationGracePeriodFlag, fmt.Sprint(terminationGracePeriod)),
				setFlag(InstancetypeFlag, fmt.Sprintf("%s/%s", apiinstancetype.SingularResourceName, instancetype.Name)),
				setFlag(PreferenceFlag, fmt.Sprintf("%s/%s", apiinstancetype.SingularPreferenceResourceName, preference.Name)),
				setFlag(DataSourceVolumeFlag, fmt.Sprintf("src:%s/%s", dataSource.Namespace, dataSource.Name)),
				setFlag(ContainerdiskVolumeFlag, fmt.Sprintf("src:%s,size:%s", cdSource, cdSize)),
				setFlag(ClonePvcVolumeFlag, fmt.Sprintf("src:%s/%s", pvc.Namespace, pvc.Name)),
				setFlag(PvcVolumeFlag, fmt.Sprintf("src:%s", pvc.Name)),
				setFlag(BlankVolumeFlag, fmt.Sprintf("size:%s", blankSize)),
				setFlag(CloudInitUserDataFlag, userDataB64),
			)
			Expect(err).ToNot(HaveOccurred())
			vm, err := virtClient.VirtualMachine(util.NamespaceTestDefault).Create(unmarshalVM(out))
			Expect(err).ToNot(HaveOccurred())

			Expect(vm.Name).To(Equal(vmName))

			Expect(vm.Spec.Running).To(BeNil())
			Expect(vm.Spec.RunStrategy).ToNot(BeNil())
			Expect(*vm.Spec.RunStrategy).To(Equal(runStrategy))

			Expect(vm.Spec.Template.Spec.TerminationGracePeriodSeconds).ToNot(BeNil())
			Expect(*vm.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(terminationGracePeriod))

			Expect(vm.Spec.Instancetype).ToNot(BeNil())
			Expect(vm.Spec.Instancetype.Kind).To(Equal(apiinstancetype.SingularResourceName))
			Expect(vm.Spec.Instancetype.Name).To(Equal(instancetype.Name))

			Expect(vm.Spec.Preference).ToNot(BeNil())
			Expect(vm.Spec.Preference.Kind).To(Equal(apiinstancetype.SingularPreferenceResourceName))
			Expect(vm.Spec.Preference.Name).To(Equal(preference.Name))

			Expect(vm.Spec.DataVolumeTemplates).To(HaveLen(4))

			dvtDsName := fmt.Sprintf("%s-ds-%s", vmName, dataSource.Name)
			Expect(vm.Spec.DataVolumeTemplates[0].Name).To(Equal(dvtDsName))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Kind).To(Equal("DataSource"))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Namespace).ToNot(BeNil())
			Expect(*vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Namespace).To(Equal(dataSource.Namespace))
			Expect(vm.Spec.DataVolumeTemplates[0].Spec.SourceRef.Name).To(Equal(dataSource.Name))

			dvtCdName := fmt.Sprintf("%s-containerdisk-0", vm.Name)
			Expect(vm.Spec.DataVolumeTemplates[1].Name).To(Equal(dvtCdName))
			Expect(vm.Spec.DataVolumeTemplates[1].Spec.Source).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[1].Spec.Source.Registry).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[1].Spec.Source.Registry.URL).ToNot(BeNil())
			Expect(*vm.Spec.DataVolumeTemplates[1].Spec.Source.Registry.URL).To(Equal(cdSource))
			Expect(vm.Spec.DataVolumeTemplates[1].Spec.Storage.Resources.Requests[k8sv1.ResourceStorage]).To(Equal(resource.MustParse(cdSize)))

			dvtPvcName := fmt.Sprintf("%s-pvc-%s", vmName, pvc.Name)
			Expect(vm.Spec.DataVolumeTemplates[2].Name).To(Equal(dvtPvcName))
			Expect(vm.Spec.DataVolumeTemplates[2].Spec.Source).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[2].Spec.Source.PVC).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[2].Spec.Source.PVC.Namespace).To(Equal(pvc.Namespace))
			Expect(vm.Spec.DataVolumeTemplates[2].Spec.Source.PVC.Name).To(Equal(pvc.Name))

			dvtBlankName := fmt.Sprintf("%s-blank-0", vmName)
			Expect(vm.Spec.DataVolumeTemplates[3].Name).To(Equal(dvtBlankName))
			Expect(vm.Spec.DataVolumeTemplates[3].Spec.Source).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[3].Spec.Source.Blank).ToNot(BeNil())
			Expect(vm.Spec.DataVolumeTemplates[3].Spec.Storage.Resources.Requests[k8sv1.ResourceStorage]).To(Equal(resource.MustParse(blankSize)))

			Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(6))

			Expect(vm.Spec.Template.Spec.Volumes[0].Name).To(Equal(dvtDsName))
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[0].VolumeSource.DataVolume.Name).To(Equal(dvtDsName))

			Expect(vm.Spec.Template.Spec.Volumes[1].Name).To(Equal(dvtCdName))
			Expect(vm.Spec.Template.Spec.Volumes[1].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[1].VolumeSource.DataVolume.Name).To(Equal(dvtCdName))

			Expect(vm.Spec.Template.Spec.Volumes[2].Name).To(Equal(dvtPvcName))
			Expect(vm.Spec.Template.Spec.Volumes[2].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[2].VolumeSource.DataVolume.Name).To(Equal(dvtPvcName))

			Expect(vm.Spec.Template.Spec.Volumes[3].Name).To(Equal(pvc.Name))
			Expect(vm.Spec.Template.Spec.Volumes[3].VolumeSource.PersistentVolumeClaim).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[3].VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(pvc.Name))

			Expect(vm.Spec.Template.Spec.Volumes[4].Name).To(Equal(dvtBlankName))
			Expect(vm.Spec.Template.Spec.Volumes[4].VolumeSource.DataVolume).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[4].VolumeSource.DataVolume.Name).To(Equal(dvtBlankName))

			Expect(vm.Spec.Template.Spec.Volumes[5].Name).To(Equal("cloudinitdisk"))
			Expect(vm.Spec.Template.Spec.Volumes[5].VolumeSource.CloudInitNoCloud).ToNot(BeNil())
			Expect(vm.Spec.Template.Spec.Volumes[5].VolumeSource.CloudInitNoCloud.UserDataBase64).To(Equal(userDataB64))

			decoded, err := base64.StdEncoding.DecodeString(vm.Spec.Template.Spec.Volumes[5].VolumeSource.CloudInitNoCloud.UserDataBase64)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(decoded)).To(Equal(cloudInitUserData))
		})
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
	return vm
}

func createInstancetype(virtClient kubecli.KubevirtClient) *instancetypev1alpha2.VirtualMachineInstancetype {
	instancetype := &instancetypev1alpha2.VirtualMachineInstancetype{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "vm-instancetype-",
			Namespace:    util.NamespaceTestDefault,
		},
		Spec: instancetypev1alpha2.VirtualMachineInstancetypeSpec{
			CPU: instancetypev1alpha2.CPUInstancetype{
				Guest: uint32(1),
			},
			Memory: instancetypev1alpha2.MemoryInstancetype{
				Guest: resource.MustParse("128M"),
			},
		},
	}
	instancetype, err := virtClient.VirtualMachineInstancetype(util.NamespaceTestDefault).Create(context.Background(), instancetype, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	return instancetype
}

func createPreference(virtClient kubecli.KubevirtClient) *instancetypev1alpha2.VirtualMachinePreference {
	preference := &instancetypev1alpha2.VirtualMachinePreference{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "vm-preference-",
			Namespace:    util.NamespaceTestDefault,
		},
		Spec: instancetypev1alpha2.VirtualMachinePreferenceSpec{
			CPU: &instancetypev1alpha2.CPUPreferences{
				PreferredCPUTopology: instancetypev1alpha2.PreferCores,
			},
		},
	}
	preference, err := virtClient.VirtualMachinePreference(util.NamespaceTestDefault).Create(context.Background(), preference, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	return preference
}

func createDataSource(virtClient kubecli.KubevirtClient) *v1beta1.DataSource {
	dataSource := &v1beta1.DataSource{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "vm-datasource-",
		},
		Spec: v1beta1.DataSourceSpec{
			Source: v1beta1.DataSourceSource{},
		},
	}
	dataSource, err := virtClient.CdiClient().CdiV1beta1().DataSources(util.NamespaceTestDefault).Create(context.Background(), dataSource, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	return dataSource
}
